package protocol

import (
	"encoding/json"
	"fmt"
	"strings"
)

// AnthropicToOpenAIRequest 将 Anthropic /v1/messages 请求转为 OpenAI chat/completions。
func AnthropicToOpenAIRequest(body []byte, model string, stream bool) ([]byte, error) {
	var in map[string]any
	if err := json.Unmarshal(body, &in); err != nil {
		return nil, fmt.Errorf("invalid anthropic request: %w", err)
	}
	out := map[string]any{
		"stream": stream,
	}
	if model == "" {
		if m, ok := in["model"].(string); ok {
			model = m
		}
	}
	out["model"] = model

	if v, ok := asInt(in["max_tokens"]); ok && v > 0 {
		out["max_tokens"] = v
	}
	if v, ok := asFloat(in["temperature"]); ok {
		out["temperature"] = v
	}
	if v, ok := asFloat(in["top_p"]); ok {
		out["top_p"] = v
	}
	if stops, ok := in["stop_sequences"].([]any); ok && len(stops) > 0 {
		if len(stops) == 1 {
			out["stop"] = stops[0]
		} else {
			out["stop"] = stops
		}
	}

	messages := make([]any, 0)
	// system
	if sys, ok := in["system"]; ok && sys != nil {
		sysText := anthropicSystemToText(sys)
		if sysText != "" {
			messages = append(messages, map[string]any{
				"role":    "system",
				"content": sysText,
			})
		}
	}
	rawMsgs, _ := in["messages"].([]any)
	for _, raw := range rawMsgs {
		msg, _ := raw.(map[string]any)
		if msg == nil {
			continue
		}
		role, _ := msg["role"].(string)
		content := msg["content"]
		switch role {
		case "user":
			// 可能含 tool_result：拆成 chat 的 tool 消息，避免把 tool_result 当 user content 原样塞入。
			text, toolResults := splitAnthropicUserContent(content)
			userContent := anthropicUserContentWithoutToolResults(content)
			if userContent != nil {
				messages = append(messages, map[string]any{
					"role":    "user",
					"content": userContent,
				})
			} else if text != "" {
				messages = append(messages, map[string]any{
					"role":    "user",
					"content": text,
				})
			} else if len(toolResults) == 0 {
				// 空 user 轮次仍保留，避免对话结构塌掉
				messages = append(messages, map[string]any{
					"role":    "user",
					"content": "",
				})
			}
			for _, tr := range toolResults {
				messages = append(messages, tr)
			}
		case "assistant":
			text, toolCalls := anthropicContentToOpenAI(content)
			m := map[string]any{
				"role":    "assistant",
				"content": text,
			}
			if len(toolCalls) > 0 {
				m["tool_calls"] = toolCalls
				if text == "" {
					m["content"] = nil
				}
			}
			messages = append(messages, m)
		}
	}
	out["messages"] = messages

	if tools, ok := in["tools"].([]any); ok && len(tools) > 0 {
		oTools := make([]any, 0, len(tools))
		for _, t := range tools {
			tm, _ := t.(map[string]any)
			if tm == nil {
				continue
			}
			oTools = append(oTools, map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        tm["name"],
					"description": tm["description"],
					"parameters":  tm["input_schema"],
				},
			})
		}
		out["tools"] = oTools
	}
	if tc, ok := in["tool_choice"]; ok {
		out["tool_choice"] = convertToolChoiceAnthropicToOpenAI(tc)
	}

	// thinking → reasoning_effort
	if thinking, ok := in["thinking"].(map[string]any); ok {
		if typ, _ := thinking["type"].(string); typ == "enabled" {
			budget, _ := asInt(thinking["budget_tokens"])
			out["reasoning_effort"] = budgetToEffort(budget)
		}
	}

	return json.Marshal(out)
}

