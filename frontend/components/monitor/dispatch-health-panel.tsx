"use client"

import { useEffect, useMemo, useRef, useState } from "react"
import { Link } from "react-router-dom"
import { Activity, ChevronRight } from "lucide-react"
import * as echarts from "echarts"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { useGatewayDispatchAttention, useGatewayDispatchErrors } from "@/lib/queries"
import { useRefreshTick } from "@/lib/refresh-context"
import type {
  DispatchHealth,
  GatewayDispatchAttempt,
  GatewayDispatchAttentionGroup,
  GatewayDispatchAttentionRoute,
  GatewayDispatchErrorScope,
  GatewayDispatchWindow,
} from "@/lib/api-types"
import {
  DISPATCH_RANGE_OPTIONS,
  NEEDS_ACTION_FAILOVER,
  NEEDS_ACTION_FAIL_STREAK,
  WATCH_FAILOVER,
  WATCH_FAIL_STREAK,
  WATCH_TTFT_MS,
  dispatchRangeMinutes,
  dispatchRoutePath,
  formatRouteIdentity,
  formatTTFT,
} from "@/components/monitor/dispatch-health-utils"
import { cn } from "@/lib/utils"

/**
 * 调度情况面板。
 *
 * 组织方式跟着运维动作走：网关是操作单位（一个网关一组备选路由），所以「建议关注」
 * 先按网关折叠，展开才看是哪条路由在拖后腿，最后用「最近请求状态」把抽象的百分比
 * 落到一格一格具体的尝试上。下半部分保留错误下钻，回答「到底错在哪」。
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

const SUCCESS_COLOR = "#3f9d6a"

const HEALTH_META: Record<DispatchHealth, { mark: string; label: string; className: string }> = {
  action: { mark: "▲", label: "需处理", className: "text-danger" },
  watch: { mark: "●", label: "关注", className: "text-warning" },
  ok: { mark: "○", label: "健康", className: "text-success" },
}

const ATTEMPT_KIND_LABEL: Record<string, string> = {
  primary: "首发", retry: "重试", failover: "顺延",
}

function severityColor(severity: number): string {
  return SEVERITY_META[severity]?.color ?? SUCCESS_COLOR
}

function formatClock(iso: string): string {
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return iso
  return date.toLocaleTimeString("zh-CN", { hour12: false })
}

function attemptTitle(mark: GatewayDispatchAttempt): string {
  const kind = ATTEMPT_KIND_LABEL[mark.attempt_kind ?? "primary"] ?? mark.attempt_kind ?? ""
  const head = [formatClock(mark.timestamp), kind, mark.success ? "成功" : (SEVERITY_META[mark.severity]?.label ?? "失败")]
  if (mark.status_code) head.push(String(mark.status_code))
  const tail: string[] = []
  if (mark.model) tail.push(mark.model)
  if (mark.first_token_ms) tail.push(`首字 ${formatTTFT(mark.first_token_ms)}`)
  if (mark.message) tail.push(mark.message)
  return tail.length > 0 ? `${head.join(" · ")}\n${tail.join("\n")}` : head.join(" · ")
}

/**
 * 最近请求状态条：一格一次尝试，左旧右新。
 *
 * 这比趋势折线更适合当前这个决策——折线把「20% 失败率」摊平成一条线，看不出
 * 那 20% 是均匀散着（抖动）还是连着一片（挂了）。一格一格摆出来，连败一眼可见。
 */
function RecentStrip({ marks }: { marks: GatewayDispatchAttempt[] }) {
  if (marks.length === 0) {
    return <span className="text-[10px] text-muted-foreground">窗口内无尝试</span>
  }
  return (
    <span className="inline-flex flex-wrap items-center gap-[2px]">
      {marks.map((mark, index) => (
        <span
          key={`${mark.timestamp}-${index}`}
          title={attemptTitle(mark)}
          className="h-3.5 w-2 rounded-[1px]"
          style={{ backgroundColor: mark.success ? SUCCESS_COLOR : severityColor(mark.severity), opacity: mark.success ? 0.55 : 1 }}
        />
      ))}
    </span>
  )
}

function SeverityCounts({ severity, className }: { severity: { p0: number; p1: number; p2: number }; className?: string }) {
  return (
    <span className={cn("inline-flex items-center gap-1.5 tabular-nums", className)}>
      {SEVERITY_META.map((item) => {
        const value = severity[item.key]
        return (
          <span key={item.key} title={item.hint} className={cn(value === 0 && "text-muted-foreground/40")}
            style={value > 0 ? { color: item.color } : undefined}>
            {item.label} {value}
          </span>
        )
      })}
    </span>
  )
}

