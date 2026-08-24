import assert from "node:assert/strict"
import test from "node:test"
import {
  DISPATCH_RANGE_OPTIONS,
  dispatchRangeMinutes,
  dispatchRoutePath,
  flowShare,
  formatDuration,
  formatErrorClock,
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

test("error clocks add a date only when the row is from another day", () => {
  const now = new Date("2026-08-24T12:00:00+08:00")
  assert.match(formatErrorClock("2026-08-24T09:30:05+08:00", now), /^09:30:05$/)
  assert.match(formatErrorClock("2026-08-23T09:30:05+08:00", now), /^8\/23 09:30:05$/)
  assert.equal(formatErrorClock("not-a-date", now), "not-a-date")
})
