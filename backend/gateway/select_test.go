package gateway

import (
	"testing"
	"time"

	"github.com/bejix/upstream-ops/backend/connector"
	"github.com/bejix/upstream-ops/backend/storage"
)

func TestSortRoutes_RateThenWeight(t *testing.T) {
	now := time.Now()
	routes := []storage.GatewayRoute{
		{ID: 1, SourceChannelID: 1, Position: 0, Weight: 1, Enabled: true, RateConvertMode: "custom", RateConvertValue: 0.5, SourceAPIKeyCipher: "x", BillingRateMultiplier: 1},
		{ID: 2, SourceChannelID: 2, Position: 1, Weight: 10, Enabled: true, RateConvertMode: "custom", RateConvertValue: 0.2, SourceAPIKeyCipher: "x", BillingRateMultiplier: 1},
		{ID: 3, SourceChannelID: 3, Position: 2, Weight: 5, Enabled: true, RateConvertMode: "custom", RateConvertValue: 0.2, SourceAPIKeyCipher: "x", BillingRateMultiplier: 1},
	}
	got := SortRoutes(routes, nil, "asc", now, nil)
	if len(got) != 3 {
		t.Fatalf("len=%d", len(got))
	}
	// rate 0.2 档在前（2、3 加权随机任意序），rate 0.5 档(1) 恒在最后
	if got[2].Route.ID != 1 {
		t.Fatalf("rate 0.5 route should be last, got %v", got[2].Route.ID)
	}
	if !((got[0].Route.ID == 2 && got[1].Route.ID == 3) || (got[0].Route.ID == 3 && got[1].Route.ID == 2)) {
		t.Fatalf("rate 0.2 tier wrong: %v %v", got[0].Route.ID, got[1].Route.ID)
	}
}

func TestOrderRoutesByRate_MatchesAttemptOrder(t *testing.T) {
	routes := []storage.GatewayRoute{
		{ID: 10, Position: 0, Weight: 1, Enabled: true, RateConvertMode: "custom", RateConvertValue: 1, SourceAPIKeyCipher: "x"},
		{ID: 11, Position: 1, Weight: 1, Enabled: true, RateConvertMode: "custom", RateConvertValue: 0.05, SourceAPIKeyCipher: "x"},
	}
	ordered := OrderRoutesByRate(routes, nil, "asc")
	if len(ordered) != 2 || ordered[0].ID != 11 || ordered[1].ID != 10 {
		t.Fatalf("asc order=%+v", ordered)
	}
	if ordered[0].Position != 0 || ordered[1].Position != 1 {
		t.Fatalf("positions=%d %d", ordered[0].Position, ordered[1].Position)
	}
	// 禁用路由也参与落库排序
	routes[0].Enabled = false
	ordered = OrderRoutesByRate(routes, nil, "asc")
	if ordered[0].ID != 11 || ordered[1].ID != 10 {
		t.Fatalf("disabled still sorted by rate: %+v", ordered)
	}
}

func TestSortRoutes_TempPauseAndExclude(t *testing.T) {
	now := time.Now()
	until := now.Add(time.Minute)
	routes := []storage.GatewayRoute{
		{ID: 1, SourceChannelID: 1, Position: 0, Weight: 1, Enabled: true, RateConvertMode: "custom", RateConvertValue: 0.1, SourceAPIKeyCipher: "x", TempUnschedulableUntil: &until},
		{ID: 2, SourceChannelID: 2, Position: 1, Weight: 1, Enabled: true, RateConvertMode: "custom", RateConvertValue: 0.2, SourceAPIKeyCipher: "x"},
		{ID: 3, SourceChannelID: 3, Position: 2, Weight: 1, Enabled: false, RateConvertMode: "custom", RateConvertValue: 0.05, SourceAPIKeyCipher: "x"},
	}
	got := SortRoutes(routes, nil, "asc", now, map[uint]struct{}{2: {}})
	if len(got) != 0 {
		t.Fatalf("expected empty, got %d", len(got))
	}
	got = SortRoutes(routes, nil, "asc", now, nil)
	if len(got) != 1 || got[0].Route.ID != 2 {
		t.Fatalf("got=%+v", got)
	}
}

func TestRateForRoute_GroupRatio(t *testing.T) {
	gid := int64(9)
	route := &storage.GatewayRoute{
		SourceGroupID:    &gid,
		RateConvertMode:  "multiply_100",
		RateConvertValue: 1,
	}
	groups := []connector.APIKeyGroup{
		{ID: &gid, Name: "default", Ratio: 0.05},
	}
	got := RateForRoute(route, groups)
	if got != 5 {
		t.Fatalf("got %v want 5", got)
	}
}

func TestRateForRoute_FallbackBillingMultiplier(t *testing.T) {
	// 列表显示 0.05 且已落库，但运行时拉不到源分组时不应变成 1
	route := &storage.GatewayRoute{
		SourceGroupName:       "grok",
		RateConvertMode:       "raw",
		BillingRateMultiplier: 0.05,
	}
	got := RateForRoute(route, nil)
	if got != 0.05 {
		t.Fatalf("got %v want 0.05", got)
	}
}

func TestOrderRoutesByRate_SameRateByPosition(t *testing.T) {
	routes := []storage.GatewayRoute{
		{ID: 1, Position: 0, Weight: 1, Enabled: true, RateConvertMode: "custom", RateConvertValue: 0.05, SourceAPIKeyCipher: "x"},
		{ID: 2, Position: 1, Weight: 99, Enabled: true, RateConvertMode: "custom", RateConvertValue: 0.05, SourceAPIKeyCipher: "x"},
	}
	// 同倍率现按 Position(优先级)排位：pos0 的 ID1 在前，与 weight 无关
	ordered := OrderRoutesByRate(routes, nil, "asc")
	if ordered[0].ID != 1 || ordered[1].ID != 2 {
		t.Fatalf("want position order [1,2], got ids %d %d", ordered[0].ID, ordered[1].ID)
	}
}

