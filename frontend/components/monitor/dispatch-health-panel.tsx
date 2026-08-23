"use client"

import { useEffect, useMemo, useRef, useState } from "react"
import { Link } from "react-router-dom"
import { Activity } from "lucide-react"
import * as echarts from "echarts"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { useGatewayDispatchErrors, useGatewayDispatchScorecard } from "@/lib/queries"
import { useRefreshTick } from "@/lib/refresh-context"
import type {
  DispatchHealth,
  GatewayDispatchErrorScope,
  GatewayDispatchScorePoint,
} from "@/lib/api-types"
import {
  NEEDS_ACTION_FAILOVER,
  WATCH_FAILOVER,
  WATCH_TTFT_MS,
  dispatchRoutePath,
  formatScoreRouteIdentity,
  formatTTFT,
} from "@/components/monitor/dispatch-health-utils"
import { cn } from "@/lib/utils"

/**
 * 调度情况面板。
 *
 * 这里刻意不做通用趋势浏览器，而是围绕一个具体决策组织信息：**这条路由该不该禁掉**。
 * 所以主体是按路由横向铺开的记分卡（路由之间的比较才是决策依据），时间维度退化成
 * 每行一条迷你折线交代走向；下半部分保留错误下钻，用来回答「到底错在哪」。
 */

const EMPTY_ERROR_SCOPE: GatewayDispatchErrorScope = {
  requests: 0, final_failed: 0, error_rate: 0, recovered_requests: 0,
  attempts: 0, failed_attempts: 0, attempt_error_rate: 0,
  severity: { p0: 0, p1: 0, p2: 0 }, categories: [], samples: [],
}

const ERROR_COLORS: Record<string, string> = {
  http: "#c45050", transport: "#db7b2b", config: "#7b61b3", internal: "#b3527d", client: "#6b7787", "": "#98a3b4",
}

// P0/P1/P2 按「要不要人工介入」分级，与后端 dispatchSeverityOf 一一对应。
const SEVERITY_META = [
  { key: "p0" as const, label: "P0", hint: "需人工处理：认证失效 / 欠费 / 分组被删 / 配置写错，放着不会自愈", color: "#c45050" },
  { key: "p1" as const, label: "P1", hint: "上游抖动：5xx / 429 / 超时 / 传输错，可能自愈但要盯着", color: "#db7b2b" },
  { key: "p2" as const, label: "P2", hint: "噪声：客户端主动断开或取消，通常不用管", color: "#98a3b4" },
]

const WINDOWS = [
  { hours: 1, label: "近 1 小时" },
  { hours: 6, label: "近 6 小时" },
  { hours: 24, label: "近 24 小时" },
]

type SparkMode = "failover" | "ttft"

const HEALTH_META: Record<DispatchHealth, { mark: string; label: string; className: string }> = {
  action: { mark: "▲", label: "需处理", className: "text-danger" },
  watch: { mark: "●", label: "关注", className: "text-warning" },
  ok: { mark: "○", label: "健康", className: "text-success" },
}

/**
 * 迷你折线。用内联 SVG 而不是 ECharts 实例：一屏可能有几十行，
 * 每行开一个图表实例既慢又占内存，而这里只需要交代走向。
 *
 * 纵轴刻度由外部统一传入（顺延率固定 0~100%，TTFT 取全表最大值），
 * 这样各行之间可以直接横向比高低——否则每行自适应缩放，图形好看但没法比。
 */
function Sparkline({ points, mode, max }: { points: GatewayDispatchScorePoint[]; mode: SparkMode; max: number }) {
  const width = 72
  const height = 20
  const segments: string[] = []
  let current: string[] = []

  points.forEach((point, index) => {
    // 该时间桶没有任何尝试 → 断线，而不是画成 0（0 会被误读成"很健康"）
    if (point.attempts === 0) {
      if (current.length > 1) segments.push(current.join(" "))
      current = []
      return
    }
    const raw = mode === "failover" ? point.failover_rate : point.ttft_p95
    const ratio = max > 0 ? Math.min(1, raw / max) : 0
    const x = points.length > 1 ? (index / (points.length - 1)) * width : width / 2
    const y = height - ratio * (height - 2) - 1
    current.push(`${x.toFixed(1)},${y.toFixed(1)}`)
  })
  if (current.length > 1) segments.push(current.join(" "))

  const stroke = mode === "failover" ? "#c45050" : "#2878bd"
  return (
    <svg width={width} height={height} viewBox={`0 0 ${width} ${height}`} className="shrink-0" aria-hidden>
      <line x1={0} y1={height - 1} x2={width} y2={height - 1} stroke="currentColor" strokeOpacity={0.12} strokeWidth={1} />
      {segments.length === 0
        ? <text x={width / 2} y={height / 2 + 3} textAnchor="middle" fontSize={8} fill="currentColor" fillOpacity={0.35}>无数据</text>
        : segments.map((segment, index) => (
            <polyline key={index} points={segment} fill="none" stroke={stroke} strokeWidth={1.2} strokeLinejoin="round" strokeLinecap="round" />
          ))}
    </svg>
  )
}

