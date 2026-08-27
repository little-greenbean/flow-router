// Service → Runtime 薄委托，保持对外 API 兼容。
package gateway

import (
	"context"
	"net/http"
	"time"

	"github.com/bejix/upstream-ops/backend/config"
	"github.com/bejix/upstream-ops/backend/connector"
	"github.com/bejix/upstream-ops/backend/storage"
	"github.com/gin-gonic/gin"
)

func (s *Service) gatewayRuntime() config.GatewayConfig {
	return s.runtime().gatewayRuntime()
}

func (s *Service) proxyURLForChannel(ch *storage.Channel) string {
	return s.runtime().proxyURLForChannel(ch)
}

func (s *Service) proxyURLForTarget(ch *storage.Channel, provider *storage.GatewayProvider) string {
	return s.runtime().proxyURLForTarget(ch, provider)
}

func (s *Service) httpClientForChannel(ch *storage.Channel) *http.Client {
	return s.runtime().httpClientForChannel(ch)
}

func (s *Service) httpClientForTarget(ch *storage.Channel, provider *storage.GatewayProvider) *http.Client {
	return s.runtime().httpClientForTarget(ch, provider)
}

// Authenticate 校验客户端密钥并返回所属分组。
func (s *Service) Authenticate(c *gin.Context) (*AuthResult, error) {
	return s.runtime().Authenticate(c)
}

// HandleForward 处理兼容协议的主转发入口。
func (s *Service) HandleForward(c *gin.Context, path string, kind protocolKind) {
	s.runtime().HandleForward(c, path, kind)
}

func (s *Service) loadGroupsByChannel(ctx context.Context, routes []storage.GatewayRoute) map[uint][]connector.APIKeyGroup {
	return s.runtime().loadGroupsByChannel(ctx, routes)
}

// loadGroupsByChannelBlocking 同步拉取源分组（管理面保存/重排专用）。
func (s *Service) loadGroupsByChannelBlocking(ctx context.Context, routes []storage.GatewayRoute) map[uint][]connector.APIKeyGroup {
	return s.runtime().loadGroupsByChannelBlocking(ctx, routes)
}

func (s *Service) storeChannelGroupsCache(channelID uint, groups []connector.APIKeyGroup) {
	s.runtime().storeChannelGroupsCache(channelID, groups)
}

// InvalidateChannelGroupsCache 清空源分组缓存。
func (s *Service) InvalidateChannelGroupsCache() {
	s.runtime().InvalidateChannelGroupsCache()
}

func (s *Service) resolveUpstreamTarget(route *storage.GatewayRoute) (*upstreamTarget, error) {
	return s.runtime().resolveUpstreamTarget(route)
}

func (s *Service) forwardOnce(
	ctx context.Context,
	c *gin.Context,
	target *upstreamTarget,
	path string,
	method string,
	inHeader http.Header,
	body []byte,
	stream bool,
	kind protocolKind,
	firstTokenTimeout time.Duration, // 0=关闭；从发起请求起算到首字节的总等待
) (status int, respHeader http.Header, respBody []byte, firstTokenMS *int64, err error) {
	return s.runtime().forwardOnce(ctx, c, target, path, method, inHeader, body, stream, kind, firstTokenTimeout)
}

func (s *Service) recordUsage(key *storage.GatewayKey, group *storage.GatewayGroup, route *storage.GatewayRoute, target *upstreamTarget, reqID, requestedModel, upstreamModel, chain string, tokens UsageTokens, rate, billingRate float64, stream bool, status int, success bool, errInfo usageErrorInfo, durationMS int64, firstTokenMS *int64, c *gin.Context, meta usageRecordMeta) {
	s.runtime().recordUsage(key, group, route, target, reqID, requestedModel, upstreamModel, chain, tokens, rate, billingRate, stream, status, success, errInfo, durationMS, firstTokenMS, c, meta)
}

// HandleModels 处理 /v1/models 列表。
func (s *Service) HandleModels(c *gin.Context) {
	s.runtime().HandleModels(c)
}

func (s *Service) fetchUpstreamModels(ctx context.Context, ch *storage.Channel, apiKey, userAgent string) ([]string, error) {
	return s.runtime().fetchUpstreamModels(ctx, ch, apiKey, userAgent)
}

// HandleCountTokens 处理 Anthropic count_tokens。
func (s *Service) HandleCountTokens(c *gin.Context) {
	s.runtime().HandleCountTokens(c)
}

// HandleUsage 处理用量相关公开查询（若启用）。
func (s *Service) HandleUsage(c *gin.Context) {
	s.runtime().HandleUsage(c)
}

// HandleResponsesWebSocket 处理 Responses WebSocket（能力探测/占位）。
func (s *Service) HandleResponsesWebSocket(c *gin.Context) {
	s.runtime().HandleResponsesWebSocket(c)
}

// HandleGeminiModels 处理 Gemini 风格 models 列表。
func (s *Service) HandleGeminiModels(c *gin.Context) {
	s.runtime().HandleGeminiModels(c)
}

// HandleGeminiGenerate 处理 Gemini generate 转发。
func (s *Service) HandleGeminiGenerate(c *gin.Context) {
	s.runtime().HandleGeminiGenerate(c)
}

// defaultUpstreamUserAgent 返回当前上游默认 UA。
func (s *Service) defaultUpstreamUserAgent() string {
	return s.runtime().defaultUpstreamUserAgent()
}

func (s *Service) resolveAdminUserAgent(group *storage.GatewayGroup, route *storage.GatewayRoute) string {
	return s.runtime().resolveAdminUserAgent(group, route)
}

// applyRouteUserAgentForAdmin 管理面探测路径写入上游 UA。
func (s *Service) applyRouteUserAgentForAdmin(target *upstreamTarget, group *storage.GatewayGroup, route *storage.GatewayRoute) {
	s.runtime().applyRouteUserAgentForAdmin(target, group, route)
}
