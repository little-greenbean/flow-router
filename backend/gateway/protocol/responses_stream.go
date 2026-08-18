package protocol

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ResponsesToAnthropicStream 将上游 /v1/responses SSE 事件增量转为 Anthropic Messages SSE。
// 对齐 sub2api ResponsesEventToAnthropicEvents：边收边转，避免缓冲整包后假流。
type ResponsesToAnthropicStream struct {
	Model string
	MsgID string

	messageStartSent bool
	messageStopSent  bool
	done             bool

	contentBlockIndex   int
	contentBlockOpen    bool
	currentBlockType    string // text | thinking | tool_use
	currentToolName     string
	currentToolArgs     string
	currentToolHadDelta bool
	hasToolCall         bool

	// output_index → anthropic content block index
	outputIndexToBlockIdx map[int]int

	inputTokens  int
	outputTokens int
	cacheRead    int
	cacheCreate  int
}

func NewResponsesToAnthropicStream(model string) *ResponsesToAnthropicStream {
	return &ResponsesToAnthropicStream{
		Model:                 model,
		MsgID:                 "msg_stream",
		outputIndexToBlockIdx: make(map[int]int),
	}
}

// Feed 处理一个完整 SSE 事件（event 名 + data 载荷）。
func (s *ResponsesToAnthropicStream) Feed(eventName, data string) [][]byte {
	if s == nil || s.done || s.messageStopSent {
		return nil
	}
	data = strings.TrimSpace(data)
	if data == "" || data == "[DONE]" {
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(data), &payload); err != nil {
		return nil
	}
	typ, _ := payload["type"].(string)
	if typ == "" {
		typ = strings.TrimSpace(eventName)
	}
	if typ == "" {
		// 裸 Response 对象（少见）
		if _, ok := payload["output"]; ok {
			return s.handleCompletedPayload(payload)
		}
		return nil
	}

	switch typ {
	case "response.created", "response.in_progress":
		return s.handleCreated(payload)
	case "response.output_item.added":
		return s.handleOutputItemAdded(payload)
	case "response.output_text.delta":
		return s.handleTextDelta(payload)
	case "response.output_text.done":
		return s.closeCurrentBlock()
	case "response.function_call_arguments.delta", "response.custom_tool_call_input.delta":
		return s.handleFuncArgsDelta(payload)
	case "response.function_call_arguments.done", "response.custom_tool_call_input.done":
		return s.handleFuncArgsDone(payload)
	case "response.output_item.done":
		return s.handleOutputItemDone(payload)
	case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
		return s.handleReasoningDelta(payload)
	case "response.reasoning_summary_text.done", "response.reasoning_text.done":
		return s.closeCurrentBlock()
	case "response.completed", "response.done", "response.incomplete", "response.failed":
		return s.handleCompleted(payload)
	default:
		// 部分网关只推带 response 的完成包，无 type 前缀事件
		if resp, ok := payload["response"].(map[string]any); ok && resp != nil {
			if st, _ := resp["status"].(string); st == "completed" || st == "incomplete" || st == "failed" {
				return s.handleCompleted(payload)
			}
		}
		return nil
	}
}

// Close 流异常结束时补 message_stop 等终端帧。
func (s *ResponsesToAnthropicStream) Close() [][]byte {
	if s == nil || s.done {
		return nil
	}
	s.done = true
	if s.messageStopSent {
		return nil
	}
	if !s.messageStartSent {
		// 空流：仍给最小合法 Anthropic 流，避免客户端挂起
		frames := s.ensureMessageStart()
		frames = append(frames, s.finalize("end_turn")...)
		return frames
	}
	return s.finalize(s.stopReason())
}

func (s *ResponsesToAnthropicStream) handleCreated(payload map[string]any) [][]byte {
	if resp, ok := payload["response"].(map[string]any); ok && resp != nil {
		if id, ok := resp["id"].(string); ok && id != "" {
			s.MsgID = id
		}
		if m, ok := resp["model"].(string); ok && m != "" && s.Model == "" {
			s.Model = m
		}
		if m, ok := resp["model"].(string); ok && m != "" {
			// 保留调用方 model 覆盖；仅在空时用上游
			if s.Model == "" || s.Model == "msg_stream" {
				s.Model = m
			}
		}
	}
	return s.ensureMessageStart()
}

