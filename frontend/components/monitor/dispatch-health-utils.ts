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

/**
 * 使用记录深链接：切到「使用记录」标签，带上网关 + 结果筛选 + 时间区间。
 *
 * 时间用 datetime-local 的格式（YYYY-MM-DDTHH:mm，本地时区），因为那两个筛选框
 * 就是 <input type="datetime-local">，传 ISO 带 Z 的话它认不出来会留空。
 */
export function dispatchUsagePath(
  options: { group?: number; result: string; from: Date; to: Date },
): string {
  const params = new URLSearchParams({ tab: "usage", usage_result: options.result })
  params.set("usage_group", options.group ? String(options.group) : "all")
  params.set("usage_from", toDatetimeLocal(options.from))
  params.set("usage_to", toDatetimeLocal(options.to))
  return `/gateway?${params.toString()}`
}

export function toDatetimeLocal(date: Date): string {
  if (Number.isNaN(date.getTime())) return ""
  const pad = (value: number) => String(value).padStart(2, "0")
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`
}

/**
 * 路由节点的配色索引。同一条路由在不同跳是不同节点，但必须是同一个颜色，
 * 这样才能顺着颜色把一条路由在图上串起来——所以按 route_id 定色，不按位置。
 */
export function routeColorIndex(routeID: number, paletteSize: number): number {
  if (!Number.isFinite(routeID) || paletteSize <= 0) return 0
  return Math.abs(Math.trunc(routeID)) % paletteSize
}

/**
 * 顺延次数筛选档位。数值跟后端 dispatchFlowFailoverFilterOverflow 保持一致——
 * 达到 5 是「5+ 次」这一档（精确到某个数字以上没意义，链路顺延次数没有自然上限，
 * 桑基图本身也是从第 6 跳开始把更深的链收进一个溢出节点），比它小的都是精确匹配。
 */
export const DISPATCH_FAILOVER_FILTER_OVERFLOW = 5

export const DISPATCH_FAILOVER_FILTER_OPTIONS: { value: number | null; label: string }[] = [
  { value: null, label: "全部" },
  { value: 0, label: "0 次" },
  { value: 1, label: "1 次" },
  { value: 2, label: "2 次" },
  { value: 3, label: "3 次" },
  { value: 4, label: "4 次" },
  { value: DISPATCH_FAILOVER_FILTER_OVERFLOW, label: "5+ 次" },
]
