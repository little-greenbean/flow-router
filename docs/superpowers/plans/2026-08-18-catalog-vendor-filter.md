# Catalog Vendor Filter Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task with verification checkpoints.

**Goal:** Add multi-select vendor filter tags below the official catalog search input.

**Architecture:** Keep filtering as a pure helper that derives normalized vendor tags and applies keyword-plus-vendor predicates. The dialog owns the selected vendor state and renders the helper's dynamic tags; backend contracts remain unchanged.

**Tech Stack:** React 19, TypeScript, Tailwind CSS, Node built-in test runner for the pure helper.

---

### Task 1: Define and test catalog vendor filtering

**Files:**
- Create: `frontend/components/gateway/catalog-filter.ts`
- Create: `frontend/components/gateway/catalog-filter.test.ts`

- [ ] Write a failing test for normalized vendor tags and multi-vendor OR filtering.
- [ ] Run `node --test --experimental-strip-types frontend/components/gateway/catalog-filter.test.ts` and confirm it fails because the helper is missing.
- [ ] Implement the minimal pure helpers `catalogVendors` and `filterCatalogModels`.
- [ ] Re-run the test and confirm it passes.

### Task 2: Integrate vendor tags into the catalog dialog

**Files:**
- Modify: `frontend/components/gateway/catalog-model-dialog.tsx`

- [ ] Track selected vendor names and clear them whenever the dialog opens.
- [ ] Derive vendor tags from the loaded catalog and filter models by keyword plus any selected vendor.
- [ ] Render `全部` and vendor buttons below the search input with multi-select active styling and an accessible selected state.
- [ ] Keep existing select-all, clear, route binding, and apply behavior unchanged.

### Task 3: Verify the UI and build

**Files:**
- Modify: none

- [ ] Run the focused helper test.
- [ ] Run `pnpm lint`.
- [ ] Run `pnpm build`.
- [ ] With the local app open, open the official model catalog, select two vendor tags, verify models from either vendor remain, then click `全部` and verify the full catalog returns.
