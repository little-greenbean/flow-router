# Official Model Catalog Sync Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add official Sub2API-compatible model catalog sync with explicit route binding and model-aware runtime filtering.

**Architecture:** A small backend catalog client fetches and normalizes `models.json`; Gateway Group APIs merge selected catalog entries into `models_json` and validate route IDs. Runtime candidate selection filters only models with explicit sources, preserving legacy behavior for existing entries. The frontend adds a searchable selection dialog inside the existing model mapping panel.

**Tech Stack:** Go, Gin, GORM storage, React/TypeScript, existing shadcn-style UI, Vitest/Jest-free Go tests, pnpm lint/build.

---

### Task 1: Add catalog types and fetcher

**Files:**
- Create: `backend/gateway/catalog.go`
- Test: `backend/gateway/catalog_test.go`
- Modify: `backend/config/config.go` only if a shared environment default helper is needed

- [ ] **Step 1: Write failing catalog parsing tests**

Add tests for both the Sub2API object shape (`{"data":[...]}`) and a bare model array, preserving `model_name`, description, vendor, tags, status, and de-duplicating blank IDs.

- [ ] **Step 2: Run the focused tests and verify failure**

Run `go test ./backend/gateway -run 'TestCatalog' -count=1` from the repository root. Expected: compilation/test failure because catalog types and parser do not exist.

- [ ] **Step 3: Implement the bounded catalog client**

Implement:

```go
type CatalogModel struct {
    ID          string `json:"model_name"`
    Description string `json:"description"`
    Icon        string `json:"icon"`
    Tags        string `json:"tags"`
    Vendor      string `json:"vendor_name"`
    Status      int    `json:"status"`
}

func FetchOfficialModelCatalog(ctx context.Context) ([]CatalogModel, error)
func parseCatalogBody(body []byte) ([]CatalogModel, error)
```

Use `UPSTREAM_OPS_MODEL_CATALOG_URL` with default `https://basellm.github.io/llm-metadata/api/newapi/models.json`, a 15-second request timeout, a 10 MiB body limit, and three attempts with short exponential backoff. Never log response bodies or credentials.

- [ ] **Step 4: Run focused tests and formatting**

Run `gofmt -w backend/gateway/catalog.go backend/gateway/catalog_test.go` and `go test ./backend/gateway -run 'TestCatalog' -count=1`. Expected: PASS.

### Task 2: Add group catalog preview/apply API

**Files:**
- Modify: `backend/gateway/admin_types.go`
- Modify: `backend/gateway/admin_models.go`
- Modify: `backend/api/gateway_admin.go`
- Modify: `backend/storage/model.go` only if a JSON source type needs a documented constant
- Test: `backend/gateway/admin_models_test.go` or the nearest existing gateway admin test file

- [ ] **Step 1: Write failing merge and validation tests**

Cover: selected catalog models merge without deleting existing `sync`/`custom` entries; duplicate IDs collapse; unknown route IDs and disabled route IDs fail without persistence; selected routes are stored as `ModelSource{RouteID: ...}`; an `auto` group becomes `hybrid`.

- [ ] **Step 2: Run focused tests and verify failure**

Run `go test ./backend/gateway -run 'Test(.*Catalog|.*Model.*Merge|.*Route.*Source)' -count=1`. Expected: FAIL before the new service methods exist.

- [ ] **Step 3: Implement preview/apply service methods**

Add typed inputs/results such as:

```go
type CatalogSyncInput struct {
    Models   []string `json:"models"`
    RouteIDs []uint   `json:"route_ids"`
}
```

Add a read-only catalog endpoint and a group apply endpoint. The apply method must load the group and all routes, validate every selected route as enabled and belonging to the group, merge `source:"catalog"` entries into `models_json`, preserve existing entries, switch `models_mode` from `auto` to `hybrid`, update the group once, and invalidate the runtime model cache.

- [ ] **Step 4: Register the API routes**

Register:

```text
GET  /api/gateway/models/catalog
POST /api/gateway/groups/:id/models/catalog-sync
```

Return typed JSON with selected model IDs, route bindings, and whether `models_mode` changed.

- [ ] **Step 5: Run tests and formatting**

Run `gofmt -w backend/gateway backend/api/gateway_admin.go` and the focused Go tests. Expected: PASS.

### Task 3: Filter runtime routes by explicit model sources