func (s *ResponsesToAnthropicStream) ensureMessageStart() [][]byte {
	if s.messageStartSent {
		return nil
	}
	s.messageStartSent = true
	if s.MsgID == "" {
		s.MsgID = "msg_stream"
	}
	return [][]byte{encodeSSEFrame("message_start", map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id":            s.MsgID,
			"type":          "message",
			"role":          "assistant",
			"model":         s.Model,
			"content":       []any{},
			"stop_reason":   nil,
			"stop_sequence": nil,
			"usage":         map[string]any{"input_tokens": 0, "output_tokens": 0},
		},
	})}
}

func (s *ResponsesToAnthropicStream) handleOutputItemAdded(payload map[string]any) [][]byte {
	item, _ := payload["item"].(map[string]any)
	if item == nil {
		return nil
	}
	outIdx, _ := asInt(payload["output_index"])
	typ, _ := item["type"].(string)

	switch typ {
	case "function_call", "custom_tool_call", "tool_call":
		var frames [][]byte
		frames = append(frames, s.ensureMessageStart()...)
		frames = append(frames, s.closeCurrentBlock()...)

		idx := s.contentBlockIndex
		s.outputIndexToBlockIdx[outIdx] = idx
		s.contentBlockOpen = true
		s.currentBlockType = "tool_use"
		s.currentToolName, _ = item["name"].(string)
		s.currentToolArgs = ""
		s.currentToolHadDelta = false
		s.hasToolCall = true

		callID, _ := item["call_id"].(string)
		if callID == "" {
			callID, _ = item["id"].(string)
		}
		callID = fromResponsesCallID(callID)
		name := s.currentToolName

		frames = append(frames, encodeSSEFrame("content_block_start", map[string]any{
			"type":  "content_block_start",
			"index": idx,
			"content_block": map[string]any{
				"type":  "tool_use",
				"id":    callID,
				"name":  name,
				"input": map[string]any{},
			},
		}))
		return frames

	case "reasoning":
		var frames [][]byte
		frames = append(frames, s.ensureMessageStart()...)
		frames = append(frames, s.closeCurrentBlock()...)

		idx := s.contentBlockIndex
		s.outputIndexToBlockIdx[outIdx] = idx
		s.contentBlockOpen = true
		s.currentBlockType = "thinking"

		frames = append(frames, encodeSSEFrame("content_block_start", map[string]any{
			"type":  "content_block_start",
			"index": idx,
			"content_block": map[string]any{
				"type":     "thinking",
				"thinking": "",
			},
		}))
		return frames

	case "message":
		// 文本块在 output_text.delta 时懒开启
		return s.ensureMessageStart()
	default:
		return s.ensureMessageStart()
	}
}

func (s *ResponsesToAnthropicStream) handleTextDelta(payload map[string]any) [][]byte {
	delta, _ := payload["delta"].(string)
	if delta == "" {
		// 少数实现用 text 字段
		delta, _ = payload["text"].(string)
	}
	if delta == "" {
		return nil
	}

	var frames [][]byte
	frames = append(frames, s.ensureMessageStart()...)

	if !s.contentBlockOpen || s.currentBlockType != "text" {
		frames = append(frames, s.closeCurrentBlock()...)
		idx := s.contentBlockIndex
		s.contentBlockOpen = true
		s.currentBlockType = "text"
		frames = append(frames, encodeSSEFrame("content_block_start", map[string]any{
			"type":  "content_block_start",
			"index": idx,
			"content_block": map[string]any{
				"type": "text",
				"text": "",
			},
		}))
	}

	idx := s.contentBlockIndex
	frames = append(frames, encodeSSEFrame("content_block_delta", map[string]any{
		"type":  "content_block_delta",
		"index": idx,
		"delta": map[string]any{
			"type": "text_delta",
			"text": delta,
		},
	}))
	return frames
}

