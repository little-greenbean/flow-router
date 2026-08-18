package protocol

import (
	"encoding/json"
	"strings"
)

// SupportsIncrementalStream 是否支持事件级增量 SSE 转换（或透传）。
// 三协议矩阵（Chat / Responses / Anthropic）互转均走真流，禁止假流缓冲。
func SupportsIncrementalStream(inbound, upstream Kind, converted bool) bool {
	if !converted {
		return true
	}
	in, up := NormalizeKind(inbound), NormalizeKind(upstream)
	// 任意 OpenAI Chat / Responses / Anthropic 互转均可增量
	okIn := in == KindOpenAIChat || in == KindOpenAI || in == KindOpenAIResponses || in == KindAnthropic
	okUp := up == KindOpenAIChat || up == KindOpenAI || up == KindOpenAIResponses || up == KindAnthropic
	return okIn && okUp
}

// AnthropicToOpenAIStream 将 Anthropic SSE 事件增量转为 OpenAI chat.completion.chunk SSE。
type AnthropicToOpenAIStream struct {
	Model    string
	MsgID    string
	RoleSent bool
	done     bool
	// content block index → tool_calls index（仅 tool_use）
	toolIndex map[int]int
	nextTool  int
	finish    any
}

func NewAnthropicToOpenAIStream(model string) *AnthropicToOpenAIStream {
	return &AnthropicToOpenAIStream{
		Model:     model,
		MsgID:     "chatcmpl-stream",
		toolIndex: make(map[int]int),
	}
}

// Feed 处理一个完整 SSE 事件的 data 载荷（及可选 event 名）。
func (s *AnthropicToOpenAIStream) Feed(eventName, data string) [][]byte {
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
	if typ == "" && eventName != "" {
		typ = eventName
	}
	var out [][]byte
	switch typ {
	case "message_start":
		if msg, ok := payload["message"].(map[string]any); ok {
			if id, ok := msg["id"].(string); ok && id != "" {
				s.MsgID = id
			}
			if m, ok := msg["model"].(string); ok && m != "" {
				s.Model = m
			}
		}
		if !s.RoleSent {
			s.RoleSent = true
			out = append(out, openAISSEFrame(openaiChunk(s.MsgID, s.Model, map[string]any{
				"role":    "assistant",
				"content": "",
			}, nil)))
		}
	case "content_block_start":
		block, _ := payload["content_block"].(map[string]any)
		if block == nil {
			return nil
		}
		bt, _ := block["type"].(string)
		blockIdx, _ := asInt(payload["index"])
		switch bt {
		case "tool_use":
			id, _ := block["id"].(string)
			name, _ := block["name"].(string)
			ti := s.nextTool
			s.toolIndex[blockIdx] = ti
			s.nextTool++
			if !s.RoleSent {
				s.RoleSent = true
				out = append(out, openAISSEFrame(openaiChunk(s.MsgID, s.Model, map[string]any{
					"role": "assistant",
				}, nil)))
			}
			out = append(out, openAISSEFrame(openaiChunk(s.MsgID, s.Model, map[string]any{
				"tool_calls": []any{
					map[string]any{
						"index": ti,
						"id":    id,
						"type":  "function",
						"function": map[string]any{
							"name":      name,
							"arguments": "",
						},
					},
				},
			}, nil)))
		}
	case "content_block_delta":
		delta, _ := payload["delta"].(map[string]any)
		if delta == nil {
			return nil
		}
		dt, _ := delta["type"].(string)
		blockIdx, _ := asInt(payload["index"])
		switch dt {
		case "text_delta":
			text, _ := delta["text"].(string)
			if text == "" {
				return nil
			}
			if !s.RoleSent {
				s.RoleSent = true
				out = append(out, openAISSEFrame(openaiChunk(s.MsgID, s.Model, map[string]any{
					"role":    "assistant",
					"content": "",
				}, nil)))
			}
			out = append(out, openAISSEFrame(openaiChunk(s.MsgID, s.Model, map[string]any{"content": text}, nil)))
		case "input_json_delta":
			partial, _ := delta["partial_json"].(string)
			if partial == "" {
				return nil
			}
			ti, ok := s.toolIndex[blockIdx]
			if !ok {
				ti = 0
			}
			out = append(out, openAISSEFrame(openaiChunk(s.MsgID, s.Model, map[string]any{
				"tool_calls": []any{
					map[string]any{
						"index": ti,
						"function": map[string]any{
							"arguments": partial,
						},
					},
				},
			}, nil)))
		case "thinking_delta":
			// chat 侧无标准 thinking 字段时，可选映射 reasoning_content
			text, _ := delta["thinking"].(string)
			if text == "" {
				return nil
			}
			out = append(out, openAISSEFrame(openaiChunk(s.MsgID, s.Model, map[string]any{
				"reasoning_content": text,
			}, nil)))
		}
	case "message_delta":
		delta, _ := payload["delta"].(map[string]any)
		var finish any
		if delta != nil {
			if sr, ok := delta["stop_reason"].(string); ok {
				finish = mapStopReasonToOpenAI(sr)
				s.finish = finish
			}
		}
		usageDelta := map[string]any{}
		if u, ok := payload["usage"].(map[string]any); ok {
			if v, ok := asInt(u["output_tokens"]); ok {
				usageDelta["completion_tokens"] = v
			}
			if v, ok := asInt(u["input_tokens"]); ok {
				usageDelta["prompt_tokens"] = v
			}
		}
		chunkMap := map[string]any{
			"id":      s.MsgID,
			"object":  "chat.completion.chunk",
			"created": 0,
			"model":   s.Model,
			"choices": []any{
				map[string]any{
					"index":         0,
					"delta":         map[string]any{},
					"finish_reason": finish,
				},
			},
		}
		if len(usageDelta) > 0 {
			chunkMap["usage"] = usageDelta
		}
		raw, _ := json.Marshal(chunkMap)
		out = append(out, openAISSEFrame(raw))
	case "message_stop", "error":
		// message_stop: Close 负责 [DONE]
	}
	return out
}