**Files:**
- Create: `backend/gateway/model_routes.go`
- Test: `backend/gateway/model_routes_test.go`
- Modify: `backend/gateway/runtime_forward.go`
- Modify: `backend/gateway/runtime_public.go`
- Modify: `backend/gateway/runtime_ws.go` if its route candidate construction bypasses the shared helper

- [ ] **Step 1: Write failing route-filter tests**

Test that a model entry with route IDs `[1,4]` returns only those routes, disabled/paused routes are still excluded, and a model entry with no explicit sources preserves the existing all-route candidate set.

- [ ] **Step 2: Run focused tests and verify failure**

Run `go test ./backend/gateway -run 'TestModelRoute' -count=1`. Expected: FAIL before the helper exists.

- [ ] **Step 3: Implement the compatibility-aware filter**

Add a helper that parses the group model entry by requested model and filters only when that entry has non-empty `sources` containing route IDs. Keep existing route sorting, retry, failover, pause, and rate behavior after filtering.

- [ ] **Step 4: Wire forwarding and model aggregation**

Apply the helper immediately after loading group routes and before `SortRoutes` in forwarding and public model aggregation. Do not filter legacy entries without route sources. Keep model mapping resolution after route selection.

- [ ] **Step 5: Run focused tests and existing gateway tests**

Run `go test ./backend/gateway -run 'TestModelRoute|TestHandleModels|TestResolveModel' -count=1` and then `go test ./backend/gateway ./backend/api -count=1`. Expected: PASS.

### Task 4: Add model catalog selection dialog

**Files:**
- Create: `frontend/components/gateway/catalog-model-dialog.tsx`
- Modify: `frontend/components/gateway/models-panel.tsx`
- Modify: `frontend/components/gateway/gateway-page.tsx`
- Modify: `frontend/lib/api-types.ts`

- [ ] **Step 1: Add API types and client calls**

Define catalog model, catalog response, and catalog sync result types. Add existing `apiFetch` calls for the new GET and POST endpoints in `gateway-page.tsx`.

- [ ] **Step 2: Build the dialog UI**

Add search, selectable model rows, route checkboxes labeled with route ID/channel/source group, select-all routes, and a confirm button. Disable confirmation when no models or routes are selected. Show catalog metadata but do not display secrets.

- [ ] **Step 3: Wire the new button into ModelsPanel**

Add `同步官方模型目录` beside `从渠道同步去重`. On success, merge returned group data into the current page state, show the number of imported models/routes, and refresh groups/routes.

- [ ] **Step 4: Run frontend checks**

Run `pnpm lint`, `pnpm build`, and `git diff --check` from the frontend root. Expected: PASS.

### Task 5: Local integration verification

**Files:**
- Create: `backend/gateway/testdata/catalog-models.json`
- Modify: local test configuration only; do not commit secrets

- [ ] **Step 1: Add a fixture containing `gpt-5.6` and a second model**

- [ ] **Step 2: Start the local backend with `UPSTREAM_OPS_MODEL_CATALOG_URL` pointed at the fixture-serving test server**

- [ ] **Step 3: Use the local UI/API to import `gpt-5.6`, bind two routes, and verify the group switches to hybrid**

- [ ] **Step 4: Verify the group model list contains `gpt-5.6` and the runtime candidate helper excludes unbound routes**

- [ ] **Step 5: Run the full verification set**

Run `go test ./...`, `pnpm lint`, `pnpm build`, and `git diff --check`. Record failures before making any deployment decision.

### Task 6: Build and deploy only UpstreamOps

**Files/targets:**
- Target container: `upstream-ops-scheme-a`
- Target port: `8418`
- Target data: `/opt/upstream-ops-scheme-a/data`
- Target config: `/opt/upstream-ops-scheme-a/config.env`

- [ ] **Step 1: Record remote baseline**

Check container ID/image, port 8418, `/healthz`, and confirm `sub2api`, `intelligent-router`, and `sub2api-intelligent-gateway` are unchanged/running.

- [ ] **Step 2: Build an amd64 image locally**

Run the repository's existing image build command and tag it `upstream-ops:local-amd64`.

- [ ] **Step 3: Deploy only the target container**

Back up `/opt/upstream-ops-scheme-a/config.env` and the data directory metadata, replace only the target container using the same port, networks, volume, and restart policy, and do not alter firewall/Nginx or neighboring containers.

- [ ] **Step 4: Verify remote health and feature endpoint**

Check `/healthz`, query the catalog endpoint, and verify the existing services plus port 8418. Roll back to the recorded target image/container if validation fails.

