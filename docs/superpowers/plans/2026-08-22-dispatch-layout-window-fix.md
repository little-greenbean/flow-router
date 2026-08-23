# Dispatch Layout and Window Fix Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make dispatch trends compact and reliable by placing gateway and route charts side by side, showing the selected gateway's route list, and keeping dataZoom within the loaded data range.

**Architecture:** Keep the existing `/api/gateway/dispatch/trends` contract. The React panel owns one query range and one gateway-chart dataZoom; the route chart follows that range without its own zoom control. Route metadata is rendered beside the route chart from the existing `routes` response.

**Tech Stack:** React, TypeScript, ECharts 5, Tailwind utility classes, existing gateway trends API.

---

### Task 1: Compact dual-chart layout and route list

**Files:**
- Modify: `frontend/components/monitor/dispatch-health-panel.tsx`

- [x] Replace the vertical chart sections with a responsive two-column grid; keep one chart per column on narrow screens.
- [x] Render the selected gateway's route names, provider names, and point availability beside the route chart.
- [x] Keep the gateway chart as the only chart with a visible slider; route chart uses the same x-axis range without a slider.

### Task 2: Fix dataZoom range handling

**Files:**
- Modify: `frontend/components/monitor/dispatch-health-panel.tsx`

- [x] Derive the data bounds from the current gateway point timestamps.
- [x] Clamp `startValue` and `endValue` to those bounds before updating the query range.
- [x] Ignore invalid or collapsed ranges and debounce only the gateway chart request.
- [x] Apply the current query range to both charts while preventing the route chart from emitting zoom events.
  - Route chart uses `zoom: "follow"`: its x-axis min/max come from the current visible range, so it has no
    dataZoom component at all (no slider, no wheel zoom, no `datazoom` event). The gateway chart keeps
    `zoom: "slider"`, where the axis spans the full data bounds and the window is driven by dataZoom.

### Task 3: Verify UI and regression behavior

**Files:**
- Test: `frontend/components/monitor/dispatch-health-panel.tsx` via the running browser and build checks.

- [x] Run `npm run lint` and `npm run build`.
- [x] Confirm the trends endpoint returns four gateways and fourteen routes.
- [x] Confirm the browser shows side-by-side charts, visible route names, no right-side blank area after dragging, and no route-chart zoom control.
- [x] Extend browser verification to shared dual-chart zoom, route color/highlight, per-line tooltip configuration, and gateway line event handling.
