// 数据面：公开 HTTP 端点（models / count_tokens / gemini 等）。
package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/bejix/upstream-ops/backend/connector"
	"github.com/bejix/upstream-ops/backend/gateway/protocol"
	"github.com/bejix/upstream-ops/backend/storage"
	"github.com/gin-gonic/gin"
)

// HandleModels 返回 OpenAI 风格模型列表。
func (rt *Runtime) HandleModels(c *gin.Context) {
	_ = rt.ensureGatewayRequestID(c)
	auth, err := rt.Authenticate(c)
	if err != nil {
		rt.writeAuthError(c, protocolOpenAI, err.Error())
		return
	}
	key, group := auth.Key, auth.Group
	_ = rt.Keys.TouchLastUsed(key.ID, time.Now())

	rt.modelsCacheMu.Lock()
	if ent, ok := rt.modelsCache[group.ID]; ok && time.Since(ent.at) < rt.gatewayRuntime().ModelsCacheTTL() {
		body := ent.body
		rt.modelsCacheMu.Unlock()
		c.Data(http.StatusOK, "application/json", body)
		return
	}
	rt.modelsCacheMu.Unlock()

	type modelObj struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		OwnedBy string `json:"owned_by"`
	}
	seen := map[string]struct{}{}
	data := make([]modelObj, 0)
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		data = append(data, modelObj{
			ID:      id,
			Object:  "model",
			Created: time.Now().Unix(),
			OwnedBy: "upstream-ops",
		})
	}

	mode := group.ModelsMode
	if mode == "" {
		mode = storage.GatewayModelsModeAuto
	}
	stored := rt.ParseModelsJSON(group.ModelsJSON)

	if mode == storage.GatewayModelsModeManual {
		for _, it := range stored {
			add(it.ID)
		}
	} else {
		// auto / hybrid: live aggregate
		routes, err := rt.Routes.ListByGroupID(group.ID)
		if err != nil {
			rt.writeGatewayError(c, protocolOpenAI, http.StatusInternalServerError, "api_error", err.Error())
			return
		}
		groupsByChannel := rt.loadGroupsByChannel(c.Request.Context(), routes)
		candidates := SortRoutes(routes, groupsByChannel, group.RateSortDirection, time.Now(), nil)
		groupMapping := ParseModelMapping(group.ModelMappingJSON)

		for _, cand := range candidates {
			route := cand.Route
			// 与 pullRouteModels / 转发一致：监控渠道与直连 provider 都走 resolveUpstreamTarget。
			// 旧逻辑只 FindByID(SourceChannelID)，直连路由 channel_id=0 会被整段跳过 → /v1/models 空列表。
			target, err := rt.resolveUpstreamTarget(&route)
			if err != nil {
				continue
			}
			ch := target.Channel
			if ch == nil {
				label := ""
				if target.Provider != nil {
					label = target.Provider.Name
				}
				ch = &storage.Channel{
					Name:    label,
					SiteURL: target.BaseURL,
				}
			}
			// 拉模型：组+路由 UA，空则默认 UA（与模型测试一致；转发仍透传客户端）
			models, err := rt.fetchUpstreamModels(c.Request.Context(), ch, target.APIKey, rt.resolveAdminUserAgent(group, &route))
			if err != nil {
				continue
			}
			routeMapping := ParseModelMapping(route.ModelMappingJSON)
			for _, m := range models {
				id := m
				for src, dst := range routeMapping {
					if dst == m && src != "*" {
						id = src
						break
					}
				}
				for src, dst := range groupMapping {
					if dst == m && src != "*" {
						id = src
						break
					}
				}
				if !routeAllowsModel(stored, route.ID, id) {
					continue
				}
				add(id)
			}
		}
		if mode == storage.GatewayModelsModeHybrid {
			for _, it := range stored {
				if it.Source == "custom" || it.Source == "catalog" {
					add(it.ID)
				}
			}
		}
	}

	payload, _ := json.Marshal(gin.H{"object": "list", "data": data})
	rt.modelsCacheMu.Lock()
	rt.modelsCache[group.ID] = modelsCacheEntry{at: time.Now(), body: payload}
	rt.modelsCacheMu.Unlock()
	c.Data(http.StatusOK, "application/json", payload)
}

// fetchUpstreamModels 拉取上游 /v1/models。
// userAgent 为组+路由解析结果；空则回落默认 UA（无客户端可透传）。

