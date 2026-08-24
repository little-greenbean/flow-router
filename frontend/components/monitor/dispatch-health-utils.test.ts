import assert from "node:assert/strict"
import test from "node:test"
import {
  DISPATCH_RANGE_OPTIONS,
  dispatchRangeMinutes,
  dispatchRoutePath,
  flowShare,
  formatDuration,
  dispatchUsagePath,
  routeColorIndex,
  toDatetimeLocal,
  formatPercent,
} from "./dispatch-health-utils.ts"

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

test("builds gateway deep links with encoded ids", () => {
  assert.equal(dispatchRoutePath(5, 90), "/gateway?group=5&route=90")
})

test("flow shares never divide by zero", () => {
  assert.equal(flowShare(3, 12), 0.25)
  assert.equal(flowShare(3, 0), 0)
  assert.equal(flowShare(Number.NaN, 12), 0)
  assert.equal(formatPercent(0.25), "25.0%")
  assert.equal(formatPercent(Number.NaN), "—")
})

test("formats durations by magnitude and marks missing samples", () => {
  assert.equal(formatDuration(0), "—")
  assert.equal(formatDuration(null), "—")
  assert.equal(formatDuration(842), "842ms")
  assert.equal(formatDuration(25_800), "25.8s")
})

test("usage deep links carry the tab, gateway scope, result mode and local-time range", () => {
  const from = new Date(2026, 7, 24, 9, 5)
  const to = new Date(2026, 7, 24, 10, 5)
  const all = new URLSearchParams(dispatchUsagePath({ result: "fail", from, to }).split("?")[1])
  assert.equal(all.get("tab"), "usage")
  assert.equal(all.get("usage_result"), "fail")
  // 没指定网关时要显式给 all，否则使用记录会沿用上次的筛选
  assert.equal(all.get("usage_group"), "all")
  // datetime-local 只认本地时间的 YYYY-MM-DDTHH:mm，给 ISO 带 Z 会被输入框丢掉
  assert.equal(all.get("usage_from"), "2026-08-24T09:05")
  assert.equal(all.get("usage_to"), "2026-08-24T10:05")

  const scoped = new URLSearchParams(dispatchUsagePath({ group: 7, result: "multi_success", from, to }).split("?")[1])
  assert.equal(scoped.get("usage_group"), "7")
  assert.equal(scoped.get("usage_result"), "multi_success")
})

test("datetime-local formatting pads and survives invalid dates", () => {
  assert.equal(toDatetimeLocal(new Date(2026, 0, 3, 4, 7)), "2026-01-03T04:07")
  assert.equal(toDatetimeLocal(new Date("nope")), "")
})

test("route colors follow the route, not its position in the chart", () => {
  // 同一条路由在第 1 跳和第 4 跳必须同色，才能顺着颜色把它串起来
  assert.equal(routeColorIndex(71, 10), routeColorIndex(71, 10))
  assert.equal(routeColorIndex(71, 10), 1)
  assert.equal(routeColorIndex(0, 10), 0)
  assert.equal(routeColorIndex(Number.NaN, 10), 0)
  assert.equal(routeColorIndex(71, 0), 0)
})