func TestResolveModel(t *testing.T) {
	up, chain := ResolveModel("claude-opus", map[string]string{"claude-opus": "claude-sonnet"})
	if up != "claude-sonnet" || chain != "claude-opus->claude-sonnet" {
		t.Fatalf("up=%s chain=%s", up, chain)
	}
	up, chain = ResolveModel("foo", map[string]string{"*": "bar"})
	if up != "bar" || chain != "foo->bar" {
		t.Fatalf("up=%s chain=%s", up, chain)
	}
}

func TestCalculateCost(t *testing.T) {
	p := ModelPricing{InputPricePerToken: 0.001, OutputPricePerToken: 0.002}
	// 原值倍率 0.06：actual = base × 0.06（只乘一次账号计费倍率）
	c := CalculateCost(p, UsageTokens{InputTokens: 10, OutputTokens: 5}, 0.06, 0.06)
	// base = 10*0.001 + 5*0.002 = 0.02; actual = 0.0012
	if c.TotalCost != 0.02 || c.ActualCost != 0.0012 {
		t.Fatalf("%+v", c)
	}
	// billing 无效时回退 rate
	c2 := CalculateCost(p, UsageTokens{InputTokens: 10, OutputTokens: 5}, 2, 0)
	if c2.ActualCost != 0.04 {
		t.Fatalf("fallback actual=%v want 0.04", c2.ActualCost)
	}
}

func TestPricingCatalog_Grok45Fallback(t *testing.T) {
	cat := NewPricingCatalog(nil)
	p := cat.Resolve("grok-4.5")
	// sub2api: $2 / $6 per MTok
	if p.InputPricePerToken != 2e-6 || p.OutputPricePerToken != 6e-6 {
		t.Fatalf("grok-4.5 pricing = %+v", p)
	}
	cost := CalculateCost(p, UsageTokens{InputTokens: 2693, OutputTokens: 49}, 0.05, 0.05)
	if cost.TotalCost <= 0 || cost.ActualCost <= 0 {
		t.Fatalf("expected non-zero cost, got %+v", cost)
	}
}

func TestPricingCatalog_KnownLiteLLMModel(t *testing.T) {
	cat := NewPricingCatalog(nil)
	p := cat.Resolve("claude-sonnet-4-5")
	if !p.HasTokenPrice() {
		t.Fatalf("expected litellm price for claude-sonnet-4-5, got %+v", p)
	}
}

func TestPricingCatalog_DeepSeekModule(t *testing.T) {
	cat := NewPricingCatalog(nil)
	// 系统默认价应覆盖官方 DeepSeek 主型号
	for _, name := range []string{
		"deepseek-chat",
		"deepseek-reasoner",
		"deepseek-v3",
		"deepseek-v3.2",
		"deepseek-r1",
		"deepseek-coder",
		"deepseek-v4-flash",
		"deepseek-v4-pro",
		"deepseek/deepseek-chat",
	} {
		p := cat.Resolve(name)
		if !p.HasTokenPrice() {
			t.Fatalf("expected price for %s, got %+v", name, p)
		}
	}
	// 带前缀 / 变体后缀走家族回退
	p := cat.Resolve("deepseek/deepseek-v4-flash-experimental")
	if p.InputPricePerToken != 1.4e-7 || p.OutputPricePerToken != 2.8e-7 {
		t.Fatalf("v4-flash family fallback = %+v", p)
	}
	// 列表可见 deepseek 条目
	items := cat.ListDefaults("deepseek")
	if len(items) < 10 {
		t.Fatalf("ListDefaults(deepseek) too few: %d", len(items))
	}
}

func TestRateForRoute_RawIsSourceRatio(t *testing.T) {
	gid := int64(3)
	route := &storage.GatewayRoute{
		SourceGroupID:   &gid,
		RateConvertMode: "raw",
	}
	groups := []connector.APIKeyGroup{
		{ID: &gid, Name: "pro", Ratio: 0.06},
	}
	got := RateForRoute(route, groups)
	if got != 0.06 {
		t.Fatalf("raw mode should keep source ratio, got %v want 0.06", got)
	}
}

func TestSortRoutes_WeightedSplit(t *testing.T) {
	now := time.Now()
	routes := []storage.GatewayRoute{
		{ID: 1, SourceChannelID: 1, Position: 0, Weight: 3, Enabled: true, RateConvertMode: "custom", RateConvertValue: 0.1, SourceAPIKeyCipher: "x", BillingRateMultiplier: 1},
		{ID: 2, SourceChannelID: 2, Position: 1, Weight: 1, Enabled: true, RateConvertMode: "custom", RateConvertValue: 0.1, SourceAPIKeyCipher: "x", BillingRateMultiplier: 1},
	}
	const N = 20000
	first := map[uint]int{}
	for i := 0; i < N; i++ {
		got := SortRoutes(routes, nil, "asc", now, nil)
		if len(got) != 2 {
			t.Fatalf("len=%d", len(got))
		}
		first[got[0].Route.ID]++
	}
	// 权重 3:1 → ID1 首选概率 ~0.75
	ratio := float64(first[1]) / float64(N)
	if ratio < 0.72 || ratio > 0.78 {
		t.Fatalf("weight 3:1 expect ~0.75 first-pick for ID1, got %.3f (counts %v)", ratio, first)
	}
}