// Close 结束流并输出 [DONE]（仅一次）。
func (s *AnthropicToOpenAIStream) Close() [][]byte {
	if s == nil || s.done {
		return nil
	}
	s.done = true
	return [][]byte{[]byte("data: [DONE]\n\n")}
}

// OpenAIToAnthropicStream 将 OpenAI chat SSE 增量转为 Anthropic SSE。
// 支持 text + tool_calls（真流，不预开 text block）。
type OpenAIToAnthropicStream struct {
	Model   string
	MsgID   string
	Finish  string
	Started bool
	done    bool

	// 当前内容块
	textOpen     bool
	textIndex    int
	nextBlockIdx int
	// chat tool index → anthropic content block index
	toolBlockIdx map[int]int
	toolOpened   map[int]bool
}

func NewOpenAIToAnthropicStream(model string) *OpenAIToAnthropicStream {
	return &OpenAIToAnthropicStream{
		Model:        model,
		MsgID:        "msg_stream",
		toolBlockIdx: make(map[int]int),
		toolOpened:   make(map[int]bool),
	}
}

// EnsureStarted 只写 message_start（不再预开 text block，避免纯 tool 流多出空 text）。
func (s *OpenAIToAnthropicStream) EnsureStarted() [][]byte {
	if s == nil || s.Started || s.done {
		return nil
	}
	s.Started = true
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

// FeedData 处理 OpenAI SSE 的 data 载荷（不含 "data: " 前缀）。
func (s *OpenAIToAnthropicStream) FeedData(data string) [][]byte {
	if s == nil || s.done {
		return nil
	}
	data = strings.TrimSpace(data)
	if data == "" || data == "[DONE]" {
		return nil
	}
	var out [][]byte
	if !s.Started {
		out = append(out, s.EnsureStarted()...)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(data), &payload); err != nil {
		return out
	}
	if id, ok := payload["id"].(string); ok && id != "" {
		s.MsgID = id
	}
	if m, ok := payload["model"].(string); ok && m != "" {
		s.Model = m
	}
	choices, _ := payload["choices"].([]any)
	if len(choices) == 0 {
		return out
	}
	ch, _ := choices[0].(map[string]any)
	if ch == nil {
		return out
	}
	if fr, ok := ch["finish_reason"].(string); ok && fr != "" {
		s.Finish = fr
	}
	delta, _ := ch["delta"].(map[string]any)
	if delta == nil {
		return out
	}
	// text
	if text, ok := delta["content"].(string); ok && text != "" {
		if !s.textOpen {
			s.textIndex = s.nextBlockIdx
			s.nextBlockIdx++
			s.textOpen = true
			out = append(out, encodeSSEFrame("content_block_start", map[string]any{
				"type":  "content_block_start",
				"index": s.textIndex,
				"content_block": map[string]any{
					"type": "text",
					"text": "",
				},
			}))
		}
		out = append(out, encodeSSEFrame("content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": s.textIndex,
			"delta": map[string]any{
				"type": "text_delta",
				"text": text,
			},
		}))
	}
	// tool_calls
	if tcs, ok := delta["tool_calls"].([]any); ok {
		// 新 tool 前关闭 text block
		for _, raw := range tcs {
			tc, _ := raw.(map[string]any)
			if tc == nil {
				continue
			}
			tIdx, _ := asInt(tc["index"])
			if !s.toolOpened[tIdx] {
				if s.textOpen {
					out = append(out, encodeSSEFrame("content_block_stop", map[string]any{
						"type":  "content_block_stop",
						"index": s.textIndex,
					}))
					s.textOpen = false
				}
				bIdx := s.nextBlockIdx
				s.nextBlockIdx++
				s.toolBlockIdx[tIdx] = bIdx
				s.toolOpened[tIdx] = true
				id, _ := tc["id"].(string)
				name := ""
				if fn, ok := tc["function"].(map[string]any); ok {
					name, _ = fn["name"].(string)
				}
				out = append(out, encodeSSEFrame("content_block_start", map[string]any{
					"type":  "content_block_start",
					"index": bIdx,
					"content_block": map[string]any{
						"type":  "tool_use",
						"id":    id,
						"name":  name,
						"input": map[string]any{},
					},
				}))
			}
			bIdx := s.toolBlockIdx[tIdx]
			if fn, ok := tc["function"].(map[string]any); ok {
				// 迟到的 name：Anthropic 无单独 name delta，忽略
				if args, ok := fn["arguments"].(string); ok && args != "" {
					out = append(out, encodeSSEFrame("content_block_delta", map[string]any{
						"type":  "content_block_delta",
						"index": bIdx,
						"delta": map[string]any{
							"type":         "input_json_delta",
							"partial_json": args,
						},
					}))
				}
			}
		}
	}
	return out
}