function errorPieOption(data: GatewayDispatchErrorScope) {
  const inner = data.categories.map((category) => ({
    name: category.label, value: category.count,
    itemStyle: { color: ERROR_COLORS[category.error_type] ?? "#98a3b4" },
  }))
  // 外环：状态码明细，继承所属分类的色相并按占比递减透明度，保持父子可辨认
  const outer = data.categories.flatMap((category) =>
    category.codes.map((code, index) => ({
      name: `${category.label} · ${code.label}`, value: code.count,
      itemStyle: { color: ERROR_COLORS[category.error_type] ?? "#98a3b4", opacity: Math.max(0.35, 1 - index * 0.2) },
    })),
  )
  return {
    animationDuration: 200,
    tooltip: {
      trigger: "item" as const, confine: true, borderColor: "#dde3ea", textStyle: { fontSize: 11 },
      formatter: (params: unknown) => {
        const item = params as { name?: string; value?: number; percent?: number; marker?: string }
        return `${item.marker ?? ""}${item.name ?? ""}<br/><b>${item.value ?? 0}</b> 次 · ${(item.percent ?? 0).toFixed(1)}%`
      },
    },
    legend: { show: false },
    series: [
      {
        type: "pie" as const, radius: ["30%", "52%"], center: ["50%", "50%"], data: inner,
        label: { show: false }, labelLine: { show: false },
        emphasis: { scale: false, itemStyle: { shadowBlur: 6, shadowColor: "rgba(0,0,0,.15)" } },
      },
      {
        type: "pie" as const, radius: ["58%", "76%"], center: ["50%", "50%"], data: outer,
        label: { show: false }, labelLine: { show: false },
        emphasis: { scale: false, itemStyle: { shadowBlur: 6, shadowColor: "rgba(0,0,0,.15)" } },
      },
    ],
  }
}