func (s *ResponsesToAnthropicStream) handleFuncArgsDelta(payload map[string]any) [][]byte {
	delta, _ := payload["delta"].(string)
	if delta == "" {
		return nil
	}
	outIdx, _ := asInt(payload["output_index"])
	blockIdx, ok := s.outputIndexToBlockIdx[outIdx]
	if !ok {
		// 无 added 事件时，尝试在当前 tool_use 块上追加
		if s.contentBlockOpen && s.currentBlockType == "tool_use" {
			blockIdx = s.contentBlockIndex
		} else {
			return nil
		}
	}
	if s.currentBlockType == "tool_use" {
		s.currentToolHadDelta = true
		s.currentToolArgs += delta
	}
	return [][]byte{encodeSSEFrame("content_block_delta", map[string]any{
		"type":  "content_block_delta",
		"index": blockIdx,
		"delta": map[string]any{
			"type":         "input_json_delta",
			"partial_json": delta,
		},
	})}
}

func (s *ResponsesToAnthropicStream) handleFuncArgsDone(payload map[string]any) [][]byte {
	if s.currentBlockType != "tool_use" {
		return s.closeCurrentBlock()
	}
	// 若全程无 delta，用完整 arguments 补一帧
	raw, _ := payload["arguments"].(string)
	if raw == "" {
		raw = s.currentToolArgs
	}
	var frames [][]byte
	if raw != "" && !s.currentToolHadDelta {
		idx := s.contentBlockIndex
		frames = append(frames, encodeSSEFrame("content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": idx,
			"delta": map[string]any{
				"type":         "input_json_delta",
				"partial_json": raw,
			},
		}))
	}
	frames = append(frames, s.closeCurrentBlock()...)
	return frames
}

func (s *ResponsesToAnthropicStream) handleReasoningDelta(payload map[string]any) [][]byte {
	delta, _ := payload["delta"].(string)
	if delta == "" {
		return nil
	}
	outIdx, _ := asInt(payload["output_index"])
	blockIdx, ok := s.outputIndexToBlockIdx[outIdx]
	if !ok {
		// 懒开启 thinking 块
		var frames [][]byte
		frames = append(frames, s.ensureMessageStart()...)
		if !s.contentBlockOpen || s.currentBlockType != "thinking" {
			frames = append(frames, s.closeCurrentBlock()...)
			idx := s.contentBlockIndex
			s.outputIndexToBlockIdx[outIdx] = idx
			s.contentBlockOpen = true
			s.currentBlockType = "thinking"
			frames = append(frames, encodeSSEFrame("content_block_start", map[string]any{
				"type":  "content_block_start",
				"index": idx,
				"content_block": map[string]any{
					"type":     "thinking",
					"thinking": "",
				},
			}))
			blockIdx = idx
		} else {
			blockIdx = s.contentBlockIndex
		}
		frames = append(frames, encodeSSEFrame("content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": blockIdx,
			"delta": map[string]any{
				"type":     "thinking_delta",
				"thinking": delta,
			},
		}))
		return frames
	}
	return [][]byte{encodeSSEFrame("content_block_delta", map[string]any{
		"type":  "content_block_delta",
		"index": blockIdx,
		"delta": map[string]any{
			"type":     "thinking_delta",
			"thinking": delta,
		},
	})}
}

func (s *ResponsesToAnthropicStream) handleOutputItemDone(payload map[string]any) [][]byte {
	item, _ := payload["item"].(map[string]any)
	if item == nil {
		return s.closeCurrentBlock()
	}
	// 若 function_call 整项在 done 才出现（无 added/delta），合成 tool_use
	typ, _ := item["type"].(string)
	if (typ == "function_call" || typ == "custom_tool_call" || typ == "tool_call") &&
		!(s.contentBlockOpen && s.currentBlockType == "tool_use") {
		// 检查是否已有对应 block
		outIdx, _ := asInt(payload["output_index"])
		if _, ok := s.outputIndexToBlockIdx[outIdx]; !ok {
			var frames [][]byte
			// 复用 added 逻辑
			frames = append(frames, s.handleOutputItemAdded(map[string]any{
				"item":         item,
				"output_index": outIdx,
			})...)
			// 补全 arguments
			args, _ := item["arguments"].(string)
			if args == "" {
				if a, ok := item["arguments"]; ok && a != nil {
					if b, err := json.Marshal(a); err == nil {
						args = string(b)
					}
				}
			}
			if args != "" {
				idx := s.contentBlockIndex
				frames = append(frames, encodeSSEFrame("content_block_delta", map[string]any{
					"type":  "content_block_delta",
					"index": idx,
					"delta": map[string]any{
						"type":         "input_json_delta",
						"partial_json": args,
					},
				}))
			}
			frames = append(frames, s.closeCurrentBlock()...)
			return frames
		}
	}
	if s.contentBlockOpen {
		return s.closeCurrentBlock()
	}
	return nil
}

