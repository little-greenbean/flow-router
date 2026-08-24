// 管理面：组模型预览、同步与探测。
package gateway

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/bejix/upstream-ops/backend/gateway/protocol"
	"github.com/bejix/upstream-ops/backend/storage"
	"github.com/gin-gonic/gin"
)

// ListOfficialModelCatalog returns the public metadata directory used by
// Sub2API's "sync latest models" action.
func (a *AdminService) ListOfficialModelCatalog(ctx context.Context) ([]CatalogModel, error) {
	return FetchOfficialModelCatalog(ctx)
}

// SyncCatalogModels binds selected official catalog models to enabled routes in
// one gateway group. It is deliberately separate from channel /v1/models sync:
// catalog entries are metadata declarations, not proof of live availability.
func (a *AdminService) SyncCatalogModels(ctx context.Context, groupID uint, in CatalogSyncInput) (*CatalogSyncResult, error) {
	group, err := a.Groups.FindByID(groupID)
	if err != nil {
		return nil, err
	}
	routes, err := a.Routes.ListByGroupID(groupID)
	if err != nil {
		return nil, err
	}
	routeIDs := make([]uint, 0, len(in.RouteIDs))
	routeSeen := make(map[uint]struct{}, len(in.RouteIDs))
	for _, id := range in.RouteIDs {
		if _, ok := routeSeen[id]; ok {
			continue
		}
		routeSeen[id] = struct{}{}
		routeIDs = append(routeIDs, id)
	}
	if err := validateCatalogRouteIDs(routeIDs, routes); err != nil {
		return nil, err
	}
	requested := make([]string, 0, len(in.Models))
	seen := make(map[string]struct{}, len(in.Models))
	for _, id := range in.Models {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		requested = append(requested, id)
	}
	if len(requested) == 0 {
		return nil, errors.New("at least one catalog model is required")
	}

	catalog, err := FetchOfficialModelCatalog(ctx)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]CatalogModel, len(catalog))
	for _, model := range catalog {
		byID[model.ID] = model
	}
	selected := make([]CatalogModel, 0, len(requested))
	for _, id := range requested {
		model, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("catalog model %q was not found", id)
		}
		selected = append(selected, model)
	}

	selectedRoutes := make(map[uint]storage.GatewayRoute, len(routeIDs))
	for _, route := range routes {
		for _, id := range routeIDs {
			if route.ID == id {
				selectedRoutes[id] = route
				break
			}
		}
	}
	sources := make([]ModelSource, 0, len(routeIDs))
	for _, id := range routeIDs {
		route := selectedRoutes[id]
		source := ModelSource{RouteID: route.ID, ChannelID: route.SourceChannelID,
			SourceGroupID: route.SourceGroupID, SourceGroupName: strings.TrimSpace(route.SourceGroupName)}
		if route.NormalizeSourceKind() == storage.GatewayRouteSourceProvider {
			if a.Providers != nil {
				if provider, findErr := a.Providers.FindByID(route.GatewayProviderID); findErr == nil && provider != nil {
					source.ChannelName = provider.Name
				}
			}
		} else if a.Channels != nil {
			if channel, findErr := a.Channels.FindByID(route.SourceChannelID); findErr == nil && channel != nil {
				source.ChannelName = channel.Name
			}
		}
		sources = append(sources, source)
	}

	existing := a.ParseModelsJSON(group.ModelsJSON)
	merged := mergeCatalogModels(existing, selected, sources)
	group.ModelsJSON = a.ModelsJSONString(merged)
	mode := group.ModelsMode
	modeChanged := false
	if strings.TrimSpace(mode) == "" || mode == storage.GatewayModelsModeAuto {
		group.ModelsMode = storage.GatewayModelsModeHybrid
		modeChanged = true
	}
	if err := a.Groups.Update(group); err != nil {
		return nil, err
	}
	a.invalidateModelsCache(groupID)
	return &CatalogSyncResult{
		Group: group, ModelIDs: requested, RouteIDs: routeIDs,
		ModelCount: len(selected), ModeChanged: modeChanged, ModelsMode: group.ModelsMode,
	}, nil
}