// OpenAIToAnthropicResponse 将 OpenAI chat.completion 转为 Anthropic message。
func OpenAIToAnthropicResponse(body []byte, model string) ([]byte, error) {
	var in map[string]any
	if err := json.Unmarshal(body, &in); err != nil {
		return nil, err
	}
	if errObj, ok := in["error"].(map[string]any); ok {
		return json.Marshal(map[string]any{
			"type": "error",
			"error": map[string]any{
				"type":    errObj["type"],
				"message": errObj["message"],
			},
		})
	}
	id, _ := in["id"].(string)
	if id == "" {
		id = "msg_converted"
	}
	if model == "" {
		if m, ok := in["model"].(string); ok {
			model = m
		}
	}
	var content any = []any{}
	var stopReason string
	choices, _ := in["choices"].([]any)
	if len(choices) > 0 {
		ch, _ := choices[0].(map[string]any)
		if ch != nil {
			if fr, ok := ch["finish_reason"].(string); ok {
				stopReason = mapFinishReasonToAnthropic(fr)
			}
			msg, _ := ch["message"].(map[string]any)
			if msg != nil {
				blocks := openAIMessageToAnthropicBlocks(msg)
				content = blocks
			}
		}
	}
	usage := map[string]any{}
	if u, ok := in["usage"].(map[string]any); ok {
		inTok, _ := asInt(u["prompt_tokens"])
		outTok, _ := asInt(u["completion_tokens"])
		usage["input_tokens"] = inTok
		usage["output_tokens"] = outTok
		if v, ok := asInt(u["cache_creation_input_tokens"]); ok {
			usage["cache_creation_input_tokens"] = v
		}
		if v, ok := asInt(u["cache_read_input_tokens"]); ok {
			usage["cache_read_input_tokens"] = v
		}
	}
	out := map[string]any{
		"id":          id,
		"type":        "message",
		"role":        "assistant",
		"model":       model,
		"content":     content,
		"stop_reason": stopReason,
		"usage":       usage,
	}
	return json.Marshal(out)
}

// OpenAISSEToAnthropicSSE 将缓冲的 OpenAI SSE 转为 Anthropic SSE。
func OpenAISSEToAnthropicSSE(sse []byte, model string) []byte {
	conv := NewOpenAIToAnthropicStream(model)
	frames := conv.EnsureStarted()
	for _, ev := range parseSSEEvents(string(sse)) {
		frames = append(frames, conv.FeedData(ev.Data)...)
	}
	frames = append(frames, conv.Close()...)
	return JoinSSEFrames(frames)
}

func writeSSE(b *strings.Builder, event string, payload any) {
	raw, _ := json.Marshal(payload)
	if event != "" {
		b.WriteString("event: ")
		b.WriteString(event)
		b.WriteString("\n")
	}
	b.WriteString("data: ")
	b.Write(raw)
	b.WriteString("\n\n")
}

