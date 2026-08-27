// 数据面：源分组列表缓存。
//
// 拉取源分组要打号商管理面（EnsureSession 可能触发登录），病态渠道单次能拖 30-60s+。
// 数据面请求绝不为此阻塞（stale-while-revalidate）：命中未过期缓存直接用；过期或缺失
// 先用旧值（缺失则回落已落库的账号计费倍率，见 RateForRoute），同时后台单飞刷新。
// 管理面（保存路由 / 倍率重排）需要最新 ratio，走同步的 Blocking 变体。
package gateway

import (
	"context"
	"sync"
	"time"

	"github.com/bejix/upstream-ops/backend/connector"
	"github.com/bejix/upstream-ops/backend/storage"
)

// channelGroupsRefreshTimeout 后台刷新单渠道的总超时。
// 内部可能串联登录 + 鉴权 + 拉分组（各受 upstream.TimeoutSeconds 约束），给足余量。
const channelGroupsRefreshTimeout = 2 * time.Minute

// collectRouteChannelIDs 提取 monitor 路由涉及的渠道 id（去重保序）。
func collectRouteChannelIDs(routes []storage.GatewayRoute) []uint {
	ids := make([]uint, 0)
	seen := make(map[uint]struct{})
	for _, r := range routes {
		if r.NormalizeSourceKind() == storage.GatewayRouteSourceProvider {
			continue
		}
		if r.SourceChannelID == 0 {
			continue
		}
		if _, ok := seen[r.SourceChannelID]; ok {
			continue
		}
		seen[r.SourceChannelID] = struct{}{}
		ids = append(ids, r.SourceChannelID)
	}
	return ids
}

// loadGroupsByChannel 数据面读取：只读缓存 + 异步刷新，永不阻塞调用方。
// 过期条目照常返回旧值（旧倍率优于扣住用户请求等号商管理面）；从未拉到过的渠道
// 返回 nil，排序回落到路由上已保存的计费倍率，尝试顺序仍与列表一致。
func (rt *Runtime) loadGroupsByChannel(_ context.Context, routes []storage.GatewayRoute) map[uint][]connector.APIKeyGroup {
	out := make(map[uint][]connector.APIKeyGroup)
	if rt.ChannelAPI == nil {
		return out
	}
	ids := collectRouteChannelIDs(routes)
	if len(ids) == 0 {
		return out
	}

	ttl := rt.gatewayRuntime().ModelsCacheTTL()
	if ttl <= 0 {
		ttl = 60 * time.Second
	}
	now := time.Now()
	var toRefresh []uint
	rt.channelGroupsCacheMu.Lock()
	if rt.channelGroupsCache == nil {
		rt.channelGroupsCache = map[uint]channelGroupsCacheEntry{}
	}
	if rt.channelGroupsRefreshing == nil {
		rt.channelGroupsRefreshing = map[uint]struct{}{}
	}
	for _, id := range ids {
		ent, ok := rt.channelGroupsCache[id]
		if ok {
			out[id] = ent.groups
		}
		if ok && now.Sub(ent.at) < ttl {
			continue
		}
		// 判重放在锁内：同一渠道同时只有一个后台刷新
		if _, busy := rt.channelGroupsRefreshing[id]; busy {
			continue
		}
		rt.channelGroupsRefreshing[id] = struct{}{}
		toRefresh = append(toRefresh, id)
	}
	rt.channelGroupsCacheMu.Unlock()

	for _, id := range toRefresh {
		go rt.refreshChannelGroups(id)
	}
	return out
}