func (rt *Runtime) fetchUpstreamModels(ctx context.Context, ch *storage.Channel, apiKey, userAgent string) ([]string, error) {
	client := rt.httpClientForChannel(ch)
	client.Timeout = 30 * time.Second
	url := strings.TrimRight(ch.SiteURL, "/") + "/v1/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build GET %s: %w", url, err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", withDefaultUserAgent(userAgent, rt.defaultUpstreamUserAgent()))
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("GET %s: read body: %w", url, err)
	}
	if resp.StatusCode >= 400 {
		// 保留 URL + 状态码 + 上游 body 摘要，便于模型同步结果页排查（如 base_url 多写/少写 /v1）
		return nil, fmt.Errorf("GET %s: %w", url, connector.HTTPStatusError(resp.StatusCode, body))
	}
	var parsed struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		snippet := rt.extractUpstreamErrorSnippet(body)
		if snippet == "" {
			snippet = rt.truncateRunes(string(body), 200)
		}
		if snippet == "" {
			return nil, fmt.Errorf("GET %s: HTTP %d invalid models JSON: %w", url, resp.StatusCode, err)
		}
		return nil, fmt.Errorf("GET %s: HTTP %d invalid models JSON (%v): %s", url, resp.StatusCode, err, snippet)
	}
	out := make([]string, 0, len(parsed.Data))
	for _, d := range parsed.Data {
		if strings.TrimSpace(d.ID) != "" {
			out = append(out, d.ID)
		}
	}
	return out, nil
}

// HandleCountTokens POST /v1/messages/count_tokens
// 对齐 sub2api：优先透传上游；失败则本地粗估。

func (rt *Runtime) HandleCountTokens(c *gin.Context) {
	_ = rt.ensureGatewayRequestID(c)
	auth, err := rt.Authenticate(c)
	if err != nil {
		rt.writeAuthError(c, protocolAnthropic, err.Error())
		return
	}
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		rt.writeGatewayError(c, protocolAnthropic, http.StatusBadRequest, "invalid_request_error", "failed to read body")
		return
	}
	body, err = rt.applySystemPrompt(auth.Group, auth.Key, "/v1/messages/count_tokens", protocol.KindAnthropic, body)
	if err != nil {
		rt.writeGatewayError(c, protocolAnthropic, http.StatusBadRequest, "invalid_request_error", "system prompt injection failed: "+err.Error())
		return
	}
	// 尝试转发到第一条可用路由的 /v1/messages/count_tokens
	routes, _ := rt.Routes.ListByGroupID(auth.Group.ID)
	groupsByChannel := rt.loadGroupsByChannel(c.Request.Context(), routes)
	cands := SortRoutes(routes, groupsByChannel, auth.Group.RateSortDirection, time.Now(), nil)
	for _, cand := range cands {
		route := cand.Route
		target, rerr := rt.resolveUpstreamTarget(&route)
		if rerr != nil {
			continue
		}
		rt.applyRouteUserAgent(target, auth.Group, &route)
		status, _, respBody, _, ferr := rt.forwardOnce(
			c.Request.Context(), c, target, "/v1/messages/count_tokens",
			http.MethodPost, c.Request.Header, body, false, protocol.KindAnthropic, 0,
		)
		if ferr == nil && status >= 200 && status < 300 && len(respBody) > 0 {
			c.Data(status, "application/json", respBody)
			return
		}
	}
	// 本地粗估（字符/4）
	inputTokens := rt.estimateTokenCount(body)
	c.JSON(http.StatusOK, gin.H{
		"input_tokens": inputTokens,
		"type":         "message_count_tokens_result",
	})
}

// HandleUsage 用量相关端点。
func (rt *Runtime) HandleUsage(c *gin.Context) {
	_ = rt.ensureGatewayRequestID(c)
	auth, err := rt.Authenticate(c)
	if err != nil {
		rt.writeAuthError(c, protocolOpenAI, err.Error())
		return
	}
	if rt.Usage == nil {
		c.JSON(http.StatusOK, gin.H{"object": "list", "data": []any{}})
		return
	}
	page, err := rt.Usage.List(storage.GatewayUsageQuery{
		GatewayKeyID: auth.Key.ID,
		Page:         1,
		PageSize:     50,
	})
	if err != nil {
		rt.writeGatewayError(c, protocolOpenAI, http.StatusInternalServerError, "api_error", err.Error())
		return
	}
	stats, _ := rt.Usage.Stats(storage.GatewayUsageQuery{GatewayKeyID: auth.Key.ID})
	c.JSON(http.StatusOK, gin.H{
		"object": "usage",
		"key_id": auth.Key.ID,
		"stats":  stats,
		"recent": page.Items,
	})
}