/** 展开后的一条路由：身份 + 指标 + 最近请求状态。 */
function AttentionRoute({ route }: { route: GatewayDispatchAttentionRoute }) {
  const meta = HEALTH_META[route.health] ?? HEALTH_META.ok
  const identity = formatRouteIdentity(route)
  return (
    <div className="border-t border-border/50 px-2 py-1.5 first:border-t-0">
      <div className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
        <span className={cn("text-[10px]", meta.className)} title={meta.label}>{meta.mark}</span>
        {route.alive ? (
          // 深链接：网关页会切到对应组的路由标签、滚动定位并短暂高亮
          <Link to={dispatchRoutePath(route.gateway_group_id, route.route_id)}
            className="max-w-[300px] truncate text-[11px] font-medium text-brand hover:underline"
            title={`${identity} — 点击跳转到该路由`}>
            {identity}
          </Link>
        ) : (
          <span className="max-w-[300px] truncate text-[11px] font-medium text-muted-foreground" title={`${identity}（路由已删除）`}>
            {identity}
          </span>
        )}
        <span className="text-[10px] text-muted-foreground">
          {!route.alive ? "已删除 · " : !route.enabled ? "已禁用 · " : ""}
          {route.attempts} 次尝试 / {route.requests} 请求
        </span>
      </div>

      <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-[10px] text-muted-foreground">
        <span title="该路由失败并触发重试/顺延的尝试占比">
          顺延率{" "}
          <b className={cn("tabular-nums", route.failover_rate >= NEEDS_ACTION_FAILOVER ? "text-danger" : route.failover_rate >= WATCH_FAILOVER ? "text-warning" : "text-foreground")}>
            {(route.failover_rate * 100).toFixed(1)}%
          </b>
        </span>
        <span title="窗口末尾还连着败几次 / 窗口内最长的一段连败">
          连败{" "}
          <b className={cn("tabular-nums", route.current_fail_streak >= NEEDS_ACTION_FAIL_STREAK ? "text-danger" : route.current_fail_streak >= WATCH_FAIL_STREAK ? "text-warning" : "text-foreground")}>
            {route.current_fail_streak}
          </b>
          <span className="text-muted-foreground/70"> 次（最长 {route.max_fail_streak}）</span>
        </span>
        <span title="该路由失败所在的请求链里，顺延次数最多的那条链顺延了几次">
          连深 <b className={cn("tabular-nums", route.max_failover_depth >= 2 ? "text-danger" : "text-foreground")}>{route.max_failover_depth}</b>
        </span>
        <span title="首字节耗时 P95">
          TTFT95 <b className={cn("tabular-nums", route.ttft_p95 > WATCH_TTFT_MS ? "text-warning" : "text-foreground")}>{formatTTFT(route.ttft_p95)}</b>
        </span>
        <SeverityCounts severity={route.severity} />
      </div>

      <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1">
        <span className="text-[10px] text-muted-foreground">最近</span>
        <RecentStrip marks={route.recent} />
        {route.top_error ? (
          <span className="max-w-[420px] truncate text-[10px] text-muted-foreground" title={route.top_error}>{route.top_error}</span>
        ) : null}
      </div>
    </div>
  )
}

