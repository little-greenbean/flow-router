import type { GatewayDispatchWindow } from "@/lib/api-types"

// 调度面板的纯函数都放这里：档位表、跳转路径、展示格式化。
// 组件只负责画，这些编码的是「按什么粒度看」「怎么落到具体路由」，值得单独测。

/**
 * 时间粒度。档位跟后端 dispatchWindowDurations（/dispatch/stats 的 window 参数）保持一致，
 * 但这里额外带分钟数——调度面板走 from/to 而不是 window，好让流向图和原始报错共用同一个区间。
 */
export const DISPATCH_RANGE_OPTIONS: { value: GatewayDispatchWindow; label: string; minutes: number }[] = [
  { value: "1m", label: "1 分钟", minutes: 1 },
  { value: "5m", label: "5 分钟", minutes: 5 },
  { value: "30m", label: "30 分钟", minutes: 30 },
  { value: "1h", label: "1 小时", minutes: 60 },
  { value: "4h", label: "4 小时", minutes: 240 },
  { value: "8h", label: "8 小时", minutes: 480 },
  { value: "12h", label: "12 小时", minutes: 720 },
  { value: "24h", label: "24 小时", minutes: 1440 },
]

export function dispatchRangeMinutes(value: GatewayDispatchWindow): number {
  return DISPATCH_RANGE_OPTIONS.find((option) => option.value === value)?.minutes ?? 60
}

/** 深链接：网关页会切到对应组的路由标签、滚动定位并短暂高亮。 */
export function dispatchRoutePath(groupID: number, routeID: number): string {
  return `/gateway?group=${encodeURIComponent(groupID)}&route=${encodeURIComponent(routeID)}`
}

/** 桑基图上的占比。分母为 0 时给 0 而不是 NaN。 */
export function flowShare(value: number, total: number): number {
  if (!Number.isFinite(value) || !Number.isFinite(total) || total <= 0) return 0
  return value / total
}

export function formatPercent(ratio: number, digits = 1): string {
  if (!Number.isFinite(ratio)) return "—"
  return `${(ratio * 100).toFixed(digits)}%`
}

/** 耗时展示：秒级用 s，毫秒级用 ms，没有样本用破折号（0 会被误读成"很快"）。 */
export function formatDuration(ms: number | null | undefined): string {
  if (ms == null || !Number.isFinite(ms) || ms <= 0) return "—"
  return ms >= 1000 ? `${(ms / 1000).toFixed(1)}s` : `${Math.round(ms)}ms`
}

/** 原始报错行的时间戳：精确到秒，跨天时带上日期。 */
export function formatErrorClock(iso: string, now = new Date()): string {
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return iso
  const sameDay = date.getFullYear() === now.getFullYear()
    && date.getMonth() === now.getMonth()
    && date.getDate() === now.getDate()
  const clock = date.toLocaleTimeString("zh-CN", { hour12: false })
  return sameDay ? clock : `${date.getMonth() + 1}/${date.getDate()} ${clock}`
}