func (a *AdminService) collectGroupModels(
	ctx context.Context,
	groupID uint,
) (preview []ModelPreviewItem, routeResults []ModelSyncRouteResult, err error) {
	group, err := a.Groups.FindByID(groupID)
	if err != nil {
		return nil, nil, err
	}
	routes, err := a.Routes.ListByGroupID(groupID)
	if err != nil {
		return nil, nil, err
	}
	if len(routes) == 0 {
		return nil, nil, nil
	}

	// 多路由并发 GET /v1/models；结果按下标写回，合并阶段再串行去重。
	pulls := make([]routeModelPull, len(routes))
	if len(routes) == 1 {
		pulls[0] = a.pullRouteModels(ctx, group, routes[0])
	} else {
		sem := make(chan struct{}, a.gatewayRuntime().RouteBatchConcurrency)
		var wg sync.WaitGroup
		for i := range routes {
			i, route := i, routes[i]
			wg.Add(1)
			go func() {
				defer wg.Done()
				select {
				case sem <- struct{}{}:
					defer func() { <-sem }()
				case <-ctx.Done():
					pulls[i] = routeModelPull{
						rr: ModelSyncRouteResult{
							RouteID:    route.ID,
							SourceKind: route.NormalizeSourceKind(),
							ChannelID:  route.SourceChannelID,
							ProviderID: route.GatewayProviderID,
							Label:      fmt.Sprintf("route#%d", route.ID),
							Error:      ctx.Err().Error(),
						},
					}
					return
				}
				pulls[i] = a.pullRouteModels(ctx, group, route)
			}()
		}
		wg.Wait()
	}

	byModel := map[string]map[string]ModelSource{}
	routeResults = make([]ModelSyncRouteResult, 0, len(pulls))
	for i, pull := range pulls {
		routeResults = append(routeResults, pull.rr)
		if !pull.merge {
			continue
		}
		route := routes[i]
		srcKey := fmt.Sprintf("%d:%d:%d:%v:%s",
			pull.src.RouteID, pull.src.ChannelID, route.GatewayProviderID,
			pull.src.SourceGroupID, pull.src.SourceGroupName)
		for _, m := range pull.models {
			m = strings.TrimSpace(m)
			if m == "" {
				continue
			}
			if _, ok := byModel[m]; !ok {
				byModel[m] = map[string]ModelSource{}
			}
			byModel[m][srcKey] = pull.src
		}
	}

	preview = make([]ModelPreviewItem, 0, len(byModel))
	for id, srcMap := range byModel {
		sources := make([]ModelSource, 0, len(srcMap))
		for _, src := range srcMap {
			sources = append(sources, src)
		}
		preview = append(preview, ModelPreviewItem{
			ID:         id,
			ChannelIDs: a.channelIDsFromSources(sources),
			Sources:    sources,
		})
	}
	return preview, routeResults, nil
}

// pullRouteModels 单路由拉取上游模型清单（禁用/缺密钥记 skip，HTTP 失败记 error）。
// group 用于解析路由 UA 策略（与转发/模型测试一致）。

func (a *AdminService) pullRouteModels(ctx context.Context, group *storage.GatewayGroup, route storage.GatewayRoute) routeModelPull {
	rr := ModelSyncRouteResult{
		RouteID:    route.ID,
		SourceKind: route.NormalizeSourceKind(),
		ChannelID:  route.SourceChannelID,
		ProviderID: route.GatewayProviderID,
	}
	if !route.Enabled {
		rr.Skipped = true
		rr.SkipReason = "路由已禁用"
		rr.Label = fmt.Sprintf("route#%d", route.ID)
		return routeModelPull{rr: rr}
	}

	var models []string
	var src ModelSource
	// 拉模型无客户端 UA：组+路由解析后为空则用默认 UA
	ua := a.resolveAdminUserAgent(group, &route)

	if route.NormalizeSourceKind() == storage.GatewayRouteSourceProvider {
		target, resolveErr := a.resolveUpstreamTarget(&route)
		if resolveErr != nil {
			rr.Error = resolveErr.Error()
			rr.Label = fmt.Sprintf("直连 route#%d", route.ID)
			return routeModelPull{rr: rr}
		}
		name := ""
		if target.Provider != nil {
			name = target.Provider.Name
			rr.ProviderName = name
		}
		rr.Label = name
		if rr.Label == "" {
			rr.Label = fmt.Sprintf("直连 #%d", route.GatewayProviderID)
		}
		// 用 provider base + key 拉 /v1/models
		pseudo := &storage.Channel{
			ID:      0,
			Name:    rr.Label,
			SiteURL: target.BaseURL,
		}
		var fetchErr error
		models, fetchErr = a.fetchUpstreamModels(ctx, pseudo, target.APIKey, ua)
		if fetchErr != nil {
			rr.Error = fetchErr.Error()
			return routeModelPull{rr: rr}
		}
		src = ModelSource{
			RouteID:     route.ID,
			ChannelName: rr.Label,
		}
	} else {
		ch, chErr := a.Channels.FindByID(route.SourceChannelID)
		if chErr != nil {
			rr.Error = "渠道不存在: " + chErr.Error()
			rr.Label = fmt.Sprintf("渠道 #%d", route.SourceChannelID)
			return routeModelPull{rr: rr}
		}
		rr.ChannelName = ch.Name
		rr.Label = a.formatChannelGroupLabel(ch.Name, route.SourceGroupName, route.SourceChannelID)
		secret, decErr := a.Cipher.Decrypt(route.SourceAPIKeyCipher)
		if decErr != nil || strings.TrimSpace(secret) == "" {
			rr.Skipped = true
			rr.SkipReason = "未确保上游密钥"
			return routeModelPull{rr: rr}
		}
		var fetchErr error
		models, fetchErr = a.fetchUpstreamModels(ctx, ch, secret, ua)
		if fetchErr != nil {
			rr.Error = fetchErr.Error()
			return routeModelPull{rr: rr}
		}
		src = ModelSource{
			RouteID:         route.ID,
			ChannelID:       route.SourceChannelID,
			ChannelName:     ch.Name,
			SourceGroupID:   route.SourceGroupID,
			SourceGroupName: strings.TrimSpace(route.SourceGroupName),
		}
	}

	rr.OK = true
	rr.ModelCount = len(models)
	return routeModelPull{rr: rr, models: models, src: src, merge: true}
}