func (s *ResponsesToAnthropicStream) handleCompleted(payload map[string]any) [][]byte {
	return s.handleCompletedPayload(payload)
}

func (s *ResponsesToAnthropicStream) handleCompletedPayload(payload map[string]any) [][]byte {
	if s.messageStopSent {
		return nil
	}
	// usage：事件顶层或 response.usage
	s.ingestUsage(payload)
	if resp, ok := payload["response"].(map[string]any); ok && resp != nil {
		s.ingestUsage(resp)
		if id, ok := resp["id"].(string); ok && id != "" {
			s.MsgID = id
		}
		if m, ok := resp["model"].(string); ok && m != "" && (s.Model == "" || s.Model == "msg_stream") {
			s.Model = m
		}
		// 若流中从未推过 delta，从 completed.output 兜底合成（仍一次性，但保证有内容）
		if !s.contentBlockOpen && s.contentBlockIndex == 0 {
			if frames := s.synthesizeFromOutput(resp); len(frames) > 0 {
				var out [][]byte
				out = append(out, s.ensureMessageStart()...)
				out = append(out, frames...)
				out = append(out, s.finalize(s.stopReasonFromResponse(resp))...)
				return out
			}
		}
		stop := s.stopReasonFromResponse(resp)
		var frames [][]byte
		frames = append(frames, s.ensureMessageStart()...)
		frames = append(frames, s.finalize(stop)...)
		return frames
	}
	var frames [][]byte
	frames = append(frames, s.ensureMessageStart()...)
	frames = append(frames, s.finalize(s.stopReason())...)
	return frames
}

func (s *ResponsesToAnthropicStream) synthesizeFromOutput(resp map[string]any) [][]byte {
	outputs, _ := resp["output"].([]any)
	if len(outputs) == 0 {
		return nil
	}
	var frames [][]byte
	for _, raw := range outputs {
		item, _ := raw.(map[string]any)
		if item == nil {
			continue
		}
		switch item["type"] {
		case "reasoning":
			var thinking string
			if summary, ok := item["summary"].([]any); ok {
				var parts []string
				for _, sm := range summary {
					m, _ := sm.(map[string]any)
					if m == nil {
						continue
					}
					if t, ok := m["text"].(string); ok && t != "" {
						parts = append(parts, t)
					}
				}
				thinking = strings.Join(parts, "")
			}
			if thinking == "" {
				continue
			}
			frames = append(frames, s.closeCurrentBlock()...)
			idx := s.contentBlockIndex
			s.contentBlockOpen = true
			s.currentBlockType = "thinking"
			frames = append(frames, encodeSSEFrame("content_block_start", map[string]any{
				"type":          "content_block_start",
				"index":         idx,
				"content_block": map[string]any{"type": "thinking", "thinking": ""},
			}))
			frames = append(frames, encodeSSEFrame("content_block_delta", map[string]any{
				"type":  "content_block_delta",
				"index": idx,
				"delta": map[string]any{"type": "thinking_delta", "thinking": thinking},
			}))
			frames = append(frames, s.closeCurrentBlock()...)
		case "message":
			var text string
			if c, ok := item["content"].([]any); ok {
				var parts []string
				for _, p := range c {
					pm, _ := p.(map[string]any)
					if pm == nil {
						continue
					}
					if pt, _ := pm["type"].(string); pt == "output_text" || pt == "text" {
						if t, ok := pm["text"].(string); ok {
							parts = append(parts, t)
						}
					}
				}
				text = strings.Join(parts, "")
			}
			if text == "" {
				continue
			}
			frames = append(frames, s.closeCurrentBlock()...)
			idx := s.contentBlockIndex
			s.contentBlockOpen = true
			s.currentBlockType = "text"
			frames = append(frames, encodeSSEFrame("content_block_start", map[string]any{
				"type":          "content_block_start",
				"index":         idx,
				"content_block": map[string]any{"type": "text", "text": ""},
			}))
			frames = append(frames, encodeSSEFrame("content_block_delta", map[string]any{
				"type":  "content_block_delta",
				"index": idx,
				"delta": map[string]any{"type": "text_delta", "text": text},
			}))
			frames = append(frames, s.closeCurrentBlock()...)
		case "function_call", "tool_call", "custom_tool_call":
			name, _ := item["name"].(string)
			args, _ := item["arguments"].(string)
			if args == "" {
				args = "{}"
			}
			callID, _ := item["call_id"].(string)
			if callID == "" {
				callID, _ = item["id"].(string)
			}
			callID = fromResponsesCallID(callID)
			frames = append(frames, s.closeCurrentBlock()...)
			idx := s.contentBlockIndex
			s.contentBlockOpen = true
			s.currentBlockType = "tool_use"
			s.hasToolCall = true
			frames = append(frames, encodeSSEFrame("content_block_start", map[string]any{
				"type":  "content_block_start",
				"index": idx,
				"content_block": map[string]any{
					"type":  "tool_use",
					"id":    callID,
					"name":  name,
					"input": map[string]any{},
				},
			}))
			frames = append(frames, encodeSSEFrame("content_block_delta", map[string]any{
				"type":  "content_block_delta",
				"index": idx,
				"delta": map[string]any{"type": "input_json_delta", "partial_json": args},
			}))
			frames = append(frames, s.closeCurrentBlock()...)
		}
	}
	return frames
}

