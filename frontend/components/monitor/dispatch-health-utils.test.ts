import assert from "node:assert/strict"
import test from "node:test"
import {
  chunkDispatchGroups,
  DISPATCH_WINDOW_OPTIONS,
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

test("dispatch windows stay in the supported order", () => {
  assert.deepEqual(
    DISPATCH_WINDOW_OPTIONS.map((item) => item.value),
    ["1m", "5m", "30m", "1h", "4h", "8h", "12h", "24h"],
  )
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
