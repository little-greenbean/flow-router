import type { GatewayDispatchWindow } from "@/lib/api-types"

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