func (s *ResponsesToAnthropicStream) closeCurrentBlock() [][]byte {
	if !s.contentBlockOpen {
		return nil
	}
	idx := s.contentBlockIndex
	s.contentBlockOpen = false
	s.contentBlockIndex++
	s.currentToolName = ""
	s.currentToolArgs = ""
	s.currentToolHadDelta = false
	s.currentBlockType = ""
	return [][]byte{encodeSSEFrame("content_block_stop", map[string]any{
		"type":  "content_block_stop",
		"index": idx,
	})}
}

func (s *ResponsesToAnthropicStream) finalize(stopReason string) [][]byte {
	if s.messageStopSent {
		return nil
	}
	var frames [][]byte
	frames = append(frames, s.closeCurrentBlock()...)
	if stopReason == "" {
		stopReason = s.stopReason()
	}
	usage := map[string]any{"output_tokens": s.outputTokens}
	if s.inputTokens > 0 {
		usage["input_tokens"] = s.inputTokens
	}
	if s.cacheRead > 0 {
		usage["cache_read_input_tokens"] = s.cacheRead
	}
	if s.cacheCreate > 0 {
		usage["cache_creation_input_tokens"] = s.cacheCreate
	}
	frames = append(frames, encodeSSEFrame("message_delta", map[string]any{
		"type": "message_delta",
		"delta": map[string]any{
			"stop_reason":   stopReason,
			"stop_sequence": nil,
		},
		"usage": usage,
	}))
	frames = append(frames, encodeSSEFrame("message_stop", map[string]any{"type": "message_stop"}))
	s.messageStopSent = true
	s.done = true
	return frames
}

func (s *ResponsesToAnthropicStream) stopReason() string {
	if s.hasToolCall {
		return "tool_use"
	}
	return "end_turn"
}

func (s *ResponsesToAnthropicStream) stopReasonFromResponse(resp map[string]any) string {
	if s.hasToolCall {
		return "tool_use"
	}
	if st, _ := resp["status"].(string); st == "incomplete" {
		return "max_tokens"
	}
	// completed.output 含 function_call
	if outs, ok := resp["output"].([]any); ok {
		for _, raw := range outs {
			item, _ := raw.(map[string]any)
			if item == nil {
				continue
			}
			if t, _ := item["type"].(string); t == "function_call" || t == "tool_call" || t == "custom_tool_call" {
				return "tool_use"
			}
		}
	}
	return "end_turn"
}