func anthropicSystemToText(sys any) string {
	switch v := sys.(type) {
	case string:
		return v
	case []any:
		var parts []string
		for _, p := range v {
			pm, _ := p.(map[string]any)
			if pm == nil {
				continue
			}
			if t, _ := pm["type"].(string); t == "text" {
				if s, ok := pm["text"].(string); ok {
					parts = append(parts, s)
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}

func anthropicContentToOpenAIContent(content any) any {
	if c := anthropicUserContentWithoutToolResults(content); c != nil {
		return c
	}
	return ""
}

// anthropicUserContentWithoutToolResults 抽出 user 消息中的 text/image，去掉 tool_result。
// 若没有任何非 tool 内容返回 nil（调用方决定是否省略 user 消息）。
func anthropicUserContentWithoutToolResults(content any) any {
	switch v := content.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		return v
	case []any:
		allText := true
		var texts []string
		parts := make([]any, 0, len(v))
		for _, p := range v {
			pm, _ := p.(map[string]any)
			if pm == nil {
				continue
			}
			switch pm["type"] {
			case "text":
				s, _ := pm["text"].(string)
				texts = append(texts, s)
				parts = append(parts, map[string]any{"type": "text", "text": s})
			case "image":
				allText = false
				src, _ := pm["source"].(map[string]any)
				if src == nil {
					continue
				}
				var url string
				if st, _ := src["type"].(string); st == "base64" {
					mt, _ := src["media_type"].(string)
					data, _ := src["data"].(string)
					url = "data:" + mt + ";base64," + data
				} else if st == "url" {
					url, _ = src["url"].(string)
				}
				if url == "" {
					continue
				}
				parts = append(parts, map[string]any{
					"type": "image_url",
					"image_url": map[string]any{
						"url": url,
					},
				})
			case "tool_result":
				// 单独转 role=tool，不进 user content
				continue
			}
		}
		if len(parts) == 0 && len(texts) == 0 {
			return nil
		}
		if allText {
			joined := strings.Join(texts, "")
			if joined == "" {
				return nil
			}
			return joined
		}
		if len(parts) == 0 {
			return nil
		}
		return parts
	default:
		return nil
	}
}

func splitAnthropicUserContent(content any) (text string, toolMsgs []any) {
	arr, ok := content.([]any)
	if !ok {
		return contentToPlainText(content), nil
	}
	var texts []string
	for _, p := range arr {
		pm, _ := p.(map[string]any)
		if pm == nil {
			continue
		}
		switch pm["type"] {
		case "text":
			if s, ok := pm["text"].(string); ok {
				texts = append(texts, s)
			}
		case "tool_result":
			id, _ := pm["tool_use_id"].(string)
			c := contentToPlainText(pm["content"])
			toolMsgs = append(toolMsgs, map[string]any{
				"role":         "tool",
				"tool_call_id": id,
				"content":      c,
			})
		}
	}
	return strings.Join(texts, ""), toolMsgs
}

func openAIMessageToAnthropicBlocks(msg map[string]any) []any {
	blocks := make([]any, 0)
	if c, ok := msg["content"].(string); ok && c != "" {
		blocks = append(blocks, map[string]any{"type": "text", "text": c})
	} else if arr, ok := msg["content"].([]any); ok {
		for _, p := range arr {
			pm, _ := p.(map[string]any)
			if pm == nil {
				continue
			}
			if t, _ := pm["type"].(string); t == "text" {
				blocks = append(blocks, map[string]any{"type": "text", "text": pm["text"]})
			}
		}
	}
	if tcs, ok := msg["tool_calls"].([]any); ok {
		for _, tc := range tcs {
			tcm, _ := tc.(map[string]any)
			if tcm == nil {
				continue
			}
			id, _ := tcm["id"].(string)
			fn, _ := tcm["function"].(map[string]any)
			name, _ := fn["name"].(string)
			argsStr, _ := fn["arguments"].(string)
			var input any = map[string]any{}
			if strings.TrimSpace(argsStr) != "" {
				_ = json.Unmarshal([]byte(argsStr), &input)
			}
			blocks = append(blocks, map[string]any{
				"type":  "tool_use",
				"id":    id,
				"name":  name,
				"input": input,
			})
		}
	}
	if len(blocks) == 0 {
		blocks = []any{map[string]any{"type": "text", "text": ""}}
	}
	return blocks
}

func mapFinishReasonToAnthropic(fr string) string {
	switch fr {
	case "stop":
		return "end_turn"
	case "length":
		return "max_tokens"
	case "tool_calls":
		return "tool_use"
	case "":
		return "end_turn"
	default:
		return "end_turn"
	}
}

func convertToolChoiceAnthropicToOpenAI(tc any) any {
	tm, ok := tc.(map[string]any)
	if !ok {
		return "auto"
	}
	switch tm["type"] {
	case "auto":
		return "auto"
	case "none":
		return "none"
	case "any":
		return "required"
	case "tool":
		return map[string]any{
			"type": "function",
			"function": map[string]any{
				"name": tm["name"],
			},
		}
	default:
		return "auto"
	}
}

func budgetToEffort(budget int) string {
	if budget <= 0 {
		return "medium"
	}
	if budget <= 2048 {
		return "low"
	}
	if budget <= 16000 {
		return "medium"
	}
	return "high"
}