// refreshChannelGroups 后台刷新单个渠道的源分组；调用前须已占住 channelGroupsRefreshing。
// 用独立 context：刷新是给缓存续命的，不该随触发它的那次请求一起被取消。
func (rt *Runtime) refreshChannelGroups(channelID uint) {
	defer func() {
		rt.channelGroupsCacheMu.Lock()
		delete(rt.channelGroupsRefreshing, channelID)
		rt.channelGroupsCacheMu.Unlock()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), channelGroupsRefreshTimeout)
	defer cancel()
	start := time.Now()
	groups, err := rt.ChannelAPI.ListAPIKeyGroups(ctx, channelID)
	if err != nil {
		// 失败保留旧值，只推后下次刷新时间：既不丢已知倍率，也不连环打病态渠道
		rt.channelGroupsCacheMu.Lock()
		old := rt.channelGroupsCache[channelID]
		rt.channelGroupsCache[channelID] = channelGroupsCacheEntry{at: time.Now(), groups: old.groups}
		rt.channelGroupsCacheMu.Unlock()
		if rt.Log != nil {
			rt.Log.Warn("refresh channel groups failed",
				"channel_id", channelID,
				"took_ms", time.Since(start).Milliseconds(),
				"err", err,
			)
		}
		return
	}
	rt.storeChannelGroupsCache(channelID, groups)
}

// loadGroupsByChannelBlocking 管理面读取：同步拉取，未命中时等上游返回。
// 保存路由 / 倍率重排要求拿到扫描后的最新 ratio，允许等；勿在数据面请求路径使用。
func (rt *Runtime) loadGroupsByChannelBlocking(ctx context.Context, routes []storage.GatewayRoute) map[uint][]connector.APIKeyGroup {
	out := make(map[uint][]connector.APIKeyGroup)
	if rt.ChannelAPI == nil {
		return out
	}
	ids := collectRouteChannelIDs(routes)
	if len(ids) == 0 {
		return out
	}

	// 先吃缓存，避免保存路由 / 倍率重排重复打上游。
	ttl := rt.gatewayRuntime().ModelsCacheTTL()
	if ttl <= 0 {
		ttl = 60 * time.Second
	}
	miss := make([]uint, 0, len(ids))
	now := time.Now()
	rt.channelGroupsCacheMu.Lock()
	if rt.channelGroupsCache == nil {
		rt.channelGroupsCache = map[uint]channelGroupsCacheEntry{}
	}
	for _, id := range ids {
		if ent, ok := rt.channelGroupsCache[id]; ok && now.Sub(ent.at) < ttl {
			out[id] = ent.groups
			continue
		}
		miss = append(miss, id)
	}
	rt.channelGroupsCacheMu.Unlock()

	if len(miss) == 0 {
		return out
	}

	fetchOne := func(id uint) []connector.APIKeyGroup {
		groups, err := rt.ChannelAPI.ListAPIKeyGroups(ctx, id)
		if err != nil {
			return nil
		}
		return groups
	}

	if len(miss) == 1 {
		groups := fetchOne(miss[0])
		out[miss[0]] = groups
		rt.storeChannelGroupsCache(miss[0], groups)
		return out
	}

	// 保存路由 / 重排：按渠道并发拉源分组，缩短批量等待。
	sem := make(chan struct{}, rt.gatewayRuntime().RouteBatchConcurrency)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, id := range miss {
		id := id
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				mu.Lock()
				out[id] = nil
				mu.Unlock()
				return
			}
			groups := fetchOne(id)
			mu.Lock()
			out[id] = groups
			mu.Unlock()
			rt.storeChannelGroupsCache(id, groups)
		}()
	}
	wg.Wait()
	return out
}

func (rt *Runtime) storeChannelGroupsCache(channelID uint, groups []connector.APIKeyGroup) {
	rt.channelGroupsCacheMu.Lock()
	defer rt.channelGroupsCacheMu.Unlock()
	if rt.channelGroupsCache == nil {
		rt.channelGroupsCache = map[uint]channelGroupsCacheEntry{}
	}
	rt.channelGroupsCache[channelID] = channelGroupsCacheEntry{at: time.Now(), groups: groups}
}

// InvalidateChannelGroupsCache 将源分组缓存整体标记为过期（倍率扫描后调用）。
// 只标过期不清数据：数据面继续用旧值并触发异步刷新，不会为等号商管理面卡住请求；
// 重排等同步路径视过期为未命中，仍会重新拉到扫描后的新 ratio。

func (rt *Runtime) InvalidateChannelGroupsCache() {
	rt.channelGroupsCacheMu.Lock()
	defer rt.channelGroupsCacheMu.Unlock()
	for id, ent := range rt.channelGroupsCache {
		ent.at = time.Time{}
		rt.channelGroupsCache[id] = ent
	}
}
