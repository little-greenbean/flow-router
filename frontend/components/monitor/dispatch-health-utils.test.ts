import assert from "node:assert/strict"
import test from "node:test"
import {
  chunkDispatchGroups,
  DISPATCH_RANGE_OPTIONS,
  dispatchRangeMinutes,
  failureRateTone,
  metricBarPercent,
  formatDispatchRouteMetric,
  formatDispatchRouteSource,
  formatDispatchRouteGroup,
  formatBillingRate,
  dispatchRoutePath,
  isDispatchRouteNavigable,
  formatFailureRate,
  formatFirstToken,
  formatRouteIdentity,
  formatTTFT,
} from "./dispatch-health-utils.ts"

test("chunks gateway groups into three-column rows without reordering", () => {
  assert.deepEqual(chunkDispatchGroups(["gpt", "claude", "gemini"]), [
    ["gpt", "claude", "gemini"],
  ])
  assert.deepEqual(chunkDispatchGroups(["gpt", "claude", "gemini", "openai"]), [
    ["gpt", "claude", "gemini"],
    ["openai"],
  ])
  assert.deepEqual(chunkDispatchGroups([]), [])
})

test("normalizes metric bars to a bounded percentage", () => {
  assert.equal(metricBarPercent(0), 0)
  assert.equal(metricBarPercent(0.25), 25)
  assert.equal(metricBarPercent(1.4), 100)
  assert.equal(metricBarPercent(800, 1600), 50)
  assert.equal(metricBarPercent(null, 1600), 0)
})

test("dispatch ranges stay in the supported order and resolve to minutes", () => {
  assert.deepEqual(
    DISPATCH_RANGE_OPTIONS.map((item) => item.value),
    ["1m", "5m", "30m", "1h", "4h", "8h", "12h", "24h"],
  )
  // 档位标签和分钟数必须对得上——写错了窗口会静默取错区间，页面上看不出来
  assert.deepEqual(
    DISPATCH_RANGE_OPTIONS.map((item) => item.minutes),
    [1, 5, 30, 60, 240, 480, 720, 1440],
  )
  assert.equal(dispatchRangeMinutes("30m"), 30)
  assert.equal(dispatchRangeMinutes("24h"), 1440)
  // 未知档位退回 1 小时，而不是 NaN（NaN 会让 from 变成 Invalid Date）
  assert.equal(dispatchRangeMinutes("99h" as never), 60)
})

test("formats failure rates and severity", () => {
  assert.equal(formatFailureRate(0), "0.0%")
  assert.equal(formatFailureRate(0.123), "12.3%")
  assert.equal(failureRateTone(0), "success")
  assert.equal(failureRateTone(0.1), "warning")
  assert.equal(failureRateTone(0.2), "danger")
})

test("formats first token latency with an empty state", () => {
  assert.equal(formatFirstToken(null), "暂无数据")
  assert.equal(formatFirstToken(undefined), "暂无数据")
  assert.equal(formatFirstToken(842.5), "843 ms")
})

test("formats a route into the compact dashboard metric", () => {
  assert.equal(
    formatDispatchRouteMetric({ failure_rate: 0.333, average_first_token_ms: 456.7 }),
    "失败 33.3% · 首字 457 ms",
  )
})

test("prefers source snapshots and never exposes a synthetic route id", () => {
  assert.equal(
    formatDispatchRouteSource({ source_api_key_name: "来源 A", provider_name: "Provider A", route_name: "路由 #42" }),
    "来源 A",
  )
  assert.equal(
    formatDispatchRouteSource({ source_api_key_name: "", provider_name: "", route_name: "路由 #42" }),
    "未命名来源",
  )
  assert.equal(formatDispatchRouteGroup({ source_group_name: "低价组", provider_name: "Provider A" }), "低价组")
  assert.equal(formatDispatchRouteGroup({ source_group_name: "", provider_name: "" }), "未记录源分组")
  assert.equal(formatBillingRate(0.8), "0.80x")
})

test("builds a gateway route deep link with group and route ids", () => {
  assert.equal(dispatchRoutePath(12, 34), "/gateway?group=12&route=34")
})

test("only links routes that still exist in the current gateway config", () => {
  assert.equal(isDispatchRouteNavigable({ route_available: true }), true)
  assert.equal(isDispatchRouteNavigable({ route_available: false }), false)
  assert.equal(isDispatchRouteNavigable({}), true)
})

test("route identity prefers source and source group over the key name", () => {
  assert.equal(
    formatRouteIdentity({ source_name: "55", source_group_name: "GPT-特惠-Plus", key_name: "uops-ch9-sgn-GPT-3d50", route_id: 7 }),
    "55 · GPT-特惠-Plus",
  )
  assert.equal(formatRouteIdentity({ source_name: "55", route_id: 7 }), "55")
  assert.equal(formatRouteIdentity({ source_group_name: "GPT-特惠-Plus", route_id: 7 }), "GPT-特惠-Plus")
})

test("route identity falls back to the key name, then the route id", () => {
  assert.equal(formatRouteIdentity({ key_name: "uops-ch9-3d50", route_id: 7 }), "uops-ch9-3d50")
  assert.equal(formatRouteIdentity({ route_id: 7 }), "路由 #7")
  // 全是空白串时也要退到 id，而不是显示一串空格
  assert.equal(formatRouteIdentity({ source_name: "  ", source_group_name: "", key_name: " ", route_id: 7 }), "路由 #7")
})

test("TTFT formats by magnitude and marks missing samples", () => {
  assert.equal(formatTTFT(0), "—")
  assert.equal(formatTTFT(-1), "—")
  assert.equal(formatTTFT(842), "842ms")
  assert.equal(formatTTFT(1000), "1.0s")
  assert.equal(formatTTFT(12400), "12.4s")
})