export function DispatchHealthPanel() {
  const [windowHours, setWindowHours] = useState(6)
  const [sparkMode, setSparkMode] = useState<SparkMode>("failover")
  const [errorGatewayID, setErrorGatewayID] = useState<number | null>(null)
  const [errorRouteID, setErrorRouteID] = useState<number | null>(null)
  const tick = useRefreshTick()

  // 跟着全局刷新 tick 一起前滚，窗口才是"最近 N 小时"而不是"打开页面那一刻起的 N 小时"
  const range = useMemo(() => {
    const to = new Date()
    const from = new Date(to.getTime() - windowHours * 3600_000)
    return { from: from.toISOString(), to: to.toISOString() }
  }, [windowHours, tick])

  const scorecard = useGatewayDispatchScorecard(range.from, range.to)
  const errors = useGatewayDispatchErrors(range.from, range.to)
  const routes = useMemo(() => scorecard.data?.routes ?? [], [scorecard.data])
  const errorData = errors.data

  /**
   * 迷你线的纵轴上限：两种模式都取「全表最大值」而不是各行自适应——
   * 各行自适应画出来好看，但行与行没法比高低，记分卡就失去意义了。
   *
   * 顺延率不用固定 0~100%：实际值常年在个位数百分比，贴着底边画等于没画。
   * 用 5% 兜底，避免全表都接近 0 时把噪声放大成大起大落。
   */
  const sparkMax = useMemo(() => {
    let failover = 0
    let ttft = 0
    for (const route of routes) {
      for (const point of route.points) {
        if (point.failover_rate > failover) failover = point.failover_rate
        if (point.ttft_p95 > ttft) ttft = point.ttft_p95
      }
    }
    return { failover: Math.max(failover, 0.05), ttft }
  }, [routes])

  const counts = useMemo(() => {
    const result = { action: 0, watch: 0, ok: 0 }
    for (const route of routes) result[route.health]++
    return result
  }, [routes])

  const errorRef = useRef<HTMLDivElement | null>(null)
  const errorChart = useRef<echarts.ECharts | null>(null)

  const errorGateway = errorGatewayID == null ? undefined : errorData?.groups.find((group) => group.gateway_group_id === errorGatewayID)
  const errorRoute = errorRouteID == null ? undefined : errorGateway?.routes.find((route) => route.route_id === errorRouteID)
  const errorScope: GatewayDispatchErrorScope = errorRoute ?? errorGateway ?? errorData ?? EMPTY_ERROR_SCOPE
  const scopeLabel = errorRoute ? `路由：${errorRoute.route_name}` : errorGateway ? `网关：${errorGateway.gateway_group_name}` : "全部网关"

  // 数据刷新后选中的网关/路由可能已经消失，及时复位免得卡在空作用域
  useEffect(() => {
    if (!errorData) return
    if (errorGatewayID != null && !errorData.groups.some((group) => group.gateway_group_id === errorGatewayID)) {
      setErrorGatewayID(null)
      setErrorRouteID(null)
      return
    }
    if (errorRouteID != null && errorGateway && !errorGateway.routes.some((route) => route.route_id === errorRouteID)) {
      setErrorRouteID(null)
    }
  }, [errorData, errorGatewayID, errorRouteID, errorGateway])

  useEffect(() => {
    if (!errorRef.current) return
    if (errorScope.categories.length === 0) {
      errorChart.current?.dispose()
      errorChart.current = null
      return
    }
    if (!errorChart.current) errorChart.current = echarts.init(errorRef.current)
    errorChart.current.setOption(errorPieOption(errorScope), true)
    const resize = () => errorChart.current?.resize()
    window.addEventListener("resize", resize)
    return () => window.removeEventListener("resize", resize)
  }, [errorScope])

  useEffect(() => () => { errorChart.current?.dispose(); errorChart.current = null }, [])

  return <Card className="overflow-hidden border border-border py-2 shadow-none sm:py-3">
    <CardHeader className="gap-2 px-3 pb-2 sm:flex sm:flex-row sm:items-center sm:justify-between sm:px-4">
      <CardTitle className="flex items-center gap-1.5 text-sm font-semibold">
        <Activity className="size-3.5 text-brand" />调度情况
      </CardTitle>
      <div className="flex flex-wrap items-center justify-end gap-1.5">
        <div className="inline-flex rounded-md border border-border bg-muted/30 p-0.5">
          {WINDOWS.map((item) => (
            <button key={item.hours} type="button" onClick={() => setWindowHours(item.hours)}
              className={cn("h-6 rounded px-2 text-[11px]", windowHours === item.hours ? "bg-background font-semibold shadow-sm" : "text-muted-foreground")}>
              {item.label}
            </button>
          ))}
        </div>
        <span className="text-[10px] text-success">● 实时</span>
      </div>
    </CardHeader>

    <CardContent className="px-3 pb-2 sm:px-4">
      {/* ---- 路由健康记分卡 ---- */}
      <div className="mb-1.5 flex h-6 flex-wrap items-center justify-between gap-2">
        <h3 className="text-xs font-semibold">路由健康（最该处理的排前面）</h3>
        <div className="flex items-center gap-2 text-[10px] text-muted-foreground">
          <span><span className="text-danger">▲</span> 需处理 {counts.action}</span>
          <span><span className="text-warning">●</span> 关注 {counts.watch}</span>
          <span><span className="text-success">○</span> 健康 {counts.ok}</span>
          <div className="inline-flex rounded border border-border bg-muted/30 p-0.5">
            {([["failover", "顺延率"], ["ttft", "TTFT"]] as const).map(([value, label]) => (
              <button key={value} type="button" onClick={() => setSparkMode(value)}
                className={cn("rounded px-1.5 py-0.5", sparkMode === value ? "bg-background font-medium text-foreground shadow-sm" : "")}>
                {label}
              </button>
            ))}
          </div>
        </div>
      </div>

      {scorecard.error
        ? <p className="rounded-md border border-danger/25 bg-danger/5 p-3 text-[11px] text-danger">记分卡加载失败：{scorecard.error}</p>
        : !scorecard.data
        ? <div className="flex h-44 items-center justify-center rounded-md border border-border/70 bg-muted/20 text-[11px] text-muted-foreground">加载中…</div>
        : routes.length === 0
        ? <div className="flex h-44 items-center justify-center rounded-md border border-border/70 bg-muted/20 text-[11px] text-muted-foreground">当前窗口没有调度记录</div>
        : <div className="max-h-80 overflow-auto rounded-md border border-border/70">
            <table className="w-full min-w-[720px] border-collapse text-[11px]">
              <thead className="sticky top-0 z-10 bg-muted/60 backdrop-blur">
                <tr className="text-[10px] text-muted-foreground">
                  <th className="px-2 py-1.5 text-left font-medium">路由（来源 · 源分组）</th>
                  <th className="px-2 py-1.5 text-right font-medium" title="该路由失败并触发重试/顺延的尝试占比">顺延率</th>
                  <th className="px-2 py-1.5 text-right font-medium" title="该路由失败所在的请求链里，顺延次数最多的那条链顺延了几次">连深</th>
                  <th className="px-2 py-1.5 text-right font-medium" title="首字节耗时 P95">TTFT95</th>
                  <th className="px-2 py-1.5 text-left font-medium" title="按处理紧急度分级">错误 P0/P1/P2</th>
                  <th className="px-2 py-1.5 text-right font-medium">{sparkMode === "failover" ? "顺延率走向" : "TTFT 走向"}</th>
                </tr>
              </thead>
              <tbody>
                {routes.map((route) => {
                  const meta = HEALTH_META[route.health] ?? HEALTH_META.ok
                  const identity = formatScoreRouteIdentity(route)
                  return (
                    <tr key={route.route_id} className="border-t border-border/60 hover:bg-muted/30">
                      <td className="max-w-[280px] px-2 py-1.5">
                        <div className="flex items-start gap-1.5">
                          <span className={cn("mt-px shrink-0 text-[10px]", meta.className)} title={meta.label}>{meta.mark}</span>
                          <span className="min-w-0">
                            {route.alive ? (
                              // 深链接：网关页会切到对应组的路由标签、滚动定位并短暂高亮
                              <Link to={dispatchRoutePath(route.gateway_group_id, route.route_id)}
                                className="block truncate font-medium text-brand hover:underline" title={`${identity} — 点击跳转到该路由`}>
                                {identity}
                              </Link>
                            ) : (
                              <span className="block truncate font-medium text-muted-foreground" title={`${identity}（路由已删除）`}>{identity}</span>
                            )}
                            <span className="block truncate text-[10px] text-muted-foreground">
                              {route.gateway_group_name}
                              {!route.alive ? " · 已删除" : !route.enabled ? " · 已禁用" : ""}
                              {` · ${route.attempts} 次尝试`}
                            </span>
                          </span>
                        </div>
                      </td>
                      <td className={cn("px-2 py-1.5 text-right tabular-nums", route.failover_rate >= NEEDS_ACTION_FAILOVER ? "font-semibold text-danger" : route.failover_rate >= WATCH_FAILOVER ? "text-warning" : "text-muted-foreground")}>
                        {(route.failover_rate * 100).toFixed(1)}%
                      </td>
                      <td className={cn("px-2 py-1.5 text-right tabular-nums", route.max_failover_depth >= 2 ? "font-semibold text-danger" : "text-muted-foreground")}>
                        {route.max_failover_depth}
                      </td>
                      <td className={cn("px-2 py-1.5 text-right tabular-nums", route.ttft_p95 > WATCH_TTFT_MS ? "font-semibold text-warning" : "text-muted-foreground")}>
                        {formatTTFT(route.ttft_p95)}
                      </td>
                      <td className="px-2 py-1.5">
                        <span className="flex items-center gap-1.5">
                          {SEVERITY_META.map((severity) => {
                            const value = route.severity[severity.key]
                            return (
                              <span key={severity.key} title={severity.hint}
                                className={cn("tabular-nums", value === 0 && "text-muted-foreground/40")}
                                style={value > 0 ? { color: severity.color } : undefined}>
                                {severity.label} {value}
                              </span>
                            )
                          })}
                        </span>
                        {route.top_error ? <span className="mt-0.5 block max-w-[260px] truncate text-[10px] text-muted-foreground" title={route.top_error}>{route.top_error}</span> : null}
                      </td>
                      <td className="px-2 py-1.5 text-right text-muted-foreground">
                        <span className="inline-flex justify-end">
                          <Sparkline points={route.points} mode={sparkMode} max={sparkMode === "failover" ? sparkMax.failover : sparkMax.ttft} />
                        </span>
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>}

      {/* ---- 错误分布下钻 ---- */}
      <div className="mt-3 border-t border-border pt-3">
        <div className="mb-1.5 flex h-6 items-center justify-between">
          <h3 className="text-xs font-semibold">错误分布</h3>
          <div className="flex items-center gap-1.5 text-[10px]">
            <span className="text-muted-foreground">范围</span>
            <button type="button" onClick={() => { setErrorGatewayID(null); setErrorRouteID(null) }} className={cn("rounded px-1.5 py-0.5", errorGatewayID == null ? "bg-brand/10 font-medium text-brand" : "text-muted-foreground hover:bg-muted")}>全部网关</button>
            {errorGateway ? <><span className="text-muted-foreground">›</span><button type="button" onClick={() => setErrorRouteID(null)} className={cn("max-w-40 truncate rounded px-1.5 py-0.5", errorRouteID == null ? "bg-brand/10 font-medium text-brand" : "text-muted-foreground hover:bg-muted")}>{errorGateway.gateway_group_name}</button></> : null}
            {errorRoute ? <><span className="text-muted-foreground">›</span><span className="max-w-40 truncate rounded bg-brand/10 px-1.5 py-0.5 font-medium text-brand">{errorRoute.route_name}</span></> : null}
          </div>
        </div>
        {errors.error ? <p className="rounded-md border border-danger/25 bg-danger/5 p-3 text-[11px] text-danger">错误分布加载失败：{errors.error}</p>
          : !errorData ? <div className="flex h-44 items-center justify-center rounded-md border border-border/70 bg-muted/20 text-[11px] text-muted-foreground">加载中…</div>
          : errorData.failed_attempts === 0 ? <div className="flex h-44 items-center justify-center rounded-md border border-border/70 bg-muted/20 text-[11px] text-muted-foreground">当前窗口没有失败记录</div>
          : <div className="grid gap-3 lg:grid-cols-12">
          <div className="min-w-0 lg:col-span-3">
            <div className="mb-1 text-[10px] text-muted-foreground">按网关（P0 优先）</div>
            <div className="h-44 space-y-0.5 overflow-y-auto rounded-md border border-border/70 bg-muted/20 p-1.5">
              {errorData.groups.map((group) => <button type="button" key={group.gateway_group_id} onClick={() => { setErrorGatewayID((current) => current === group.gateway_group_id ? null : group.gateway_group_id); setErrorRouteID(null) }} aria-pressed={errorGatewayID === group.gateway_group_id} className={cn("flex w-full items-center gap-1.5 rounded px-1.5 py-1 text-left text-[11px] transition-colors hover:bg-background", errorGatewayID === group.gateway_group_id && "bg-background shadow-sm ring-1 ring-border")}>
                <span className="min-w-0 flex-1"><span className="block truncate font-medium" title={group.gateway_group_name}>{group.gateway_group_name}</span><span className="block text-[10px] text-muted-foreground">{`最终失败 ${group.final_failed} / ${group.requests} 请求`}</span></span>
                <span className="shrink-0 text-right"><span className={cn("block text-[11px] font-semibold tabular-nums", group.severity.p0 > 0 ? "text-danger" : "text-warning")}>{group.severity.p0 > 0 ? `P0 ${group.severity.p0}` : `P1 ${group.severity.p1}`}</span><span className="block text-[9px] text-muted-foreground">{group.failed_attempts} 失败尝试</span></span>
              </button>)}
            </div>
          </div>
          <div className="min-w-0 lg:col-span-3">
            <div className="mb-1 truncate text-[10px] text-muted-foreground">{errorGateway ? `${errorGateway.gateway_group_name} 下的路由` : "选中一个网关后可下钻到路由"}</div>
            <div className="h-44 space-y-0.5 overflow-y-auto rounded-md border border-border/70 bg-muted/20 p-1.5">
              {!errorGateway ? <p className="p-2 text-[11px] text-muted-foreground">左侧点选网关</p>
                : errorGateway.routes.filter((route) => route.failed_attempts > 0).length === 0 ? <p className="p-2 text-[11px] text-muted-foreground">该网关下没有失败的路由</p>
                : errorGateway.routes.filter((route) => route.failed_attempts > 0).map((route) => <button type="button" key={route.route_id} onClick={() => setErrorRouteID((current) => current === route.route_id ? null : route.route_id)} aria-pressed={errorRouteID === route.route_id} className={cn("flex w-full items-center gap-1.5 rounded px-1.5 py-1 text-left text-[11px] transition-colors hover:bg-background", errorRouteID === route.route_id && "bg-background shadow-sm ring-1 ring-border")}>
                  <span className="min-w-0 flex-1"><span className="block truncate font-medium" title={route.route_name}>{route.route_name}</span><span className="block truncate text-[10px] text-muted-foreground" title={route.provider_name}>{route.provider_name || `${route.attempts} 次尝试`}</span></span>
                  <span className="shrink-0 text-right"><span className="block text-[11px] font-semibold tabular-nums text-danger">{route.failed_attempts}</span><span className="block text-[9px] text-muted-foreground">{`${(route.attempt_error_rate * 100).toFixed(1)}%`}</span></span>
                </button>)}
            </div>
          </div>
          <div className="min-w-0 lg:col-span-3">
            <div className="mb-1 truncate text-[10px] text-muted-foreground">{scopeLabel}</div>
            <div className="flex h-44 flex-col rounded-md border border-border/70 bg-muted/20 p-1">
              <div ref={errorRef} className="min-h-0 w-full flex-1" />
              <div className="grid grid-cols-3 gap-1 pb-0.5 text-center">
                {SEVERITY_META.map((severity) => (
                  <div key={severity.key} title={severity.hint}>
                    <div className="text-[12px] font-semibold tabular-nums" style={{ color: errorScope.severity[severity.key] > 0 ? severity.color : undefined }}>
                      {errorScope.severity[severity.key]}
                    </div>
                    <div className="text-[9px] text-muted-foreground">{severity.label}</div>
                  </div>
                ))}
              </div>
            </div>
          </div>
          <div className="min-w-0 lg:col-span-3">
            <div className="mb-1 text-[10px] text-muted-foreground">高频错误（P0 优先）</div>
            <div className="flex h-44 flex-col gap-1 rounded-md border border-border/70 bg-muted/20 p-1.5">
              <div className="flex flex-wrap gap-x-2 gap-y-0.5">{errorScope.categories.map((category) => <span key={category.error_type} className="inline-flex items-center gap-1 text-[10px] text-muted-foreground"><span className="size-2 rounded-full" style={{ backgroundColor: ERROR_COLORS[category.error_type] ?? "#98a3b4" }} />{category.label} {category.count}</span>)}</div>
              <div className="min-h-0 flex-1 space-y-0.5 overflow-y-auto">
                {[...errorScope.samples].sort((a, b) => a.severity - b.severity || b.count - a.count).map((sample) => <div key={`${sample.error_type}-${sample.status_code}-${sample.message}`} className="rounded px-1 py-0.5 text-[10px] leading-tight">
                  <div className="flex items-center justify-between gap-1">
                    <span className="flex items-center gap-1">
                      <span className="rounded px-1 font-medium text-white" style={{ backgroundColor: SEVERITY_META[sample.severity]?.color ?? "#98a3b4" }} title={SEVERITY_META[sample.severity]?.hint}>{SEVERITY_META[sample.severity]?.label ?? "P?"}</span>
                      <span className="font-medium" style={{ color: ERROR_COLORS[sample.error_type] ?? "#98a3b4" }}>{sample.status_code > 0 ? sample.status_code : "无响应"}</span>
                    </span>
                    <span className="tabular-nums text-muted-foreground">{sample.count} 次</span>
                  </div>
                  <div className="truncate text-muted-foreground" title={sample.message}>{sample.message}</div>
                </div>)}
              </div>
            </div>
          </div>
        </div>}
      </div>
    </CardContent>
  </Card>
}
