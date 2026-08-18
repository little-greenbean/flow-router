import assert from "node:assert/strict"
import test from "node:test"
import {
  DISPATCH_WINDOW_OPTIONS,
  failureRateTone,
  formatFailureRate,
  formatFirstToken,
} from "./dispatch-health-utils.ts"

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
