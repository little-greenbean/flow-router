// 组模型清单 JSON 编解码、源标注与展示辅助。
package gateway

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/bejix/upstream-ops/backend/connector"
	"github.com/bejix/upstream-ops/backend/storage"
)

// ParseModelsJSON 解析组 models_json 为清单项。
func (svc *Service) ParseModelsJSON(raw string) []ModelListItem {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[]" || raw == "null" {
		return nil
	}
	var list []ModelListItem
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return nil
	}
	out := make([]ModelListItem, 0, len(list))
	for _, it := range list {
		id := strings.TrimSpace(it.ID)
		if id == "" {
			continue
		}
		src := strings.TrimSpace(it.Source)
		if src != "custom" && src != "catalog" {
			src = "sync"
		}
		item := ModelListItem{
			ID:         id,
			Source:     src,
			ChannelIDs: it.ChannelIDs,
			Sources:    svc.normalizeModelSources(it.Sources, it.ChannelIDs),
		}
		if len(item.ChannelIDs) == 0 && len(item.Sources) > 0 {
			item.ChannelIDs = svc.channelIDsFromSources(item.Sources)
		}
		out = append(out, item)
	}
	return out
}

func (svc *Service) normalizeModelSources(sources []ModelSource, channelIDs []uint) []ModelSource {
	if len(sources) > 0 {
		out := make([]ModelSource, 0, len(sources))
		seen := map[string]struct{}{}
		for _, s := range sources {
			if s.ChannelID == 0 && s.RouteID == 0 {
				continue
			}
			key := fmt.Sprintf("%d:%d:%v:%s", s.RouteID, s.ChannelID, s.SourceGroupID, s.SourceGroupName)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, s)
		}
		return out
	}
	// 兼容旧数据：仅有 channel_ids
	out := make([]ModelSource, 0, len(channelIDs))
	for _, id := range channelIDs {
		if id == 0 {
			continue
		}
		out = append(out, ModelSource{ChannelID: id})
	}
	return out
}

func (svc *Service) channelIDsFromSources(sources []ModelSource) []uint {
	seen := map[uint]struct{}{}
	out := make([]uint, 0, len(sources))
	for _, s := range sources {
		if s.ChannelID == 0 {
			continue
		}
		if _, ok := seen[s.ChannelID]; ok {
			continue
		}
		seen[s.ChannelID] = struct{}{}
		out = append(out, s.ChannelID)
	}
	return out
}

// ModelsJSONString 将清单项编码为 JSON 字符串。
func (svc *Service) ModelsJSONString(list []ModelListItem) string {
	if len(list) == 0 {
		return "[]"
	}
	raw, err := json.Marshal(list)
	if err != nil {
		return "[]"
	}
	return string(raw)
}

func (svc *Service) formatChannelGroupLabel(channelName, groupName string, channelID uint) string {
	ch := strings.TrimSpace(channelName)
	if ch == "" {
		ch = fmt.Sprintf("#%d", channelID)
	}
	g := strings.TrimSpace(groupName)
	// 仅有 id:N 占位时不拼进标签（与前端 channelGroupLabel 一致）
	if g == "" || svc.isSourceGroupIDPlaceholder(g) {
		return ch
	}
	return ch + "-" + g
}

// isSourceGroupIDPlaceholder 识别「id:31」「源 ID: 31」这类无真实分组名的占位。
func (svc *Service) isSourceGroupIDPlaceholder(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	// 与前端 parseSourceGroupIDRef 对齐：id:N / 源 ID: N
	lower := strings.ToLower(strings.ReplaceAll(s, " ", ""))
	lower = strings.ReplaceAll(lower, "：", ":")
	if strings.HasPrefix(lower, "id:") {
		rest := strings.TrimPrefix(lower, "id:")
		return rest != "" && svc.isAllASCIIDigits(rest)
	}
	if strings.HasPrefix(lower, "源id:") {
		rest := strings.TrimPrefix(lower, "源id:")
		return rest != "" && svc.isAllASCIIDigits(rest)
	}
	return false
}

func (svc *Service) isAllASCIIDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// enrichRouteSourceGroupName 用上游分组列表补全路由上的源分组显示名。
// 历史前端在有 remote_group_id 时会清空 name，导致 sub2api 只剩 id。
func (svc *Service) enrichRouteSourceGroupName(route *storage.GatewayRoute, groups []connector.APIKeyGroup) {
	if route == nil {
		return
	}
	name := strings.TrimSpace(route.SourceGroupName)
	if name != "" && !svc.isSourceGroupIDPlaceholder(name) {
		return
	}
	if route.SourceGroupID == nil || *route.SourceGroupID <= 0 {
		return
	}
	want := *route.SourceGroupID
	for _, g := range groups {
		if g.ID == nil || *g.ID != want {
			continue
		}
		if n := strings.TrimSpace(g.Name); n != "" {
			route.SourceGroupName = n
		}
		return
	}
}

func (svc *Service) truncateProbeError(body []byte, max int) string {
	if len(body) == 0 {
		return ""
	}
	// 尝试解析 OpenAI / Anthropic 错误结构
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err == nil {
		if errObj, ok := payload["error"].(map[string]interface{}); ok {
			if m, ok := errObj["message"].(string); ok && strings.TrimSpace(m) != "" {
				return clipString(strings.TrimSpace(m), max)
			}
		}
		if t, ok := payload["type"].(string); ok && t == "error" {
			if errObj, ok := payload["error"].(map[string]interface{}); ok {
				if m, ok := errObj["message"].(string); ok {
					return clipString(strings.TrimSpace(m), max)
				}
			}
		}
	}
	s := strings.TrimSpace(string(body))
	return clipString(s, max)
}
