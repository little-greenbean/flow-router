import type { GatewayDispatchStatsRoute, GatewayDispatchWindow } from "@/lib/api-types"

export function chunkDispatchGroups<T>(groups: T[], columns = 3): T[][] {
  if (!Number.isInteger(columns) || columns < 1) {
    throw new RangeError("columns must be a positive integer")
  }

  const rows: T[][] = []
  for (let index = 0; index < groups.length; index += columns) {
    rows.push(groups.slice(index, index + columns))
  }
  return rows
}

export function metricBarPercent(value: number | null | undefined, ceiling = 1): number {
  if (value == null || !Number.isFinite(value) || !Number.isFinite(ceiling) || ceiling <= 0) return 0
  return Math.round(Math.min(1, Math.max(0, value / ceiling)) * 100)
}

export type FailureRateTone = "success" | "warning" | "danger"

export function formatFailureRate(rate: number): string {
  const normalized = Number.isFinite(rate) ? Math.max(0, rate) : 0
  return `${(normalized * 100).toFixed(1)}%`
}

export function failureRateTone(rate: number): FailureRateTone {
  if (!Number.isFinite(rate) || rate <= 0) return "success"
  if (rate < 0.2) return "warning"
  return "danger"
}

export function formatFirstToken(ms: number | null | undefined): string {
  if (ms == null || !Number.isFinite(ms)) return "暂无数据"
  return `${Math.max(0, Math.round(ms))} ms`
}

export function formatDispatchRouteMetric(
  route: Pick<GatewayDispatchStatsRoute, "failure_rate" | "average_first_token_ms">,
): string {
  return `失败 ${formatFailureRate(route.failure_rate)} · 首字 ${formatFirstToken(route.average_first_token_ms)}`
}

export function formatDispatchRouteSource(
  route: Pick<GatewayDispatchStatsRoute, "source_api_key_name" | "provider_name" | "route_name">,
): string {
  const source = [route.source_api_key_name, route.provider_name, route.route_name]
    .map((value) => value?.trim())
    .find((value) => value && !/^路由\s*#\d+$/.test(value))
  return source || "未命名来源"
}

export function formatDispatchRouteGroup(
  route: Pick<GatewayDispatchStatsRoute, "source_group_name" | "provider_name">,
): string {
  return route.source_group_name?.trim() || route.provider_name?.trim() || "未记录源分组"
}

export function formatBillingRate(rate: number | null | undefined): string {
  return `${Number.isFinite(rate) && Number(rate) > 0 ? Number(rate).toFixed(2) : "1.00"}x`
}

export function dispatchRoutePath(groupID: number, routeID: number): string {
  return `/gateway?group=${encodeURIComponent(groupID)}&route=${encodeURIComponent(routeID)}`
}

export function isDispatchRouteNavigable(
  route: Pick<GatewayDispatchStatsRoute, "route_available">,
): boolean {
  return route.route_available !== false
}

// ---- 建议关注 ----
//
// 判定阈值、时间粒度和身份格式化放在这里而不是组件里，因为它们编码的是
// 「什么算该禁掉」「按什么粒度看」这类业务判断，值得单独测。组件只负责画。

/**
 * 时间粒度。档位跟后端 dispatchWindowDurations（/dispatch/stats 的 window 参数）保持一致，
 * 但这里额外带分钟数——建议关注走 from/to 而不是 window，好跟错误分布共用同一个区间。
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

/**
 * 单元格染色用的阈值提示，镜像后端 dispatchActionFailover / dispatchWatchFailover /
 * dispatchActionFailStreak / dispatchWatchTTFTMillis。**判定本身在后端**（route.health），
 * 这里只决定某个数字要不要标红——这样"排在前面"和"标成需处理"不会打架。
 */
export const NEEDS_ACTION_FAILOVER = 0.5
export const WATCH_FAILOVER = 0.1
export const NEEDS_ACTION_FAIL_STREAK = 5
export const WATCH_FAIL_STREAK = 3
export const WATCH_TTFT_MS = 10_000

/**
 * 路由身份 = 来源 · 源分组。
 *
 * 注意跟上面老的 formatDispatchRouteSource 的区别：那个把密钥名排在最前，
 * 而密钥名（uops-ch9-sgn-GPT-Pro-927b 这种）对人没有意义。这里只在来源和
 * 源分组双双缺失时才退回密钥名。
 */
export function formatRouteIdentity(
  route: { source_name?: string; source_group_name?: string; key_name?: string; route_id: number },
): string {
  const parts = [route.source_name, route.source_group_name]
    .map((value) => value?.trim())
    .filter((value): value is string => Boolean(value))
  if (parts.length > 0) return parts.join(" · ")
  return route.key_name?.trim() || `路由 #${route.route_id}`
}

/** TTFT 展示：秒级用 s，毫秒级用 ms，没有样本用破折号（0 会被误读成"很快"）。 */
export function formatTTFT(ms: number): string {
  if (!Number.isFinite(ms) || ms <= 0) return "—"
  return ms >= 1000 ? `${(ms / 1000).toFixed(1)}s` : `${Math.round(ms)}ms`
}