func (s *ResponsesToAnthropicStream) ingestUsage(m map[string]any) {
	if m == nil {
		return
	}
	u, _ := m["usage"].(map[string]any)
	if u == nil {
		return
	}
	if v, ok := asInt(u["input_tokens"]); ok {
		s.inputTokens = v
	}
	if v, ok := asInt(u["output_tokens"]); ok {
		s.outputTokens = v
	}
	if d, ok := u["input_tokens_details"].(map[string]any); ok {
		if v, ok := asInt(d["cached_tokens"]); ok {
			s.cacheRead = v
			if s.inputTokens >= v {
				s.inputTokens -= v
			}
		}
	}
	if v, ok := asInt(u["cache_creation_input_tokens"]); ok {
		s.cacheCreate = v
	}
}

// ---------------------------------------------------------------------------
// Responses → OpenAI Chat 增量流（chat/completions 入站 + responses 上游）
// ---------------------------------------------------------------------------

// ResponsesToOpenAIStream 将 Responses SSE 增量转为 chat.completion.chunk SSE。
type ResponsesToOpenAIStream struct {
	Model string
	MsgID string

	roleSent bool
	done     bool
	hasTool  bool
	// tool call_id → index
	toolIndex map[string]int
	// 已通过 arguments.delta 推送过参数的 call（done 时禁止再推全量，避免客户端拼接重复）
	toolArgsDelta map[string]bool
	// 已宣告 name/id 的 call
	toolAnnounced map[string]bool
	nextTool      int
	finish        string
}

func NewResponsesToOpenAIStream(model string) *ResponsesToOpenAIStream {
	return &ResponsesToOpenAIStream{
		Model:         model,
		MsgID:         "chatcmpl-stream",
		toolIndex:     make(map[string]int),
		toolArgsDelta: make(map[string]bool),
		toolAnnounced: make(map[string]bool),
	}
}

