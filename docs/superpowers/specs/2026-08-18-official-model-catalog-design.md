# Official Model Catalog Sync Design

## Goal

Allow a Gateway Group to import model definitions from the same official catalog used by Sub2API, bind each imported model to selected routes, expose those models to downstream clients, and prevent runtime failover to unbound routes.

## Context

Some Sub2API-compatible upstreams support newer models such as `gpt-5.6` but omit them from their per-key `/v1/models` response. UpstreamOps currently discovers models only from each route's `/v1/models`, so these models cannot be represented in the group model mapping.

The Sub2API/new-api implementation uses the public metadata catalog at `https://basellm.github.io/llm-metadata/api/newapi/models.json`. That catalog describes model IDs and metadata; it does not prove a route can serve a model.

## Design

### Catalog sync

Add a Gateway model mapping action named `同步官方模型目录`. The backend fetches the official catalog with a bounded timeout and retry policy, normalizes model IDs, and returns model metadata for search/selection. The catalog URL is configurable through an environment variable with the Sub2API URL as the default so local tests can use a fixture.

The UI presents a searchable selection dialog. Existing group models remain unchanged until the user confirms the selection.

### Route binding

The confirmation request contains selected model IDs and route IDs. Each selected model is stored in the group's `models_json` with a catalog source marker and `sources` entries containing the selected route IDs. Existing `sync` and `custom` entries are preserved. Re-selecting an existing catalog model updates only its selected route sources.

If the group is in `auto` model mode, the catalog sync changes it to `hybrid` so downstream `/v1/models` includes catalog entries while retaining live upstream aggregation. This change is shown in the confirmation/result state.

### Runtime route filtering

When a group model entry has explicit route sources, runtime candidate routes are filtered to those route IDs after normal enabled/pause checks. Groups/models without explicit sources retain the existing all-route behavior for backward compatibility. This filter applies to forwarding and public model aggregation. Model mapping resolution still determines the upstream model name after the route has been selected.

Catalog entries with no selected routes are rejected by the backend. Existing custom entries with no sources keep their current behavior.

### Failure handling

- Catalog fetch failure returns a clear error and does not modify the group.
- Invalid or duplicate model IDs are normalized and de-duplicated.
- Unknown/disabled route IDs are rejected; no partial group update is written.
- The UI reports imported models, route bindings, and any auto-mode-to-hybrid change.
- The feature does not call upstream model inference or send billable probe requests during catalog sync. Per-route testing remains an explicit user action.

## Verification

- Unit tests cover catalog decoding, model merge preservation, route-source filtering, disabled/unknown route validation, and auto-to-hybrid behavior.
- Frontend tests/build/lint verify the selection dialog and API payload typing.
- Local integration verification starts the existing development stack, imports `gpt-5.6` from a fixture, binds selected routes, checks `/v1/models`, and verifies an unbound route is excluded from candidate selection.

