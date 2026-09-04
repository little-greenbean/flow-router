package gateway

import (
	"math/rand"
	"sort"
	"strings"
	"time"

	"github.com/bejix/upstream-ops/backend/connector"
	"github.com/bejix/upstream-ops/backend/pkg/rateconvert"
	"github.com/bejix/upstream-ops/backend/storage"
)

// ScoredRoute 排序后的候选路由。
type ScoredRoute struct {
	Route         storage.GatewayRoute
	EffectiveRate float64
	BillingRate   float64
}

// RateForRoute 计算路由有效倍率（对齐同步账号 rateMultiplierForAccount）。
//
// 优先级：
//  1. custom → RateConvertValue
//  2. 能匹配到源分组 → 用分组 ratio 换算（实时）
//  3. 已保存的 BillingRateMultiplier（列表「账号计费倍率」）→ 避免拉分组失败时回落成 1，导致尝试顺序与列表不一致
//  4. 最后回落 Convert(1, mode, …)
func RateForRoute(route *storage.GatewayRoute, groups []connector.APIKeyGroup) float64 {
	if route == nil {
		return 1
	}
	mode := rateconvert.NormalizeMode(route.RateConvertMode)
	if mode == "custom" {
		return rateconvert.Convert(1, mode, route.RateConvertValue)
	}
	sourceGroupName := strings.TrimSpace(route.SourceGroupName)
	if route.SourceGroupID == nil && sourceGroupName == "" {
		if route.BillingRateMultiplier > 0 {
			return route.BillingRateMultiplier
		}
		return rateconvert.Convert(1, mode, route.RateConvertValue)
	}
	for _, g := range groups {
		if (route.SourceGroupID != nil && g.ID != nil && *g.ID == *route.SourceGroupID) ||
			(sourceGroupName != "" && strings.EqualFold(g.Name, sourceGroupName)) {
			return rateconvert.Convert(g.Ratio, mode, route.RateConvertValue)
		}
	}
	// 源分组未匹配到：用保存时的账号计费倍率，保证列表序 = 尝试序
	if route.BillingRateMultiplier > 0 {
		return route.BillingRateMultiplier
	}
	return rateconvert.Convert(1, mode, route.RateConvertValue)
}

// IsRouteSchedulable 是否可参与调度。
// 直连 provider 密钥在 GatewayProvider 上，路由本身可不存 SourceAPIKeyCipher。
func IsRouteSchedulable(route *storage.GatewayRoute, now time.Time) bool {
	if route == nil || !route.Enabled {
		return false
	}
	if route.TempUnschedulableUntil != nil && route.TempUnschedulableUntil.After(now) {
		return false
	}
	if route.NormalizeSourceKind() == storage.GatewayRouteSourceProvider {
		return route.GatewayProviderID > 0
	}
	if strings.TrimSpace(route.SourceAPIKeyCipher) == "" {
		return false
	}
	return true
}

// routeRateLess 比较两条路由优先级（与同步账号 sortAccountsForApply 一致）。
// direction: asc 低倍率优先；desc 高倍率优先。
// 同倍率：按 Position 排位；再比 id。（weight 用于 SortRoutes 加权分流，不参与排序）
func routeRateLess(a, b storage.GatewayRoute, rateA, rateB float64, desc bool) bool {
	if rateA != rateB {
		if desc {
			return rateA > rateB
		}
		return rateA < rateB
	}
	// 同价：优先级(Position)排位；weight 交给 SortRoutes 做加权分流
	if a.Position != b.Position {
		return a.Position < b.Position
	}
	return a.ID < b.ID
}

// OrderRoutesByRate 按倍率对全部路由重排（含禁用），用于列表展示与保存落库。
// 列表顺序 = 排序结果 = 尝试顺序。
func OrderRoutesByRate(routes []storage.GatewayRoute, groupsByChannel map[uint][]connector.APIKeyGroup, direction string) []storage.GatewayRoute {
	if len(routes) <= 1 {
		return routes
	}
	type scored struct {
		route storage.GatewayRoute
		rate  float64
		idx   int
	}
	items := make([]scored, len(routes))
	for i, r := range routes {
		cp := r
		groups := groupsByChannel[r.SourceChannelID]
		items[i] = scored{route: cp, rate: RateForRoute(&cp, groups), idx: i}
	}
	desc := strings.EqualFold(strings.TrimSpace(direction), "desc")
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].rate != items[j].rate || items[i].route.Position != items[j].route.Position {
			return routeRateLess(items[i].route, items[j].route, items[i].rate, items[j].rate, desc)
		}
		return items[i].idx < items[j].idx
	})
	out := make([]storage.GatewayRoute, len(items))
	for i := range items {
		out[i] = items[i].route
		out[i].Position = i
	}
	return out
}

// SortRoutes 按倍率方向分档 + 同价内按 Weight 加权随机分流（仅可调度路由，用于运行时选路/failover）。
// direction: asc 低倍率优先；desc 高倍率优先。
//
// BillingRate 即 RateForRoute 换算结果（原值 / ×100 / ÷100 / 自定义），
// 不再使用独立字段默认 1，避免计费失真。
func SortRoutes(routes []storage.GatewayRoute, groupsByChannel map[uint][]connector.APIKeyGroup, direction string, now time.Time, exclude map[uint]struct{}) []ScoredRoute {
	out := make([]ScoredRoute, 0, len(routes))
	for _, r := range routes {
		if exclude != nil {
			if _, ok := exclude[r.ID]; ok {
				continue
			}
		}
		cp := r
		if !IsRouteSchedulable(&cp, now) {
			continue
		}
		groups := groupsByChannel[r.SourceChannelID]
		rate := RateForRoute(&cp, groups)
		out = append(out, ScoredRoute{Route: cp, EffectiveRate: rate, BillingRate: rate})
	}
	desc := strings.EqualFold(strings.TrimSpace(direction), "desc")
	sort.SliceStable(out, func(i, j int) bool {
		return routeRateLess(out[i].Route, out[j].Route, out[i].EffectiveRate, out[j].EffectiveRate, desc)
	})
	// 同价区块内按 Weight 加权分流；out[0] 即一次加权抽签
	weightedShuffleByRate(out)
	return out
}

// weightedShuffleByRate 在每个「同价格」区块内按 Weight 做不放回加权抽样重排，
// 使 out[0] 成为一次加权抽签（P=wi/Σw），从而同价按权重分流；不同价区块顺序不变。
func weightedShuffleByRate(routes []ScoredRoute) {
	i := 0
	for i < len(routes) {
		j := i + 1
		for j < len(routes) && routes[j].EffectiveRate == routes[i].EffectiveRate {
			j++
		}
		if j-i > 1 {
			weightedPermute(routes[i:j])
		}
		i = j
	}
}

// weightedPermute 就地对同价区段做不放回加权抽样重排：
// 正权重路由按权重分布到前面，Weight<=0 的仅作末位 failover 兜底。
func weightedPermute(seg []ScoredRoute) {
	n := len(seg)
	for start := 0; start < n-1; start++ {
		total := 0
		for k := start; k < n; k++ {
			if w := seg[k].Route.Weight; w > 0 {
				total += w
			}
		}
		if total <= 0 {
			return
		}
		r := rand.Intn(total)
		pick, acc := start, 0
		for k := start; k < n; k++ {
			w := seg[k].Route.Weight
			if w <= 0 {
				continue
			}
			acc += w
			if r < acc {
				pick = k
				break
			}
		}
		seg[start], seg[pick] = seg[pick], seg[start]
	}
}