func (s *ResponsesToOpenAIStream) Feed(eventName, data string) [][]byte {
	if s == nil || s.done {
		return nil
	}
	data = strings.TrimSpace(data)
	if data == "" || data == "[DONE]" {
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(data), &payload); err != nil {
		return nil
	}
	typ, _ := payload["type"].(string)
	if typ == "" {
		typ = strings.TrimSpace(eventName)
	}

	switch typ {
	case "response.created", "response.in_progress":
		if resp, ok := payload["response"].(map[string]any); ok && resp != nil {
			if id, ok := resp["id"].(string); ok && id != "" {
				s.MsgID = id
			}
			if m, ok := resp["model"].(string); ok && m != "" {
				s.Model = m
			}
		}
		return s.ensureRole()
	case "response.output_text.delta":
		delta, _ := payload["delta"].(string)
		if delta == "" {
			return nil
		}
		var frames [][]byte
		frames = append(frames, s.ensureRole()...)
		frames = append(frames, openAISSEFrame(openaiChunk(s.MsgID, s.Model, map[string]any{"content": delta}, nil)))
		return frames
	case "response.output_item.added":
		item, _ := payload["item"].(map[string]any)
		if item == nil {
			return nil
		}
		it, _ := item["type"].(string)
		if it != "function_call" && it != "custom_tool_call" && it != "tool_call" {
			return s.ensureRole()
		}
		s.hasTool = true
		callID, _ := item["call_id"].(string)
		if callID == "" {
			callID, _ = item["id"].(string)
		}
		name, _ := item["name"].(string)
		idx := s.allocToolIndex(callID)
		s.toolAnnounced[callID] = true
		var frames [][]byte
		frames = append(frames, s.ensureRole()...)
		frames = append(frames, openAISSEFrame(openaiChunk(s.MsgID, s.Model, map[string]any{
			"tool_calls": []any{
				map[string]any{
					"index": idx,
					"id":    callID,
					"type":  "function",
					"function": map[string]any{
						"name":      name,
						"arguments": "",
					},
				},
			},
		}, nil)))
		return frames
	case "response.function_call_arguments.delta", "response.custom_tool_call_input.delta":
		delta, _ := payload["delta"].(string)
		if delta == "" {
			return nil
		}
		callID, _ := payload["call_id"].(string)
		idx := s.resolveToolIndex(payload, callID)
		if callID != "" {
			s.toolArgsDelta[callID] = true
		} else {
			// 无 call_id 时用 index 键
			s.toolArgsDelta[fmt.Sprintf("#%d", idx)] = true
		}
		return [][]byte{openAISSEFrame(openaiChunk(s.MsgID, s.Model, map[string]any{
			"tool_calls": []any{
				map[string]any{
					"index": idx,
					"function": map[string]any{
						"arguments": delta,
					},
				},
			},
		}, nil))}
	case "response.function_call_arguments.done", "response.custom_tool_call_input.done":
		// 若已有 delta 流，忽略 done 全量，避免参数重复
		callID, _ := payload["call_id"].(string)
		if callID != "" && s.toolArgsDelta[callID] {
			return nil
		}
		args, _ := payload["arguments"].(string)
		if args == "" {
			return nil
		}
		idx := s.resolveToolIndex(payload, callID)
		if callID != "" {
			s.toolArgsDelta[callID] = true
		}
		return [][]byte{openAISSEFrame(openaiChunk(s.MsgID, s.Model, map[string]any{
			"tool_calls": []any{
				map[string]any{
					"index": idx,
					"function": map[string]any{
						"arguments": args,
					},
				},
			},
		}, nil))}
	case "response.output_item.done":
		item, _ := payload["item"].(map[string]any)
		if item == nil {
			return nil
		}
		it, _ := item["type"].(string)
		if it != "function_call" && it != "custom_tool_call" && it != "tool_call" {
			return nil
		}
		s.hasTool = true
		callID, _ := item["call_id"].(string)
		if callID == "" {
			callID, _ = item["id"].(string)
		}
		name, _ := item["name"].(string)
		args, _ := item["arguments"].(string)
		idx := s.allocToolIndex(callID)

		// 已通过 delta 推过参数：仅在未宣告时补 name/id，绝不重推全量 arguments
		hadDelta := callID != "" && s.toolArgsDelta[callID]
		announced := callID != "" && s.toolAnnounced[callID]
		if hadDelta && announced {
			return nil
		}

		fn := map[string]any{}
		if !announced {
			fn["name"] = name
			if !hadDelta && args != "" {
				fn["arguments"] = args
			} else {
				fn["arguments"] = ""
			}
		} else if !hadDelta && args != "" {
			// 宣告过但无 delta：用 done 补全量（非增量上游）
			fn["arguments"] = args
		} else {
			return nil
		}
		s.toolAnnounced[callID] = true
		if !hadDelta && args != "" {
			s.toolArgsDelta[callID] = true
		}
		tc := map[string]any{
			"index":    idx,
			"function": fn,
		}
		if !announced {
			tc["id"] = callID
			tc["type"] = "function"
		}
		var frames [][]byte
		frames = append(frames, s.ensureRole()...)
		frames = append(frames, openAISSEFrame(openaiChunk(s.MsgID, s.Model, map[string]any{
			"tool_calls": []any{tc},
		}, nil)))
		return frames
	case "response.completed", "response.done", "response.incomplete", "response.failed":
		return s.handleCompleted(payload)
	default:
		return nil
	}
}

func (s *ResponsesToOpenAIStream) Close() [][]byte {
	if s == nil || s.done {
		return nil
	}
	s.done = true
	var frames [][]byte
	if !s.roleSent {
		frames = append(frames, s.ensureRole()...)
	}
	finish := s.finish
	if finish == "" {
		if s.hasTool {
			finish = "tool_calls"
		} else {
			finish = "stop"
		}
	}
	frames = append(frames, openAISSEFrame(openaiChunk(s.MsgID, s.Model, map[string]any{}, finish)))
	frames = append(frames, []byte("data: [DONE]\n\n"))
	return frames
}

func (s *ResponsesToOpenAIStream) ensureRole() [][]byte {
	if s.roleSent {
		return nil
	}
	s.roleSent = true
	return [][]byte{openAISSEFrame(openaiChunk(s.MsgID, s.Model, map[string]any{
		"role":    "assistant",
		"content": "",
	}, nil))}
}