/** 一个网关的折叠行。收起时只给「要不要展开」需要的信息。 */
function AttentionGroup({
  group, open, onToggle, showHealthy,
}: {
  group: GatewayDispatchAttentionGroup
  open: boolean
  onToggle: () => void
  showHealthy: boolean
}) {
  const meta = HEALTH_META[group.health] ?? HEALTH_META.ok
  const visible = showHealthy ? group.routes : group.routes.filter((route) => route.health !== "ok")
  return (
    <div className="rounded-md border border-border/70">
      <button type="button" onClick={onToggle} aria-expanded={open}
        className="flex w-full flex-wrap items-center gap-x-3 gap-y-1 px-2 py-1.5 text-left hover:bg-muted/30">
        <ChevronRight className={cn("size-3 shrink-0 text-muted-foreground transition-transform", open && "rotate-90")} />
        <span className={cn("text-[10px]", meta.className)} title={meta.label}>{meta.mark}</span>
        <span className="min-w-0 flex-1 truncate text-[11px] font-medium" title={group.gateway_group_name}>{group.gateway_group_name}</span>
        <span className="text-[10px] text-muted-foreground" title="窗口内至少顺延过一次的请求占比（链级：一条请求算一次，不管跨了几条路由）">
          顺延请求{" "}
          <b className={cn("tabular-nums", group.request_failover_rate >= NEEDS_ACTION_FAILOVER ? "text-danger" : group.request_failover_rate >= WATCH_FAILOVER ? "text-warning" : "text-foreground")}>
            {(group.request_failover_rate * 100).toFixed(1)}%
          </b>
          <span className="text-muted-foreground/70"> {group.failover_requests}/{group.requests}</span>
        </span>
        <span className="text-[10px] text-muted-foreground" title="顺延完仍然没救回来的请求数">
          最终失败 <b className={cn("tabular-nums", group.failed_requests > 0 ? "text-danger" : "text-foreground")}>{group.failed_requests}</b>
        </span>
        <SeverityCounts severity={group.severity} className="text-[10px]" />
        <span className="text-[10px] tabular-nums text-muted-foreground">
          问题路由 <b className={group.problem_routes > 0 ? "text-warning" : "text-foreground"}>{group.problem_routes}</b>/{group.routes.length}
        </span>
      </button>
      {open ? (
        <div className="border-t border-border/70 bg-muted/10">
          {visible.length === 0
            ? <p className="px-2 py-2 text-[10px] text-muted-foreground">该网关下没有需要关注的路由（勾选「含健康路由」可看全部 {group.routes.length} 条）</p>
            : visible.map((route) => <AttentionRoute key={route.route_id} route={route} />)}
        </div>
      ) : null}
    </div>
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
  const [rangeValue, setRangeValue] = useState<GatewayDispatchWindow>("1h")
  const [showHealthy, setShowHealthy] = useState(false)
  // null = 还没手动点过，用默认展开（最该处理的那个网关）
  const [openGroups, setOpenGroups] = useState<number[] | null>(null)
  const [errorGatewayID, setErrorGatewayID] = useState<number | null>(null)
  const [errorRouteID, setErrorRouteID] = useState<number | null>(null)
  const tick = useRefreshTick()

  // 跟着全局刷新 tick 一起前滚，窗口才是"最近 N"而不是"打开页面那一刻起的 N"
  const range = useMemo(() => {
    const to = new Date()
    const from = new Date(to.getTime() - dispatchRangeMinutes(rangeValue) * 60_000)
    return { from: from.toISOString(), to: to.toISOString() }
  }, [rangeValue, tick])

  const attention = useGatewayDispatchAttention(range.from, range.to)
  const errors = useGatewayDispatchErrors(range.from, range.to)
  const groups = useMemo(() => attention.data?.groups ?? [], [attention.data])
  const errorData = errors.data

  // 默认展开排第一的网关（排序已经保证它最该处理），手动点过之后完全交给用户
  const effectiveOpen = useMemo(() => {
    if (openGroups != null) return new Set(openGroups)
    const first = groups.find((group) => group.problem_routes > 0) ?? groups[0]
    return new Set(first ? [first.gateway_group_id] : [])
  }, [openGroups, groups])

  const toggleGroup = (id: number) => {
    setOpenGroups((current) => {
      const next = new Set(current ?? effectiveOpen)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return [...next]
    })
  }

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
        <div className="inline-flex flex-wrap rounded-md border border-border bg-muted/30 p-0.5">
          {DISPATCH_RANGE_OPTIONS.map((item) => (
            <button key={item.value} type="button" onClick={() => setRangeValue(item.value)}
              className={cn("h-6 rounded px-1.5 text-[11px]", rangeValue === item.value ? "bg-background font-semibold shadow-sm" : "text-muted-foreground")}>
              {item.label}
            </button>
          ))}
        </div>
        <span className="text-[10px] text-success">● 实时</span>
      </div>
    </CardHeader>

    <CardContent className="px-3 pb-2 sm:px-4">
      {/* ---- 建议关注 ---- */}
      <div className="mb-1.5 flex h-6 flex-wrap items-center justify-between gap-2">
        <h3 className="text-xs font-semibold">建议关注</h3>
        <div className="flex items-center gap-2 text-[10px] text-muted-foreground">
          <span>
            <span className="text-danger">▲</span> 需处理 / <span className="text-warning">●</span> 关注：
            <b className="tabular-nums text-foreground"> {attention.data?.problem_routes ?? 0}</b> / {attention.data?.routes ?? 0} 条路由
          </span>
          <label className="inline-flex cursor-pointer items-center gap-1">
            <input type="checkbox" className="size-3 accent-current" checked={showHealthy} onChange={(event) => setShowHealthy(event.target.checked)} />
            含健康路由
          </label>
        </div>
      </div>

      {attention.error
        ? <p className="rounded-md border border-danger/25 bg-danger/5 p-3 text-[11px] text-danger">建议关注加载失败：{attention.error}</p>
        : !attention.data
        ? <div className="flex h-44 items-center justify-center rounded-md border border-border/70 bg-muted/20 text-[11px] text-muted-foreground">加载中…</div>
        : groups.length === 0
        ? <div className="flex h-44 items-center justify-center rounded-md border border-border/70 bg-muted/20 text-[11px] text-muted-foreground">当前窗口没有调度记录</div>
        : <div className="max-h-[34rem] space-y-1 overflow-auto pr-0.5">
            {groups.map((group) => (
              <AttentionGroup key={group.gateway_group_id} group={group} showHealthy={showHealthy}
                open={effectiveOpen.has(group.gateway_group_id)} onToggle={() => toggleGroup(group.gateway_group_id)} />
            ))}
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
