package gateway

import (
	"testing"

	"github.com/bejix/upstream-ops/backend/storage"
)

func TestMergeCatalogModelsPreservesExistingSources(t *testing.T) {
	existing := []ModelListItem{
		{ID: "legacy-model", Source: "sync", ChannelIDs: []uint{9}},
		{ID: "manual-model", Source: "custom"},
		{ID: "gpt-5.6", Source: "sync", ChannelIDs: []uint{8}},
	}
	selected := []CatalogModel{{ID: "gpt-5.6"}, {ID: "claude-4"}, {ID: "gpt-5.6"}}
	sources := []ModelSource{{RouteID: 2, ChannelID: 55, ChannelName: "55"}, {RouteID: 3, ChannelID: 0, ChannelName: "Walk"}}

	merged := mergeCatalogModels(existing, selected, sources)
	if len(merged) != 4 {
		t.Fatalf("expected four unique models, got %d: %#v", len(merged), merged)
	}
	var official ModelListItem
	for _, item := range merged {
		if item.ID == "gpt-5.6" {
			official = item
		}
	}
	if official.Source != "catalog" {
		t.Fatalf("expected catalog source, got %#v", official)
	}
	if len(official.Sources) != 2 || official.Sources[0].RouteID != 2 || official.Sources[1].RouteID != 3 {
		t.Fatalf("expected explicit route bindings, got %#v", official.Sources)
	}
	if official.ChannelIDs[0] != 55 {
		t.Fatalf("expected channel id derived from source, got %#v", official.ChannelIDs)
	}
	for _, item := range merged {
		if item.ID == "legacy-model" && item.Source != "sync" {
			t.Fatalf("legacy entry changed: %#v", item)
		}
	}
}

func TestValidateCatalogRouteIDsRejectsUnknownAndDisabled(t *testing.T) {
	routes := []storage.GatewayRoute{
		{ID: 2, GatewayGroupID: 7, Enabled: true},
		{ID: 3, GatewayGroupID: 7, Enabled: false},
	}
	if err := validateCatalogRouteIDs([]uint{2}, routes); err != nil {
		t.Fatalf("enabled route should pass: %v", err)
	}
	if err := validateCatalogRouteIDs([]uint{99}, routes); err == nil {
		t.Fatal("unknown route should fail")
	}
	if err := validateCatalogRouteIDs([]uint{3}, routes); err == nil {
		t.Fatal("disabled route should fail")
	}
}