// PreviewGroupModels 预览聚合模型。
func (a *AdminService) PreviewGroupModels(ctx context.Context, groupID uint) ([]ModelPreviewItem, error) {
	preview, _, err := a.collectGroupModels(ctx, groupID)
	return preview, err
}

// SyncGroupModelsInput 同步组模型时可附带前端本地尚未落库的自定义模型。

func (a *AdminService) SyncGroupModels(ctx context.Context, groupID uint, in SyncGroupModelsInput) (*ModelSyncResult, error) {
	group, err := a.Groups.FindByID(groupID)
	if err != nil {
		return nil, err
	}
	// 失败路由跳过，成功的照常合并；整体仅在读库/写库失败时返回 error
	preview, routeResults, err := a.collectGroupModels(ctx, groupID)
	if err != nil {
		return nil, err
	}
	existing := a.ParseModelsJSON(group.ModelsJSON)
	custom := make([]ModelListItem, 0)
	customSeen := map[string]struct{}{}
	appendCustom := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		if _, ok := customSeen[id]; ok {
			return
		}
		customSeen[id] = struct{}{}
		custom = append(custom, ModelListItem{ID: id, Source: "custom"})
	}
	// 库内已保存的自定义
	for _, it := range existing {
		if it.Source == "custom" {
			appendCustom(it.ID)
		}
	}
	// 前端本地未保存的自定义（同步时一并保留，避免点同步冲掉）
	for _, it := range in.CustomModels {
		appendCustom(it.ID)
	}
	merged := make([]ModelListItem, 0, len(preview)+len(custom))
	seen := map[string]struct{}{}
	for _, p := range preview {
		seen[p.ID] = struct{}{}
		merged = append(merged, ModelListItem{
			ID:         p.ID,
			Source:     "sync",
			ChannelIDs: p.ChannelIDs,
			Sources:    p.Sources,
		})
	}
	for _, c := range custom {
		if _, ok := seen[c.ID]; ok {
			continue
		}
		merged = append(merged, c)
	}
	group.ModelsJSON = a.ModelsJSONString(merged)
	if err := a.Groups.Update(group); err != nil {
		return nil, err
	}
	a.invalidateModelsCache(groupID)

	res := &ModelSyncResult{
		Group:      group,
		ModelCount: len(preview),
		Routes:     routeResults,
	}
	for _, r := range routeResults {
		if r.Skipped {
			res.SkipCount++
		} else if r.OK {
			res.OKCount++
		} else {
			res.FailCount++
		}
	}
	return res, nil
}

// TestModelInput 模型可用性探测。

const (
	// probeTimeout 单条探测的硬上限，含建连、等首字和收尾。
	probeTimeout = 30 * time.Second
	// probeFirstTokenDefault 没配组级首字超时时，探测自己的首字预算。
	// 取 20s：实测这些 Kiro/Claude 中转真实流量首字均值 2~3 秒、长尾到 30s+，
	// 20s 足够放过"慢但活着"的路由，又不会让一条死链把整批拖满 30 秒。
	probeFirstTokenDefault = 20 * time.Second
)

