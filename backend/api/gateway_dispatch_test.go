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
