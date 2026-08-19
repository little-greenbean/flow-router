package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bejix/upstream-ops/backend/storage"
	"github.com/gin-gonic/gin"
)

func TestGatewayDispatchStats(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openTestDB(t)
	usage := storage.NewGatewayUsageLogs(db)
	now := time.Now().UTC()
	if err := usage.Create(&storage.GatewayUsageLog{
		GatewayGroupID: 1,
		RouteID:        2,
		RequestID:      "dispatch-api-1",
		Success:        true,
		CreatedAt:      now.Add(-time.Minute),
	}); err != nil {
		t.Fatalf("create usage: %v", err)
	}

	r := gin.New()
	r.GET("/api/gateway/dispatch/stats", func(c *gin.Context) {
		statsGatewayDispatch(c, &Deps{GatewayUsage: usage})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/gateway/dispatch/stats?window=5m", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Window string                              `json:"window"`
		From   string                              `json:"from"`
		To     string                              `json:"to"`
		Groups []storage.GatewayDispatchStatsGroup `json:"groups"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Window != "5m" || got.From == "" || got.To == "" || len(got.Groups) != 1 {
		t.Fatalf("unexpected response: %+v", got)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/gateway/dispatch/stats?window=2m", nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid window status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestGatewayDispatchStatsUsesSourceNamesAndCostOrder(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openTestDB(t)
	usage := storage.NewGatewayUsageLogs(db)
	now := time.Now().UTC()
	if err := db.Create(&storage.GatewayGroup{ID: 1, Name: "GPT Pro 网关组"}).Error; err != nil {
		t.Fatalf("create gateway group: %v", err)
	}
	if err := db.Create(&storage.GatewayRoute{
		ID:                    1,
		GatewayGroupID:        1,
		Position:              1,
		SourceAPIKeyName:      "贵的来源",
		SourceGroupName:       "高价分组",
		BillingRateMultiplier: 1.4,
	}).Error; err != nil {
		t.Fatalf("create expensive route: %v", err)
	}
	if err := db.Create(&storage.GatewayRoute{
		ID:                    2,
		GatewayGroupID:        1,
		Position:              0,
		SourceAPIKeyName:      "便宜的来源",
		SourceGroupName:       "低价分组",
		BillingRateMultiplier: 0.8,
	}).Error; err != nil {
		t.Fatalf("create cheap route: %v", err)
	}
	for _, item := range []storage.GatewayUsageLog{
		{GatewayGroupID: 1, RouteID: 1, SourceAPIKeyName: "贵的来源", SourceGroupName: "高价分组", BillingRateMultiplier: 1.4, RequestID: "cost-order-expensive", Success: true, CreatedAt: now.Add(-time.Minute)},
		{GatewayGroupID: 1, RouteID: 2, SourceAPIKeyName: "便宜的来源", SourceGroupName: "低价分组", BillingRateMultiplier: 0.8, RequestID: "cost-order-cheap", Success: true, CreatedAt: now.Add(-time.Minute)},
		{GatewayGroupID: 1, RouteID: 99, SourceAPIKeyName: "z-旧来源", SourceGroupName: "a-旧分组", BillingRateMultiplier: 2, RequestID: "deleted-route-old", Success: true, CreatedAt: now.Add(-2 * time.Minute)},
		{GatewayGroupID: 1, RouteID: 99, SourceAPIKeyName: "a-最新来源", SourceGroupName: "z-最新分组", BillingRateMultiplier: 2, RequestID: "deleted-route-latest", Success: true, CreatedAt: now.Add(-time.Minute)},
	} {
		if err := usage.Create(&item); err != nil {
			t.Fatalf("create usage: %v", err)
		}
	}

	groups, err := usage.DispatchStats(now.Add(-5*time.Minute), now.Add(time.Minute))
	if err != nil {
		t.Fatalf("dispatch stats: %v", err)
	}
	if len(groups) != 1 || len(groups[0].Routes) != 3 {
		t.Fatalf("unexpected groups: %+v", groups)
	}
	got := groups[0].Routes
	if got[0].SourceAPIKeyName != "便宜的来源" || got[0].SourceGroupName != "低价分组" || got[0].BillingRateMultiplier != 0.8 || !got[0].RouteAvailable {
		t.Fatalf("cheap route metadata/order = %+v", got[0])
	}
	if got[1].SourceAPIKeyName != "贵的来源" || got[1].SourceGroupName != "高价分组" || got[1].BillingRateMultiplier != 1.4 || !got[1].RouteAvailable {
		t.Fatalf("expensive route metadata/order = %+v", got[1])
	}
	if got[2].SourceAPIKeyName != "a-最新来源" || got[2].SourceGroupName != "z-最新分组" || got[2].BillingRateMultiplier != 2 || got[2].RouteAvailable {
		t.Fatalf("deleted route latest snapshot = %+v", got[2])
	}
}