// probeFirstTokenWait 探测的首字预算：组上配了就跟组走（跟真实调度同一个口径），
// 没配用默认值；无论如何要给 probeTimeout 留出收尾的余量，否则首字超时还没触发
// 就被 ctx 掐了，错误信息会退化成没什么信息量的 context deadline exceeded。
func probeFirstTokenWait(group *storage.GatewayGroup) time.Duration {
	wait := probeFirstTokenDefault
	if group != nil && group.FirstTokenTimeoutSec > 0 {
		wait = time.Duration(group.FirstTokenTimeoutSec) * time.Second
	}
	if max := probeTimeout - 5*time.Second; wait > max {
		wait = max
	}
	return wait
}

func (a *AdminService) TestGroupModel(ctx context.Context, groupID uint, in TestModelInput) ([]ModelTestResult, error) {
	model := strings.TrimSpace(in.Model)
	if model == "" {
		return nil, errors.New("model is required")
	}
	group, err := a.Groups.FindByID(groupID)
	if err != nil {
		return nil, err
	}
	routes, err := a.Routes.ListByGroupID(groupID)
	if err != nil {
		return nil, err
	}
	routes = filterRoutesForModel(routes, a.ParseModelsJSON(group.ModelsJSON), model)
	groupMapping := ParseModelMapping(group.ModelMappingJSON)
	now := time.Now()

	targets := make([]storage.GatewayRoute, 0, len(routes))
	for _, r := range routes {
		if in.RouteID != nil {
			if r.ID != *in.RouteID {
				continue
			}
			// 单测时允许已暂停路由，便于手动验证
			if !r.Enabled {
				return nil, errors.New("route is disabled")
			}
			if r.NormalizeSourceKind() == storage.GatewayRouteSourceMonitor &&
				strings.TrimSpace(r.SourceAPIKeyCipher) == "" {
				return nil, errors.New("route missing upstream api key; run ensure-keys")
			}
			if r.NormalizeSourceKind() == storage.GatewayRouteSourceProvider && r.GatewayProviderID == 0 {
				return nil, errors.New("route missing gateway_provider_id")
			}
			targets = append(targets, r)
			break
		}
		cp := r
		if !IsRouteSchedulable(&cp, now) {
			continue
		}
		targets = append(targets, r)
	}
	if len(targets) == 0 {
		if in.RouteID != nil {
			return nil, errors.New("route not found in group")
		}
		return nil, errors.New("no schedulable routes")
	}

	// 批量探测：并发打各路由（单路由仍最多 30s），结果按 targets 顺序返回。
	// 限制并发避免瞬时打爆上游或本机连接池。
	results := make([]ModelTestResult, len(targets))
	if len(targets) == 1 {
		results[0] = a.probeRouteModel(ctx, group, targets[0], model, groupMapping)
		return results, nil
	}
	sem := make(chan struct{}, a.gatewayRuntime().RouteBatchConcurrency)
	var wg sync.WaitGroup
	for i := range targets {
		i, route := i, targets[i]
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				results[i] = ModelTestResult{
					RouteID:         route.ID,
					SourceKind:      route.NormalizeSourceKind(),
					ChannelID:       route.SourceChannelID,
					SourceGroupID:   route.SourceGroupID,
					SourceGroupName: strings.TrimSpace(route.SourceGroupName),
					RequestedModel:  model,
					Error:           ctx.Err().Error(),
				}
				return
			}
			results[i] = a.probeRouteModel(ctx, group, route, model, groupMapping)
		}()
	}
	wg.Wait()
	return results, nil
}

