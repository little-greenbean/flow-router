package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/bejix/upstream-ops/backend/storage"
)

func TestSyncCatalogModelsAppliesExplicitRoutesAndHybridMode(t *testing.T) {
	fixture, err := os.ReadFile("testdata/catalog-models.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture)
	}))
	t.Cleanup(server.Close)
	t.Setenv("UPSTREAM_OPS_MODEL_CATALOG_URL", server.URL)

	db := openGatewayTestDB(t)
	groups := storage.NewGatewayGroups(db)
	routes := storage.NewGatewayRoutes(db)
	group := &storage.GatewayGroup{
		Name:       "catalog-sync",
		Status:     storage.GatewayGroupStatusActive,
		ModelsMode: storage.GatewayModelsModeAuto,
		ModelsJSON: `[{"id":"legacy","source":"sync","channel_ids":[9]},{"id":"manual","source":"custom"}]`,
	}
	if err := groups.Create(group); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := routes.SaveForGroup(group.ID, []storage.GatewayRoute{
		{SourceKind: storage.GatewayRouteSourceMonitor, SourceChannelID: 55, Enabled: true, Weight: 1},
		{SourceKind: storage.GatewayRouteSourceProvider, GatewayProviderID: 8, Enabled: true, Weight: 1},
	}); err != nil {
		t.Fatalf("save routes: %v", err)
	}
	savedRoutes, err := routes.ListByGroupID(group.ID)
	if err != nil || len(savedRoutes) != 2 {
		t.Fatalf("list routes: %v %#v", err, savedRoutes)
	}

	svc := NewService(groups, storage.NewGatewayKeys(db), routes, nil, nil, nil, nil, nil, nil)
	result, err := svc.SyncCatalogModels(context.Background(), group.ID, CatalogSyncInput{
		Models:   []string{"gpt-5.6"},
		RouteIDs: []uint{savedRoutes[0].ID, savedRoutes[1].ID},
	})
	if err != nil {
		t.Fatalf("sync catalog: %v", err)
	}
	if !result.ModeChanged || result.Group.ModelsMode != storage.GatewayModelsModeHybrid {
		t.Fatalf("expected hybrid mode change, got %#v", result)
	}
	items := svc.ParseModelsJSON(result.Group.ModelsJSON)
	if len(items) != 3 {
		t.Fatalf("expected preserved entries plus catalog model, got %#v", items)
	}
	var catalog ModelListItem
	for _, item := range items {
		if item.ID == "gpt-5.6" {
			catalog = item
		}
	}
	if catalog.Source != "catalog" || len(catalog.Sources) != 2 {
		t.Fatalf("unexpected catalog model: %#v", catalog)
	}
	if catalog.Sources[0].RouteID != savedRoutes[0].ID || catalog.Sources[1].RouteID != savedRoutes[1].ID {
		t.Fatalf("unexpected route bindings: %#v", catalog.Sources)
	}
}

func TestSyncCatalogModelsRejectsDisabledRouteWithoutPersistence(t *testing.T) {
	db := openGatewayTestDB(t)
	groups := storage.NewGatewayGroups(db)
	routes := storage.NewGatewayRoutes(db)
	group := &storage.GatewayGroup{
		Name:       "catalog-disabled",
		Status:     storage.GatewayGroupStatusActive,
		ModelsMode: storage.GatewayModelsModeAuto,
		ModelsJSON: `[{"id":"legacy","source":"sync"}]`,
	}
	if err := groups.Create(group); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := routes.SaveForGroup(group.ID, []storage.GatewayRoute{{
		SourceKind: storage.GatewayRouteSourceMonitor, SourceChannelID: 55, Enabled: false, Weight: 1,
	}}); err != nil {
		t.Fatalf("save routes: %v", err)
	}
	savedRoutes, _ := routes.ListByGroupID(group.ID)
	savedRoutes[0].Enabled = false
	if err := routes.Update(&savedRoutes[0]); err != nil {
		t.Fatalf("disable route: %v", err)
	}
	svc := NewService(groups, storage.NewGatewayKeys(db), routes, nil, nil, nil, nil, nil, nil)
	if _, err := svc.SyncCatalogModels(context.Background(), group.ID, CatalogSyncInput{
		Models: []string{"gpt-5.6"}, RouteIDs: []uint{savedRoutes[0].ID},
	}); err == nil {
		t.Fatal("expected disabled route validation error")
	}
	reloaded, err := groups.FindByID(group.ID)
	if err != nil {
		t.Fatalf("reload group: %v", err)
	}
	if reloaded.ModelsJSON != group.ModelsJSON || reloaded.ModelsMode != storage.GatewayModelsModeAuto {
		t.Fatalf("group changed after validation failure: %#v", reloaded)
	}
}
