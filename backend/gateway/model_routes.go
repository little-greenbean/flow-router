package gateway

import (
	"fmt"
	"strings"

	"github.com/bejix/upstream-ops/backend/storage"
)

// mergeCatalogModels replaces selected IDs with explicit catalog entries while
// retaining all other saved sync/custom/catalog entries and their order.
func mergeCatalogModels(existing []ModelListItem, selected []CatalogModel, sources []ModelSource) []ModelListItem {
	out := make([]ModelListItem, 0, len(existing)+len(selected))
	index := make(map[string]int, len(existing)+len(selected))
	for _, item := range existing {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		if _, exists := index[id]; exists {
			continue
		}
		item.ID = id
		index[id] = len(out)
		out = append(out, item)
	}
	for _, model := range selected {
		id := strings.TrimSpace(model.ID)
		if id == "" {
			continue
		}
		item := ModelListItem{
			ID:         id,
			Source:     "catalog",
			ChannelIDs: channelIDsFromModelSources(sources),
			Sources:    append([]ModelSource(nil), sources...),
		}
		if i, ok := index[id]; ok {
			out[i] = item
			continue
		}
		index[id] = len(out)
		out = append(out, item)
	}
	return out
}

func channelIDsFromModelSources(sources []ModelSource) []uint {
	seen := make(map[uint]struct{}, len(sources))
	out := make([]uint, 0, len(sources))
	for _, source := range sources {
		if source.ChannelID == 0 {
			continue
		}
		if _, ok := seen[source.ChannelID]; ok {
			continue
		}
		seen[source.ChannelID] = struct{}{}
		out = append(out, source.ChannelID)
	}
	return out
}

func validateCatalogRouteIDs(routeIDs []uint, routes []storage.GatewayRoute) error {
	if len(routeIDs) == 0 {
		return fmt.Errorf("at least one route is required")
	}
	byID := make(map[uint]storage.GatewayRoute, len(routes))
	for _, route := range routes {
		byID[route.ID] = route
	}
	seen := make(map[uint]struct{}, len(routeIDs))
	for _, id := range routeIDs {
		if id == 0 {
			return fmt.Errorf("route id must be positive")
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		route, ok := byID[id]
		if !ok {
			return fmt.Errorf("route %d does not belong to this group", id)
		}
		if !route.Enabled {
			return fmt.Errorf("route %d is disabled", id)
		}
	}
	return nil
}

// filterRoutesForModel applies explicit route bindings when the saved model
// entry has route IDs. Entries from older sync/custom data without route IDs
// deliberately keep the legacy all-route behavior.
func filterRoutesForModel(routes []storage.GatewayRoute, items []ModelListItem, model string) []storage.GatewayRoute {
	model = strings.TrimSpace(model)
	allowed := make(map[uint]struct{})
	explicit := false
	for _, item := range items {
		if strings.TrimSpace(item.ID) != model {
			continue
		}
		for _, source := range item.Sources {
			if source.RouteID == 0 {
				continue
			}
			explicit = true
			allowed[source.RouteID] = struct{}{}
		}
		break
	}
	if !explicit {
		return append([]storage.GatewayRoute(nil), routes...)
	}
	out := make([]storage.GatewayRoute, 0, len(allowed))
	for _, route := range routes {
		if _, ok := allowed[route.ID]; ok {
			out = append(out, route)
		}
	}
	return out
}

func routeAllowsModel(items []ModelListItem, routeID uint, model string) bool {
	filtered := filterRoutesForModel([]storage.GatewayRoute{{ID: routeID}}, items, model)
	return len(filtered) == 1 && filtered[0].ID == routeID
}
