import type {
  GatewayProviderOption,
  GatewayRoute,
  GatewayGroup,
  RateSnapshot,
} from "@/lib/api-types"
import {
  isRouteTempPaused,
  routeEffectiveRate,
  routeSourceKind,
} from "./gateway-utils"

export type SchedulerRouteState =
  | "primary"
  | "fallback"
  | "disabled"
  | "paused"
  | "invalid"

export interface SchedulerRouteRow {
  route: Partial<GatewayRoute>
  index: number
  rate: number
  weight: number
  configuredShare: number
  state: SchedulerRouteState
  reason: string
  sourceLabel: string
  protocolLabel: string
}

export interface SchedulerSnapshot {
  rows: SchedulerRouteRow[]
  eligible: SchedulerRouteRow[]
  pausedCount: number
  disabledCount: number
  invalidCount: number
  configuredWeightTotal: number
}

function sourceLabel(
  route: Partial<GatewayRoute>,
  providers: GatewayProviderOption[],
): string {
  if (routeSourceKind(route) === "provider") {
    const provider = providers.find((item) => item.id === Number(route.gateway_provider_id))
    return provider?.name ?? `直连渠道 #${route.gateway_provider_id || "—"}`
  }
  const channel = route.source_channel_id ? `渠道 #${route.source_channel_id}` : "未绑定渠道"
  const group = route.source_group_name?.trim()
  return group ? `${channel} · ${group}` : channel
}

function protocolLabel(route: Partial<GatewayRoute>): string {
  switch (route.upstream_protocol) {
    case "anthropic":
      return "Anthropic"
    case "openai_responses":
      return "Responses"
    case "openai":
    case "openai_chat":
      return "OpenAI Chat"
    default:
      return "自动"
  }
}

function routeState(
  route: Partial<GatewayRoute>,
  providers: GatewayProviderOption[],
  now: number,
): { state: SchedulerRouteState; reason: string } {
  if (route.enabled === false) {
    return { state: "disabled", reason: "路由已禁用" }
  }
  if (isRouteTempPaused(route.temp_unschedulable_until, now)) {
    return { state: "paused", reason: "临时暂停中" }
  }
  if (routeSourceKind(route) === "provider") {
    const provider = providers.find((item) => item.id === Number(route.gateway_provider_id))
    if (!provider || provider.enabled === false) {
      return { state: "invalid", reason: provider ? "直连渠道已禁用" : "直连渠道不存在" }
    }
    return { state: "fallback", reason: "直连渠道已启用" }
  }
  if (!route.source_channel_id) {
    return { state: "invalid", reason: "未绑定监控渠道" }
  }
  if (!route.source_api_key_id && !route.source_api_key_name?.trim()) {
    return { state: "invalid", reason: "未绑定上游密钥" }
  }
  return { state: "fallback", reason: "已绑定密钥，凭据由后端验证" }
}

/**
 * Mirrors the runtime's hard eligibility gate and rate ordering for a read-only UI.
 * `configuredShare` is intentionally labeled as a preview: current Flow-Router uses
 * weight as a sort tie-breaker, not as proportional traffic splitting.
 */
export function deriveSchedulerSnapshot(
  group: GatewayGroup | null,
  routes: Partial<GatewayRoute>[],
  sourceGroupsByChannel: Record<number, RateSnapshot[]>,
  providers: GatewayProviderOption[],
  now = Date.now(),
): SchedulerSnapshot {
  const direction = group?.rate_sort_direction === "desc" ? -1 : 1
  const draftRows = routes.map((route, index) => {
    const sourceGroups = sourceGroupsByChannel[Number(route.source_channel_id) || 0] ?? []
    const rate = routeEffectiveRate(route, sourceGroups, providers)
    const weight = Math.max(1, Number(route.weight) || 1)
    const result = routeState(route, providers, now)
    return {
      route,
      index,
      rate: Number.isFinite(rate) ? rate : 0,
      weight,
      configuredShare: 0,
      state: result.state,
      reason: result.reason,
      sourceLabel: sourceLabel(route, providers),
      protocolLabel: protocolLabel(route),
    }
  })

  const eligible = draftRows
    .filter((row) => row.state === "fallback")
    .sort((a, b) => {
      if (a.rate !== b.rate) return (a.rate - b.rate) * direction
      if (a.weight !== b.weight) return b.weight - a.weight
      return a.index - b.index
    })
    .map((row, index) => ({
      ...row,
      state: index === 0 ? "primary" : "fallback",
    }))

  const configuredWeightTotal = eligible.reduce((sum, row) => sum + row.weight, 0)
  const eligibleWithShare = eligible.map((row) => ({
    ...row,
    configuredShare: configuredWeightTotal > 0 ? row.weight / configuredWeightTotal : 0,
  }))
  const eligibleByIndex = new Map(eligibleWithShare.map((row) => [row.index, row]))
  const rows = draftRows.map((row) => eligibleByIndex.get(row.index) ?? row)

  return {
    rows,
    eligible: eligibleWithShare,
    pausedCount: rows.filter((row) => row.state === "paused").length,
    disabledCount: rows.filter((row) => row.state === "disabled").length,
    invalidCount: rows.filter((row) => row.state === "invalid").length,
    configuredWeightTotal,
  }
}

export function formatSchedulerRate(value: number): string {
  if (!Number.isFinite(value)) return "—"
  return value >= 10 ? value.toFixed(2) : value.toFixed(4).replace(/0+$/, "").replace(/\.$/, "")
}

export function formatSchedulerPercent(value: number): string {
  return `${Math.round(value * 100)}%`
}
