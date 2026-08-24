package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/bejix/upstream-ops/backend/gateway/protocol"
	"github.com/bejix/upstream-ops/backend/storage"
)

// 探测必须发流式：非流式时上游要把整条响应生成完才回第一个字节，而中转真实流量
// 首字 2~3 秒、整条却经常十几秒到上百秒，非流式探测会把一批实际可用的路由判成
// context deadline exceeded。stream 得同时落到 body 和转换参数上。
func TestProbeRequestIsStreaming(t *testing.T) {
	chatBody := []byte(`{"model":"claude-x","max_tokens":1,"messages":[{"role":"user","content":"ping"}],"stream":true}`)

	// 跨协议：OpenAI chat → Anthropic /v1/messages，stream 由参数决定
	fwd, path, converted, err := (*Service)(nil).prepareUpstreamRequest(
		chatBody, protocol.KindOpenAIChat, protocol.KindAnthropic, "claude-x", true, "/v1/chat/completions",
	)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if !converted {
		t.Fatal("openai chat -> anthropic should convert")
	}
	if path != "/v1/messages" {
		t.Fatalf("path = %q, want /v1/messages", path)
	}
	var out map[string]any
	if err := json.Unmarshal(fwd, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out["stream"] != true {
		t.Fatalf("anthropic body stream = %v, want true", out["stream"])
	}

	// 同协议透传：不做 body 转换，stream 只能来自 body 本身
	fwd, _, converted, err = (*Service)(nil).prepareUpstreamRequest(
		chatBody, protocol.KindOpenAIChat, protocol.KindOpenAIChat, "claude-x", true, "/v1/chat/completions",
	)
	if err != nil {
		t.Fatalf("prepare passthrough: %v", err)
	}
	if converted {
		t.Fatal("same protocol should not convert")
	}
	out = nil
	if err := json.Unmarshal(fwd, &out); err != nil {
		t.Fatalf("unmarshal passthrough: %v", err)
	}
	if out["stream"] != true {
		t.Fatalf("passthrough body stream = %v, want true (body 里就得是 true)", out["stream"])
	}
}

// 慢但活着的上游：首字很快、整条慢。这正是 Kiro 系中转的形态，探测必须判它可用。
func TestProbeAcceptsSlowBodyOnceFirstTokenArrives(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		_, _ = io.WriteString(w, "event: message_start\ndata: {}\n\n")
		if flusher != nil {
			flusher.Flush()
		}
		// 首字之后再磨蹭一会儿才收尾——非流式探测会把这整段都算进等待里
		time.Sleep(120 * time.Millisecond)
		_, _ = io.WriteString(w, "event: message_stop\ndata: {}\n\n")
	}))
	defer upstream.Close()

	svc := &Service{}
	status, _, _, firstTokenMS, err := svc.forwardOnce(
		context.Background(), &gin.Context{},
		&upstreamTarget{BaseURL: upstream.URL, APIKey: "k"},
		"/v1/messages", http.MethodPost, http.Header{}, []byte(`{}`),
		true, protocol.KindAnthropic, 2*time.Second,
	)
	if err != nil {
		t.Fatalf("forwardOnce: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if firstTokenMS == nil {
		t.Fatal("firstTokenMS is nil — 探测报的耗时要用首字，不能只有总耗时")
	}
	// 首字远早于收尾：判定和报数都该落在首字上
	if *firstTokenMS >= 120 {
		t.Fatalf("firstTokenMS = %d, 应该在上游磨蹭那 120ms 之前就拿到了", *firstTokenMS)
	}
}

// 一个字都不吐的上游：要在首字预算处停，给出「first token timeout」而不是干等到
// ctx 超时后退化成没什么信息量的 context deadline exceeded。
func TestProbeStopsAtFirstTokenBudget(t *testing.T) {
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		<-release // 响应头给了，body 一个字节都不给
	}))
	defer func() { close(release); upstream.Close() }()

	svc := &Service{}
	// ctx 给得比首字预算宽松：要证明是首字预算先生效，而不是被 ctx 掐的
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	_, _, _, _, err := svc.forwardOnce(
		ctx, &gin.Context{},
		&upstreamTarget{BaseURL: upstream.URL, APIKey: "k"},
		"/v1/messages", http.MethodPost, http.Header{}, []byte(`{}`),
		true, protocol.KindAnthropic, 300*time.Millisecond,
	)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("want first token timeout, got nil")
	}
	if !svc.isFirstTokenTimeout(err) {
		t.Fatalf("err = %v, want first token timeout（不是 context deadline exceeded）", err)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("elapsed = %v, 应该在首字预算处就停了", elapsed)
	}
}

func TestProbeFirstTokenWaitFollowsGroupThenClamps(t *testing.T) {
	if got := probeFirstTokenWait(nil); got != probeFirstTokenDefault {
		t.Fatalf("nil group = %v, want %v", got, probeFirstTokenDefault)
	}
	if got := probeFirstTokenWait(&storage.GatewayGroup{FirstTokenTimeoutSec: 0}); got != probeFirstTokenDefault {
		t.Fatalf("未配置 = %v, want %v", got, probeFirstTokenDefault)
	}
	// 配了就跟组走，跟真实调度同一个口径
	if got := probeFirstTokenWait(&storage.GatewayGroup{FirstTokenTimeoutSec: 8}); got != 8*time.Second {
		t.Fatalf("组内 8s = %v", got)
	}
	// 组上配得比探测硬上限还长时要夹住，否则首字超时永远轮不到触发
	if got := probeFirstTokenWait(&storage.GatewayGroup{FirstTokenTimeoutSec: 300}); got != probeTimeout-5*time.Second {
		t.Fatalf("超长配置 = %v, want %v", got, probeTimeout-5*time.Second)
	}
}