func (a *AdminService) probeRouteModel(
	parent context.Context,
	group *storage.GatewayGroup,
	route storage.GatewayRoute,
	requestedModel string,
	groupMapping map[string]string,
) ModelTestResult {
	routeMapping := ParseModelMapping(route.ModelMappingJSON)
	upstreamModel, _ := ResolveModel(requestedModel, routeMapping, groupMapping)
	if upstreamModel == "" {
		upstreamModel = requestedModel
	}
	kind := route.NormalizeSourceKind()
	res := ModelTestResult{
		RouteID:           route.ID,
		SourceKind:        kind,
		ChannelID:         route.SourceChannelID,
		GatewayProviderID: route.GatewayProviderID,
		SourceGroupID:     route.SourceGroupID,
		SourceGroupName:   strings.TrimSpace(route.SourceGroupName),
		RequestedModel:    requestedModel,
		UpstreamModel:     upstreamModel,
	}

	target, err := a.resolveUpstreamTarget(&route)
	if err != nil {
		res.Error = err.Error()
		res.Label = fmt.Sprintf("route#%d", route.ID)
		return res
	}
	// 模型测试：组+路由 UA；无客户端，空则用默认 UA（与拉模型一致）
	a.runtime().applyRouteUserAgentForAdmin(target, group, &route)
	if target.Provider != nil {
		res.ChannelName = target.Provider.Name
		res.Label = target.Provider.Name
	} else if target.Channel != nil {
		res.ChannelName = target.Channel.Name
		res.Label = a.formatChannelGroupLabel(target.Channel.Name, route.SourceGroupName, route.SourceChannelID)
	}

	// 默认按 OpenAI Chat 入站协议探测
	inbound := protocol.KindOpenAIChat
	routeProto := a.normalizeUpstreamProtocol(route.UpstreamProtocol)
	if kind == storage.GatewayRouteSourceProvider && target.Provider != nil &&
		routeProto == storage.GatewayUpstreamProtocolAuto {
		if p := a.normalizeProviderProtocol(target.Provider.UpstreamProtocol); p != storage.GatewayUpstreamProtocolAuto {
			routeProto = p
		}
	}
	upstreamKind := protocol.ResolveUpstream(routeProto, inbound, upstreamModel)
	// 探测 body 统一用 chat 形态，再按上游协议转换。
	//
	// 这里必须是**流式**：非流式时上游要把整条响应生成完才回第一个字节，而中转
	// （尤其 Kiro 系）真实流量首字 2~3 秒就开始吐、整条却经常要十几秒、峰值上百秒。
	// 用非流式探测等于拿"跑完全程"的耗时去判"这条路通不通"，一批实际可用的路由会被
	// 判成 context deadline exceeded。改成流式后，拿到第一个字节就算通——跟真实
	// 使用场景（客户端都是流式）也是同一个口径。
	// 注意 stream 得同时给 body 和 prepareUpstreamRequest：同协议透传时用 body 里的，
	// 跨协议转换时用参数（见 protocol.OpenAIToAnthropicRequest 的 out["stream"]=stream）。
	chatBody := []byte(fmt.Sprintf(
		`{"model":%q,"max_tokens":1,"messages":[{"role":"user","content":"ping"}],"stream":true}`,
		upstreamModel,
	))
	body, path, _, convErr := a.prepareUpstreamRequest(chatBody, inbound, upstreamKind, upstreamModel, true, "/v1/chat/completions")
	if convErr != nil {
		res.Error = "protocol convert: " + convErr.Error()
		return res
	}
	res.UpstreamPath = path

	ctx, cancel := context.WithTimeout(parent, probeTimeout)
	defer cancel()
	start := time.Now()
	// 探测不走 gin 客户端上下文写回，使用空 Header（UA 由 target.UserAgentOverride 注入）
	status, _, respBody, firstTokenMS, fwdErr := a.forwardOnce(
		ctx, &gin.Context{}, target, path, http.MethodPost, http.Header{}, body, true, upstreamKind,
		probeFirstTokenWait(group),
	)
	res.LatencyMS = time.Since(start).Milliseconds()
	// 判定看首字，报的耗时也该是首字——后面那点收尾读是探测自己的开销，不是体感
	if firstTokenMS != nil && *firstTokenMS > 0 {
		res.LatencyMS = *firstTokenMS
	}
	res.StatusCode = status
	if fwdErr != nil {
		res.Error = fwdErr.Error()
		return res
	}
	// 2xx 视为可用；部分上游对 max_tokens=1 仍可能 400（参数限制），但 401/403/404/5xx 明确不可用
	if status >= 200 && status < 300 {
		res.OK = true
		return res
	}
	// 业务层错误：尽量截取上游 message
	msg := a.truncateProbeError(respBody, 240)
	if msg == "" {
		msg = fmt.Sprintf("upstream status %d", status)
	}
	// 少数上游对探测 payload 不兼容但模型实际存在：4xx 且非鉴权/找不到模型时标注
	res.Error = msg
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		res.OK = false
		return res
	}
	if status == http.StatusNotFound {
		res.OK = false
		return res
	}
	if status >= 500 {
		res.OK = false
		return res
	}
	// 其它 4xx：仍记失败，但错误信息保留供排查
	res.OK = false
	return res
}

func (a *AdminService) invalidateModelsCache(groupID uint) {
	a.modelsCacheMu.Lock()
	delete(a.modelsCache, groupID)
	a.modelsCacheMu.Unlock()
}

// ---------- forward ----------
