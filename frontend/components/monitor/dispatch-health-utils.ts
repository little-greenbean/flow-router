import type { GatewayDispatchStatsRoute, GatewayDispatchWindow } from "@/lib/api-types"

export const DISPATCH_WINDOW_OPTIONS: { value: GatewayDispatchWindow; label: string }[] = [
  { value: "1m", label: "1 分钟" },
  { value: "5m", label: "5 分钟" },
  { value: "30m", label: "30 分钟" },
  { value: "1h", label: "1 小时" },
  { value: "4h", label: "4 小时" },
  { value: "8h", label: "8 小时" },
  { value: "12h", label: "12 小时" },
  { value: "24h", label: "24 小时" },
]

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