// HandleGeminiModels GET /v1beta/models

func (rt *Runtime) HandleGeminiModels(c *gin.Context) {
	// 复用 OpenAI models 列表，包装为 Gemini 风格（简化兼容）
	reqID := rt.ensureGatewayRequestID(c)
	auth, err := rt.Authenticate(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error":                     gin.H{"message": err.Error(), "code": 401},
			jsonKeyUpstreamOpsRequestID: reqID,
		})
		return
	}
	// 调内部 HandleModels 逻辑太重，直接读组 models
	group := auth.Group
	list := rt.ParseModelsJSON(group.ModelsJSON)
	models := make([]gin.H, 0, len(list))
	for _, m := range list {
		name := "models/" + m.ID
		models = append(models, gin.H{
			"name":                       name,
			"displayName":                m.ID,
			"supportedGenerationMethods": []string{"generateContent", "countTokens"},
		})
	}
	if len(models) == 0 {
		// 回退：走上游 /v1/models 聚合
		rt.HandleModels(c)
		return
	}
	c.JSON(http.StatusOK, gin.H{"models": models})
}

// HandleGeminiGenerate POST /v1beta/models/*modelAction
// 将 Gemini generateContent 粗转为 OpenAI chat 再转发，响应简化回传。

func (rt *Runtime) HandleGeminiGenerate(c *gin.Context) {
	reqID := rt.ensureGatewayRequestID(c)
	action := c.Param("modelAction") // e.g. /gemini-pro:generateContent
	action = strings.TrimPrefix(action, "/")
	// 解析 model:action
	modelName := action
	if i := strings.LastIndex(action, ":"); i >= 0 {
		modelName = action[:i]
	}
	modelName = strings.TrimPrefix(modelName, "models/")

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":                     gin.H{"message": "bad body"},
			jsonKeyUpstreamOpsRequestID: reqID,
		})
		return
	}
	// contents → messages 粗转
	var gem map[string]any
	_ = json.Unmarshal(body, &gem)
	var messages []any
	if instruction, ok := gem["systemInstruction"].(map[string]any); ok {
		var text strings.Builder
		if parts, ok := instruction["parts"].([]any); ok {
			for _, part := range parts {
				partMap, ok := part.(map[string]any)
				if !ok {
					continue
				}
				if value, ok := partMap["text"].(string); ok {
					text.WriteString(value)
				}
			}
		}
		if text.Len() > 0 {
			messages = append(messages, map[string]any{"role": "system", "content": text.String()})
		}
	}
	if contents, ok := gem["contents"].([]any); ok {
		for _, raw := range contents {
			cm, _ := raw.(map[string]any)
			if cm == nil {
				continue
			}
			role, _ := cm["role"].(string)
			if role == "model" {
				role = "assistant"
			}
			if role == "" {
				role = "user"
			}
			text := ""
			if parts, ok := cm["parts"].([]any); ok {
				var b strings.Builder
				for _, p := range parts {
					if pm, ok := p.(map[string]any); ok {
						if t, ok := pm["text"].(string); ok {
							b.WriteString(t)
						}
					}
				}
				text = b.String()
			}
			messages = append(messages, map[string]any{"role": role, "content": text})
		}
	}
	if len(messages) == 0 {
		messages = []any{map[string]any{"role": "user", "content": string(body)}}
	}
	chatBody, _ := json.Marshal(map[string]any{
		"model":    modelName,
		"messages": messages,
		"stream":   false,
	})
	// 伪造 body 走标准转发
	c.Request.Body = io.NopCloser(bytes.NewReader(chatBody))
	c.Request.ContentLength = int64(len(chatBody))
	rt.HandleForward(c, "/v1/chat/completions", protocol.KindOpenAIChat)
}

// JSON / Header 中的网关请求 ID 字段（与使用记录 request_id 一致，便于排查）
const (
	ctxKeyUpstreamOpsRequestID  = "upstream_ops_request_id"
	headerUpstreamOpsRequestID  = "X-Upstream-Ops-Request-Id"
	jsonKeyUpstreamOpsRequestID = "upstream_ops_request_id"
)

// clientRequestIDHeaders 客户端请求相关 ID 头：原样透传到上游，网关不改写、不采纳为 usage.request_id。
var clientRequestIDHeaders = []string{
	"X-Request-Id",
	"X-Client-Request-Id",
	"X-Openai-Request-Id",
	"X-Correlation-Id",
	"Request-Id",
}

// copyClientRequestIDHeaders 把入站请求相关 ID 原样写到上游请求；不碰 X-Upstream-Ops-Request-Id。