func (s *ResponsesToOpenAIStream) allocToolIndex(callID string) int {
	if callID != "" {
		if i, ok := s.toolIndex[callID]; ok {
			return i
		}
		i := s.nextTool
		s.toolIndex[callID] = i
		s.nextTool++
		return i
	}
	i := s.nextTool
	s.nextTool++
	return i
}

func (s *ResponsesToOpenAIStream) resolveToolIndex(payload map[string]any, callID string) int {
	if callID != "" {
		return s.allocToolIndex(callID)
	}
	if cid, ok := payload["call_id"].(string); ok && cid != "" {
		return s.allocToolIndex(cid)
	}
	// 单 tool 优先 0
	if s.nextTool == 1 {
		return 0
	}
	outIdx, ok := asInt(payload["output_index"])
	if ok {
		return outIdx
	}
	if s.nextTool > 0 {
		return s.nextTool - 1
	}
	return 0
}

func (s *ResponsesToOpenAIStream) handleCompleted(payload map[string]any) [][]byte {
	if s.done {
		return nil
	}
	// 若从未推过内容，从 completed.output 兜底
	if resp, ok := payload["response"].(map[string]any); ok && resp != nil {
		if id, ok := resp["id"].(string); ok && id != "" {
			s.MsgID = id
		}
		if !s.roleSent || (s.nextTool == 0 && !s.hasTool) {
			// 尝试用缓冲路径拼一帧完整 chunk（通过 extract）
			if chat, err := responsesObjectToOpenAIChat(resp, s.Model); err == nil {
				// 转成流式末包：用 wrap 逻辑太重，这里直接发 content/tool_calls + finish
				var m map[string]any
				if json.Unmarshal(chat, &m) == nil {
					frames := s.emitChatMessageAsChunks(m)
					s.done = true
					frames = append(frames, []byte("data: [DONE]\n\n"))
					return frames
				}
			}
		}
		if st, _ := resp["status"].(string); st == "incomplete" {
			s.finish = "length"
		}
	}
	if s.hasTool {
		s.finish = "tool_calls"
	} else if s.finish == "" {
		s.finish = "stop"
	}
	s.done = true
	var frames [][]byte
	frames = append(frames, s.ensureRole()...)
	frames = append(frames, openAISSEFrame(openaiChunk(s.MsgID, s.Model, map[string]any{}, s.finish)))
	frames = append(frames, []byte("data: [DONE]\n\n"))
	return frames
}

func (s *ResponsesToOpenAIStream) emitChatMessageAsChunks(m map[string]any) [][]byte {
	var frames [][]byte
	frames = append(frames, s.ensureRole()...)
	choices, _ := m["choices"].([]any)
	if len(choices) == 0 {
		return frames
	}
	ch, _ := choices[0].(map[string]any)
	if ch == nil {
		return frames
	}
	msg, _ := ch["message"].(map[string]any)
	if msg == nil {
		return frames
	}
	if text, ok := msg["content"].(string); ok && text != "" {
		frames = append(frames, openAISSEFrame(openaiChunk(s.MsgID, s.Model, map[string]any{"content": text}, nil)))
	}
	if tcs, ok := msg["tool_calls"].([]any); ok {
		s.hasTool = len(tcs) > 0
		for i, raw := range tcs {
			tc, _ := raw.(map[string]any)
			if tc == nil {
				continue
			}
			fn, _ := tc["function"].(map[string]any)
			frames = append(frames, openAISSEFrame(openaiChunk(s.MsgID, s.Model, map[string]any{
				"tool_calls": []any{
					map[string]any{
						"index": i,
						"id":    tc["id"],
						"type":  "function",
						"function": map[string]any{
							"name":      fn["name"],
							"arguments": fn["arguments"],
						},
					},
				},
			}, nil)))
		}
	}
	finish, _ := ch["finish_reason"].(string)
	if finish == "" {
		if s.hasTool {
			finish = "tool_calls"
		} else {
			finish = "stop"
		}
	}
	s.finish = finish
	frames = append(frames, openAISSEFrame(openaiChunk(s.MsgID, s.Model, map[string]any{}, finish)))
	return frames
}
