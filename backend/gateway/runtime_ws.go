package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bejix/upstream-ops/backend/gateway/protocol"
	"github.com/bejix/upstream-ops/backend/storage"
	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
)

// wsClientReadLimitBytes 是客户端 WebSocket 单帧读上限（64MiB），
// 覆盖 coder/websocket 默认的 32KiB。
const wsClientReadLimitBytes = 64 << 20

// HandleResponsesWebSocket forwards native Responses WebSocket sessions. A
// WebSocket session cannot safely change protocol mid-flight, so only routes
// whose resolved upstream protocol is Responses are eligible.
func (rt *Runtime) HandleResponsesWebSocket(c *gin.Context) {
	reqID := rt.ensureGatewayRequestID(c)
	if !strings.EqualFold(c.GetHeader("Upgrade"), "websocket") {
		c.JSON(http.StatusOK, gin.H{
			"message":                   "Use POST /v1/responses for Responses API",
			"websocket":                 "supported",
			jsonKeyUpstreamOpsRequestID: reqID,
		})
		return
	}

	auth, err := rt.Authenticate(c)
	if err != nil {
		rt.writeAuthError(c, protocolOpenAI, err.Error())
		return
	}
	if rt.Routes == nil {
		rt.writeGatewayError(c, protocolOpenAI, http.StatusServiceUnavailable, "api_error", "routes not configured")
		return
	}

	client, err := websocket.Accept(c.Writer, c.Request, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	// coder/websocket 默认读上限仅 32KiB，而 Responses 的 response.create 帧要携带
	// 完整上下文（生产常见 10-15 万 token ≈ 400-600KB），不放开会在读第一帧时就
	// 以 "read limited at 32769 bytes" 关闭连接。与号池侧 client_read_limit_bytes
	// 的默认值（64MiB）保持一致。
	client.SetReadLimit(wsClientReadLimitBytes)
	defer client.CloseNow()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	msgType, first, err := client.Read(ctx)
	if err != nil {
		return
	}
	if msgType != websocket.MessageText {
		rt.writeWSResponsesFailed(ctx, client, "invalid_request_error", "first WebSocket message must be response.create")
		return
	}
	requestedModel, err := wsResponsesCreateModel(first)
	if err != nil {
		rt.writeWSResponsesFailed(ctx, client, "invalid_request_error", err.Error())
		return
	}

	key, group := auth.Key, auth.Group
	first, err = rt.applyResponsesWebSocketSystemPrompt(group, key, first)
	if err != nil {
		rt.writeWSResponsesFailed(ctx, client, "invalid_request_error", "system prompt injection failed: "+err.Error())
		return
	}
	_ = rt.Keys.TouchLastUsed(key.ID, time.Now())
	routes, err := rt.Routes.ListByGroupID(group.ID)
	if err != nil || len(routes) == 0 {
		rt.writeWSResponsesFailed(ctx, client, "api_error", "no routes configured")
		return
	}
	routes = filterRoutesForModel(routes, rt.ParseModelsJSON(group.ModelsJSON), requestedModel)
	if len(routes) == 0 {
		rt.writeWSResponsesFailed(ctx, client, "api_error", "no route is bound to this model")
		return
	}
	groupsByChannel := rt.loadGroupsByChannel(c.Request.Context(), routes)
	groupMapping := ParseModelMapping(group.ModelMappingJSON)
	candidates := SortRoutes(routes, groupsByChannel, group.RateSortDirection, time.Now(), nil)

	var selected *wsResponsesRoute
	for _, cand := range candidates {
		route := cand.Route
		upstreamModel, chain := ResolveModel(requestedModel, ParseModelMapping(route.ModelMappingJSON), groupMapping)
		if upstreamModel == "" {
			upstreamModel = requestedModel
		}
		routeProto := rt.normalizeUpstreamProtocol(route.UpstreamProtocol)
		target, resolveErr := rt.resolveUpstreamTarget(&route)
		if resolveErr != nil {
			continue
		}
		if route.NormalizeSourceKind() == "provider" && routeProto == "auto" && target.Provider != nil {
			routeProto = rt.normalizeProviderProtocol(target.Provider.UpstreamProtocol)
		}
		if protocol.ResolveUpstream(routeProto, protocol.KindOpenAIResponses, upstreamModel) != protocol.KindOpenAIResponses {
			continue
		}
		rt.applyRouteUserAgent(target, group, &route)
		selected = &wsResponsesRoute{
			route: route, target: target, upstreamModel: upstreamModel, chain: chain,
			rate: cand.EffectiveRate, billingRate: cand.BillingRate,
		}
		break
	}
	if selected == nil {
		rt.writeWSResponsesFailed(ctx, client, "api_error", "no native Responses WebSocket route configured")
		return
	}

	upURL, err := responsesWebSocketURL(selected.target.BaseURL)
	if err != nil {
		rt.writeWSResponsesFailed(ctx, client, "api_error", err.Error())
		return
	}
	handshake, err := rt.buildUpstreamHTTPRequest(ctx, selected.target, "/v1/responses", http.MethodGet, c.Request.Header, nil, protocol.KindOpenAIResponses, false)
	if err != nil {
		rt.writeWSResponsesFailed(ctx, client, "api_error", err.Error())
		return
	}
	upHTTP := rt.httpClientForTarget(selected.target.Channel, selected.target.Provider)
	upHTTP.Timeout = 0
	upstream, _, err := websocket.Dial(ctx, upURL, &websocket.DialOptions{HTTPClient: upHTTP, HTTPHeader: handshake.Header})
	if err != nil {
		rt.writeWSResponsesFailed(ctx, client, "upstream_error", "failed to connect upstream: "+err.Error())
		return
	}
	defer upstream.CloseNow()

	first = RewriteModelInBody(first, selected.upstreamModel)
	if err := upstream.Write(ctx, websocket.MessageText, first); err != nil {
		rt.writeWSResponsesFailed(ctx, client, "upstream_error", "failed to send response.create: "+err.Error())
		return
	}

	serviceTier, reasoningEffort := ExtractMetaFromBody(first)
	meta := usageRecordMeta{
		InboundEndpoint: "/v1/responses", UpstreamEndpoint: "/v1/responses",
		InboundProtocol: string(protocol.KindOpenAIResponses), UpstreamProtocol: string(protocol.KindOpenAIResponses),
		ServiceTier: serviceTier, ReasoningEffort: reasoningEffort, UpstreamURL: upURL,
		Attempt: 1, AttemptKind: attemptKindPrimary, BillingInput: billingInputFromRequest("/v1/responses", first),
	}
	rt.forwardResponsesWebSocket(ctx, client, upstream, c, key, group, selected, reqID, requestedModel, meta)
}

type wsResponsesRoute struct {
	route         storage.GatewayRoute
	target        *upstreamTarget
	upstreamModel string
	chain         string
	rate          float64
	billingRate   float64
}

type wsMessage struct {
	typ  websocket.MessageType
	body []byte
	err  error
}

func (rt *Runtime) forwardResponsesWebSocket(
	ctx context.Context,
	client, upstream *websocket.Conn,
	c *gin.Context,
	key *storage.GatewayKey,
	group *storage.GatewayGroup,
	selected *wsResponsesRoute,
	reqID, requestedModel string,
	meta usageRecordMeta,
) {
	upMessages := make(chan wsMessage, 32)
	clientMessages := make(chan wsMessage, 1)
	go wsReadLoop(ctx, upstream, upMessages)
	go wsReadLoop(ctx, client, clientMessages)

	started := time.Now()
	var (
		body               bytes.Buffer
		tokens             UsageTokens
		clientDisconnected bool
		clientCh           = clientMessages
	)
	for {
		select {
		case msg, ok := <-clientCh:
			if !ok || msg.err != nil {
				clientDisconnected = true
				clientCh = nil
				continue
			}
			if msg.typ != websocket.MessageText {
				continue
			}
			payload, injectErr := rt.applyResponsesWebSocketSystemPrompt(group, key, msg.body)
			if injectErr != nil {
				rt.recordWSResponsesUsage(key, group, selected, reqID, requestedModel, tokens, false, "invalid_request_error", injectErr.Error(), body.Bytes(), time.Since(started), c, meta)
				if !clientDisconnected {
					rt.writeWSResponsesFailed(ctx, client, "invalid_request_error", "system prompt injection failed: "+injectErr.Error())
				}
				return
			}
			payload = RewriteModelInBody(payload, selected.upstreamModel)
			if err := upstream.Write(ctx, websocket.MessageText, payload); err != nil {
				rt.recordWSResponsesUsage(key, group, selected, reqID, requestedModel, tokens, false, "upstream_error", err.Error(), body.Bytes(), time.Since(started), c, meta)
				if !clientDisconnected {
					rt.writeWSResponsesFailed(ctx, client, "upstream_error", err.Error())
				}
				return
			}

		case msg, ok := <-upMessages:
			if !ok || msg.err != nil {
				errText := "upstream WebSocket closed before terminal event"
				if msg.err != nil && !errors.Is(msg.err, context.Canceled) {
					errText = msg.err.Error()
				}
				rt.recordWSResponsesUsage(key, group, selected, reqID, requestedModel, tokens, false, "transport", errText, body.Bytes(), time.Since(started), c, meta)
				if !clientDisconnected {
					rt.writeWSResponsesFailed(ctx, client, "stream_incomplete", errText)
				}
				return
			}
			if msg.typ != websocket.MessageText {
				if !clientDisconnected {
					_ = client.Write(ctx, msg.typ, msg.body)
				}
				continue
			}
			body.Write(msg.body)
			body.WriteByte('\n')
			rt.mergeStreamUsage(&tokens, string(msg.body), protocol.KindOpenAIResponses)
			terminal, success, errType, errText := wsResponsesTerminal(msg.body)
			if !clientDisconnected {
				if err := client.Write(ctx, websocket.MessageText, msg.body); err != nil {
					clientDisconnected = true
				}
			}
			if !terminal {
				continue
			}
			meta.BillingInput = billingInputWithResponse(meta.BillingInput, body.Bytes())
			rt.recordWSResponsesUsage(key, group, selected, reqID, requestedModel, rt.finalizeStreamTokens(tokens, body.Bytes(), protocol.KindOpenAIResponses), success, errType, errText, body.Bytes(), time.Since(started), c, meta)
			if success {
				_ = rt.Routes.NoteSuccessForPauseError(selected.route.ID)
			}
			return
		}
	}
}

func wsReadLoop(ctx context.Context, conn *websocket.Conn, out chan<- wsMessage) {
	defer close(out)
	for {
		typ, body, err := conn.Read(ctx)
		if err != nil {
			select {
			case out <- wsMessage{err: err}:
			case <-ctx.Done():
			}
			return
		}
		select {
		case out <- wsMessage{typ: typ, body: body}:
		case <-ctx.Done():
			return
		}
	}
}

func wsResponsesCreateModel(body []byte) (string, error) {
	var event struct {
		Type  string `json:"type"`
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &event); err != nil {
		return "", errors.New("invalid response.create message")
	}
	if event.Type != "response.create" {
		return "", errors.New("first WebSocket message must be response.create")
	}
	return strings.TrimSpace(event.Model), nil
}

func wsResponsesTerminal(body []byte) (terminal, success bool, errType, errText string) {
	var event struct {
		Type     string `json:"type"`
		Response struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		} `json:"response"`
	}
	if json.Unmarshal(body, &event) != nil {
		return false, false, "", ""
	}
	switch event.Type {
	case "response.completed", "response.done", "response.incomplete":
		return true, true, "", ""
	case "response.failed", "response.cancelled", "response.canceled":
		errType = event.Response.Error.Code
		if errType == "" {
			errType = "upstream_error"
		}
		errText = event.Response.Error.Message
		if errText == "" {
			errText = event.Type
		}
		return true, false, errType, errText
	default:
		return false, false, "", ""
	}
}