// Close 写出 stop 系列事件。
func (s *OpenAIToAnthropicStream) Close() [][]byte {
	if s == nil || s.done {
		return nil
	}
	var out [][]byte
	if !s.Started {
		out = append(out, s.EnsureStarted()...)
	}
	s.done = true
	if s.textOpen {
		out = append(out, encodeSSEFrame("content_block_stop", map[string]any{
			"type":  "content_block_stop",
			"index": s.textIndex,
		}))
		s.textOpen = false
	}
	for tIdx, bIdx := range s.toolBlockIdx {
		if s.toolOpened[tIdx] {
			out = append(out, encodeSSEFrame("content_block_stop", map[string]any{
				"type":  "content_block_stop",
				"index": bIdx,
			}))
			s.toolOpened[tIdx] = false
		}
	}
	// 若全程无内容，补一个空 text block（Anthropic 要求 content 非空列表更稳）
	if s.nextBlockIdx == 0 {
		out = append(out, encodeSSEFrame("content_block_start", map[string]any{
			"type":          "content_block_start",
			"index":         0,
			"content_block": map[string]any{"type": "text", "text": ""},
		}))
		out = append(out, encodeSSEFrame("content_block_stop", map[string]any{
			"type":  "content_block_stop",
			"index": 0,
		}))
	}
	out = append(out, encodeSSEFrame("message_delta", map[string]any{
		"type": "message_delta",
		"delta": map[string]any{
			"stop_reason":   mapFinishReasonToAnthropic(s.Finish),
			"stop_sequence": nil,
		},
		"usage": map[string]any{"output_tokens": 0},
	}))
	out = append(out, encodeSSEFrame("message_stop", map[string]any{"type": "message_stop"}))
	return out
}

func openAISSEFrame(chunkJSON []byte) []byte {
	var b strings.Builder
	b.WriteString("data: ")
	b.Write(chunkJSON)
	b.WriteString("\n\n")
	return []byte(b.String())
}

func encodeSSEFrame(event string, payload any) []byte {
	var b strings.Builder
	writeSSE(&b, event, payload)
	return []byte(b.String())
}

// JoinSSEFrames 拼接多个完整 SSE frame。
func JoinSSEFrames(frames [][]byte) []byte {
	if len(frames) == 0 {
		return nil
	}
	var n int
	for _, f := range frames {
		n += len(f)
	}
	out := make([]byte, 0, n)
	for _, f := range frames {
		out = append(out, f...)
	}
	return out
}
