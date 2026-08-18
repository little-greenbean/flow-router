# Scheme A Scheduler Visualization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a live, read-only Scheme A scheduling workbench to the existing UpstreamOps Gateway page without changing gateway routing behavior or inventing AI APIs.

**Architecture:** Reuse the existing Gateway Group selection and route loading. A focused utility derives the same schedulable/candidate order from the loaded route drafts, source-group snapshots, and provider metadata, then a presentation component renders rule gates, primary/fallback order, configured weights, and an explicit note that current weight is ordering metadata rather than proportional traffic splitting. Existing management panels remain the write surface.

**Tech Stack:** React 19, TypeScript, Vite, Tailwind CSS v4, existing Radix UI primitives, lucide-react, existing `GatewayRoute`/`GatewayGroup` API types.

---

### Task 1: Add pure scheduler-view derivation helpers

**Files:**
- Create: `frontend/components/gateway/scheduler-view-utils.ts`

- [x] **Step 1: Define pure helper types and derivation functions** for route eligibility, effective-rate ordering, pause labeling, and configured-weight normalization.
- [x] **Step 2: Implement the helpers** using existing `GatewayRoute`, `GatewayProviderOption`, and `RateSnapshot` shapes; never mutate route drafts and never treat weight as actual traffic share.
- [x] **Step 3: Run the production TypeScript/Vite build** after integration to catch type and import regressions.

### Task 2: Build the Scheme A scheduler workbench

**Files:**
- Create: `frontend/components/gateway/scheduler-workbench.tsx`
- Modify: `frontend/components/gateway/gateway-page.tsx`

- [x] **Step 1: Add a read-only `调度` config tab** next to the existing keys/routes/models/policy tabs.
- [x] **Step 2: Render the workbench** with a compact rule pipeline, group policy summary, primary/fallback route chain, route status table, configured weight visualization, and warning that current UpstreamOps uses rate/weight/position ordering rather than proportional weighted load balancing.
- [x] **Step 3: Use real loaded route/provider/source-group data** and existing refresh state; show loading, no-route, paused-route, and stale-source-group states without fake success data.
- [x] **Step 4: Keep controls navigational/read-only**: route editing continues through the existing routes tab; no AI toggle or unsupported write API is added.

### Task 3: Verify the integrated frontend

**Files:**
- Modify only if lint/build reports a concrete issue in the touched files.

- [x] **Step 1: Run `pnpm lint` from `frontend/` and fix only errors caused by this change.**
- [x] **Step 2: Run `pnpm build` from `frontend/` and confirm the production bundle completes.**
- [x] **Step 3: Run the Vite dev server and inspect `/gateway` with a browser at desktop and mobile widths; verify the new tab does not overlap existing controls and the unauthenticated state remains intact without a backend.**
- [x] **Step 4: Report the local URL and the backend limitation if the Go runtime is unavailable in the workspace.**