func (rt *Runtime) recordWSResponsesUsage(
	key *storage.GatewayKey,
	group *storage.GatewayGroup,
	selected *wsResponsesRoute,
	reqID, requestedModel string,
	tokens UsageTokens,
	success bool,
	errType, errText string,
	body []byte,
	duration time.Duration,
	c *gin.Context,
	meta usageRecordMeta,
) {
	if rt.Usage == nil || selected == nil {
		return
	}
	meta.BillingInput = billingInputWithResponse(meta.BillingInput, body)
	status := http.StatusOK
	if !success {
		status = http.StatusBadGateway
	}
	rt.recordUsage(key, group, &selected.route, selected.target, reqID, requestedModel, selected.upstreamModel, selected.chain, tokens, selected.rate, selected.billingRate, true, status, success, usageErrorInfo{Type: errType, Summary: errText}, duration.Milliseconds(), nil, c, meta)
}

func (rt *Runtime) writeWSResponsesFailed(ctx context.Context, conn *websocket.Conn, code, message string) {
	payload, _ := json.Marshal(map[string]any{
		"type": "response.failed",
		"response": map[string]any{
			"id": "resp_gateway_error", "object": "response", "status": "failed", "output": []any{},
			"error": map[string]any{"code": code, "message": message},
		},
	})
	_ = conn.Write(ctx, websocket.MessageText, payload)
}

func responsesWebSocketURL(base string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(base))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("invalid upstream base URL")
	}
	switch strings.ToLower(u.Scheme) {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	case "ws", "wss":
	default:
		return "", fmt.Errorf("unsupported upstream URL scheme %q", u.Scheme)
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/v1/responses"
	u.RawQuery = ""
	return u.String(), nil
}
