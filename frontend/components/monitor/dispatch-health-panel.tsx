"use client"

import { useEffect, useMemo, useRef, useState } from "react"
import { Activity } from "lucide-react"
import * as echarts from "echarts"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { useGatewayDispatchErrors, useGatewayDispatchTrends } from "@/lib/queries"
import type { DispatchTrendAggregation, DispatchTrendMetric, GatewayDispatchErrors, GatewayDispatchTrendPoint } from "@/lib/api-types"
import { cn } from "@/lib/utils"

type ZoomMode = "slider" | "follow"
type DataZoomRange = { start?: number; end?: number; startValue?: number; endValue?: number }
type DataZoomEventParams = DataZoomRange & { batch?: DataZoomRange[] }

const COLORS = ["#2878bd", "#db7b2b", "#3d9a6d", "#c45050", "#7b61b3", "#1e969b"]
const ERROR_COLORS: Record<string, string> = {
  http: "#c45050", transport: "#db7b2b", config: "#7b61b3", internal: "#b3527d", client: "#6b7787", "": "#98a3b4",
}

function errorPieOption(data: GatewayDispatchErrors) {
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

const METRICS: { value: DispatchTrendMetric; label: string; options: { value: DispatchTrendAggregation; label: string }[] }[] = [
  { value: "ttft", label: "TTFT", options: [["p95", "P95"], ["p90", "P90"], ["p50", "P50"], ["avg", "AVG"], ["max", "Max"]].map(([value, label]) => ({ value: value as DispatchTrendAggregation, label })) },
  { value: "quality", label: "调度质量", options: [["final_error", "最终错误率"], ["failover_trigger", "顺延触发率"], ["failover_recovery", "顺延恢复率"]].map(([value, label]) => ({ value: value as DispatchTrendAggregation, label })) },
  { value: "throughput", label: "吞吐量", options: [["rpm", "RPM"], ["requests", "请求数"]].map(([value, label]) => ({ value: value as DispatchTrendAggregation, label })) },
]

function initialRange() {
  const to = new Date()
  return { from: new Date(to.getTime() - 6 * 60 * 60 * 1000).toISOString(), to: to.toISOString() }
}

function pointValue(point: GatewayDispatchTrendPoint, aggregation: DispatchTrendAggregation): number | null {
  const values: Record<DispatchTrendAggregation, number> = {
    p50: point.ttft_p50, p90: point.ttft_p90, p95: point.ttft_p95, avg: point.ttft_avg, max: point.ttft_max,
    final_error: point.final_error_rate * 100, failover_trigger: point.failover_trigger_rate * 100,
    failover_recovery: point.failover_recovery_rate * 100, rpm: point.rpm, requests: point.requests,
  }
  const value = values[aggregation]
  return ["p50", "p90", "p95", "avg", "max"].includes(aggregation) ? (value > 0 ? value : null) : value
}

function metricUnit(metric: DispatchTrendMetric, aggregation: DispatchTrendAggregation) {
  if (metric === "ttft") return "ms"
  if (metric === "quality") return "%"
  return aggregation === "rpm" ? "req/min" : "requests"
}

type TooltipEntry = { seriesName?: string; color?: string; value?: unknown }

function formatMetricValue(value: number, metric: DispatchTrendMetric) {
  return metric === "quality" ? value.toFixed(1) : String(Math.round(value))
}

function tooltipHTML(raw: unknown, metric: DispatchTrendMetric, aggregation: DispatchTrendAggregation) {
  const entries = (Array.isArray(raw) ? raw : [raw]) as TooltipEntry[]
  const rows = entries
    .map((entry) => {
      const pair = Array.isArray(entry?.value) ? (entry.value as unknown[]) : null
      const value = pair && typeof pair[1] === "number" ? (pair[1] as number) : null
      const stamp = pair && pair[0] != null ? Date.parse(String(pair[0])) : Number.NaN
      return { name: entry?.seriesName ?? "", color: entry?.color ?? "#98a3b4", value, stamp }
    })
    .filter((row) => row.value != null)
    .sort((a, b) => (b.value as number) - (a.value as number))
  if (rows.length === 0) return ""
  const stamp = rows.find((row) => Number.isFinite(row.stamp))?.stamp
  const time = stamp == null ? "" : new Date(stamp).toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit" })
  const unit = metricUnit(metric, aggregation)
  const body = rows
    .map((row) => `<div style="display:flex;align-items:center;gap:6px;line-height:17px"><span style="width:7px;height:7px;border-radius:50%;background:${row.color};flex:none"></span><span style="flex:1;max-width:150px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">${row.name}</span><b style="margin-left:8px">${formatMetricValue(row.value as number, metric)}</b></div>`)
    .join("")
  return `<div style="min-width:120px"><div style="color:#707c8e;margin-bottom:3px">${time} · ${unit}</div>${body}</div>`
}

// ECharts 的 scale:true 只是「不强制含 0」，刻度仍会下探到它认为好看的整数，
// 379~1749 照样画成 0~2100。这里直接按实测区间加留白定死上下界；刻度间隔仍交给
// ECharts 自己算，并隐藏首尾两个标签——否则边界值和 interval 刻度混在一起会出现
// 500/600/900/1200 这种不规则序列。
function valueBounds(values: number[], metric: DispatchTrendMetric) {
  if (values.length === 0) return {}
  const low = Math.min(...values); const high = Math.max(...values)
  const pad = Math.max((high - low) * 0.12, Math.abs(high) * 0.03, 1)
  const min = Math.max(0, low - pad)
  let max = high + pad
  if (metric === "quality") max = Math.min(100, max)
  if (max <= min) max = min + 1
  return { min: Math.floor(min), max: Math.ceil(max) }
}

function chartOption(
  names: string[],
  points: Map<string, GatewayDispatchTrendPoint[]>,
  metric: DispatchTrendMetric,
  aggregation: DispatchTrendAggregation,
  axisRange: { from: string; to: string },
  visible: { from: string; to: string },
  zoom: ZoomMode,
  selectedName = "",
) {
  const series = names.map((name, index) => ({
    id: name,
    name, type: "line" as const, smooth: 0.38, smoothMonotone: "x" as const, connectNulls: true, showSymbol: false, triggerLineEvent: true,
    data: (points.get(name) ?? []).map((point) => [point.timestamp, pointValue(point, aggregation)]),
    lineStyle: { color: selectedName && selectedName !== name ? "#cbd2da" : COLORS[index % COLORS.length], width: selectedName === name ? 3.2 : 2.2, opacity: selectedName && selectedName !== name ? 0.42 : 1 },
    itemStyle: { color: selectedName && selectedName !== name ? "#cbd2da" : COLORS[index % COLORS.length] },
    emphasis: { disabled: true },
    cursor: "pointer",
  }))
  const seriesValues = series.flatMap((item) => item.data.map((pair) => pair[1]).filter((value): value is number => typeof value === "number"))
  return {
    animationDuration: 180, animationDurationUpdate: 0,
    grid: { left: 48, right: 18, top: 20, bottom: zoom === "slider" ? 56 : 30 },
    tooltip: {
      trigger: "axis", confine: true, borderColor: "#dde3ea", textStyle: { fontSize: 11 },
      axisPointer: { type: "line", snap: true, lineStyle: { color: "#98a3b4", width: 1, type: "dashed" } },
      formatter: (raw: unknown) => tooltipHTML(raw, metric, aggregation),
    },
    xAxis: { type: "time", min: Date.parse(zoom === "slider" ? axisRange.from : visible.from), max: Date.parse(zoom === "slider" ? axisRange.to : visible.to), boundaryGap: false, axisLabel: { color: "#707c8e", fontSize: 10, hideOverlap: true }, axisLine: { lineStyle: { color: "#ccd4de" } } },
    yAxis: { type: "value", scale: true, ...valueBounds(seriesValues, metric), axisLabel: { color: "#707c8e", fontSize: 10, showMinLabel: false, showMaxLabel: false }, splitLine: { lineStyle: { color: "#edf0f4" } } },
    ...(zoom === "slider" ? { dataZoom: [{ type: "inside", filterMode: "none", startValue: Date.parse(visible.from), endValue: Date.parse(visible.to) }, { type: "slider", filterMode: "none", height: 16, bottom: 3, startValue: Date.parse(visible.from), endValue: Date.parse(visible.to), showDetail: false, borderColor: "#d5dde6", fillerColor: "rgba(45,114,181,.14)", handleStyle: { color: "#2d72b5" } }] } : {}),
    series,
  }
}

function bucketMilliseconds(bucket: string): number {
  const minutes = Number.parseInt(bucket, 10)
  return (Number.isFinite(minutes) && minutes > 0 ? minutes : 5) * 60_000
}

export function DispatchHealthPanel() {
  const fullRange = useMemo(initialRange, [])
  const [visibleRange, setVisibleRange] = useState(fullRange)
  const [metric, setMetric] = useState<DispatchTrendMetric>("ttft")
  const [aggregation, setAggregation] = useState<DispatchTrendAggregation>("p95")
  const [selectedGatewayID, setSelectedGatewayID] = useState<number | null>(null)
  const [selectedRouteID, setSelectedRouteID] = useState<number | null>(null)
  const bucket = "5m"
  const trends = useGatewayDispatchTrends(fullRange.from, fullRange.to, bucket)
  const errors = useGatewayDispatchErrors(visibleRange.from, visibleRange.to)
  const gatewayRef = useRef<HTMLDivElement>(null); const routeRef = useRef<HTMLDivElement>(null); const errorRef = useRef<HTMLDivElement>(null)
  const gatewayChart = useRef<echarts.ECharts | null>(null); const routeChart = useRef<echarts.ECharts | null>(null); const errorChart = useRef<echarts.ECharts | null>(null)
  const visibleRangeRef = useRef(visibleRange)
  visibleRangeRef.current = visibleRange
  const activeMetric = METRICS.find((item) => item.value === metric) ?? METRICS[0]
  const groups = trends.data?.groups ?? []
  const currentGateway = groups.find((group) => group.gateway_group_id === selectedGatewayID) ?? groups[0]
  const routes = currentGateway?.routes ?? []
  const selectedGatewayName = selectedGatewayID == null ? "" : currentGateway?.gateway_group_name ?? ""
  const selectedRouteName = routes.find((route) => route.route_id === selectedRouteID)?.route_name ?? ""

  const bounds = useMemo(() => {
    const timestamps = groups.flatMap((group) => group.points.map((point) => Date.parse(point.timestamp))).filter(Number.isFinite)
    if (timestamps.length === 0) return fullRange
    const step = bucketMilliseconds(bucket)
    return { from: new Date(Math.min(...timestamps)).toISOString(), to: new Date(Math.max(...timestamps) + step).toISOString() }
  }, [groups, fullRange, bucket])

  useEffect(() => {
    const from = Math.max(Date.parse(bounds.from), Math.min(Date.parse(visibleRange.from), Date.parse(bounds.to) - bucketMilliseconds(bucket)))
    const to = Math.min(Date.parse(bounds.to), Math.max(Date.parse(visibleRange.to), from + bucketMilliseconds(bucket)))
    if (from !== Date.parse(visibleRange.from) || to !== Date.parse(visibleRange.to)) setVisibleRange({ from: new Date(from).toISOString(), to: new Date(to).toISOString() })
  }, [bounds, bucket, visibleRange.from, visibleRange.to])
  useEffect(() => { const timer = window.setInterval(() => trends.refetch(), 60_000); return () => window.clearInterval(timer) }, [fullRange.from, fullRange.to])
  useEffect(() => { if (!activeMetric.options.some((option) => option.value === aggregation)) setAggregation(activeMetric.options[0].value) }, [activeMetric, aggregation])
  useEffect(() => { setSelectedRouteID(null) }, [currentGateway?.gateway_group_id])

  useEffect(() => {
    if (!gatewayRef.current || !routeRef.current) return
    gatewayChart.current ??= echarts.init(gatewayRef.current); routeChart.current ??= echarts.init(routeRef.current)
    const gatewayPoints = new Map(groups.map((group) => [group.gateway_group_name, group.points]))
    const routePoints = new Map(routes.map((route) => [route.route_name, route.points]))
    gatewayChart.current.setOption(chartOption(groups.map((group) => group.gateway_group_name), gatewayPoints, metric, aggregation, bounds, visibleRange, "slider", selectedGatewayName), true)
    routeChart.current.setOption(chartOption(routes.map((route) => route.route_name), routePoints, metric, aggregation, bounds, visibleRange, "follow", selectedRouteName), true)
    gatewayChart.current.off("click").on("click", (params: echarts.ECElementEvent) => {
      const group = typeof params.seriesIndex === "number" ? groups[params.seriesIndex] : groups.find((item) => item.gateway_group_name === params.seriesName)
      if (group) setSelectedGatewayID(group.gateway_group_id)
    })
    routeChart.current.off("click").on("click", (params: echarts.ECElementEvent) => {
      const route = typeof params.seriesIndex === "number" ? routes[params.seriesIndex] : routes.find((item) => item.route_name === params.seriesName)
      if (route) setSelectedRouteID((current) => current === route.route_id ? null : route.route_id)
    })
    // 系列上的 click 只在命中图形时触发，点空白不会回调；用 zrender 层补一个：
    // target 为空即点在了画布空白处，此时清空选中，恢复全部趋势线。
    const gatewayZr = gatewayChart.current.getZr(); const routeZr = routeChart.current.getZr()
    gatewayZr.off("click"); routeZr.off("click")
    gatewayZr.on("click", (event: { target?: unknown }) => { if (!event.target) setSelectedGatewayID(null) })
    routeZr.on("click", (event: { target?: unknown }) => { if (!event.target) setSelectedRouteID(null) })
    const onDataZoom = (...args: unknown[]) => {
      const params = (args[0] ?? {}) as DataZoomEventParams
      const zoom = params.batch?.[0] ?? params; const boundsFrom = Date.parse(bounds.from); const boundsTo = Date.parse(bounds.to); const step = bucketMilliseconds(bucket)
      const rawStart = typeof zoom.startValue === "number" ? zoom.startValue : boundsFrom + ((zoom.start ?? 0) / 100) * (boundsTo - boundsFrom)
      const rawEnd = typeof zoom.endValue === "number" ? zoom.endValue : boundsFrom + ((zoom.end ?? 100) / 100) * (boundsTo - boundsFrom)
      const startValue = Math.max(boundsFrom, Math.min(rawStart, boundsTo - step)); const endValue = Math.min(boundsTo, Math.max(rawEnd, startValue + step))
      if (!Number.isFinite(startValue) || !Number.isFinite(endValue) || endValue <= startValue) return
      const nextFrom = new Date(startValue).toISOString(); const nextTo = new Date(endValue).toISOString()
      if (nextFrom === visibleRangeRef.current.from && nextTo === visibleRangeRef.current.to) return
      visibleRangeRef.current = { from: nextFrom, to: nextTo }
      setVisibleRange({ from: nextFrom, to: nextTo })
    }
    gatewayChart.current.off("datazoom").on("datazoom", onDataZoom)
    routeChart.current.off("datazoom")
    const resize = () => { gatewayChart.current?.resize(); routeChart.current?.resize() }
    window.addEventListener("resize", resize)
    return () => window.removeEventListener("resize", resize)
  }, [groups, routes, metric, aggregation, bounds, visibleRange, bucket, selectedGatewayName, selectedRouteName])

  const errorData = errors.data
  useEffect(() => {
    if (!errorRef.current) return
    if (!errorData || errorData.categories.length === 0) { errorChart.current?.dispose(); errorChart.current = null; return }
    errorChart.current ??= echarts.init(errorRef.current)
    errorChart.current.setOption(errorPieOption(errorData), true)
    const resize = () => errorChart.current?.resize()
    window.addEventListener("resize", resize)
    return () => window.removeEventListener("resize", resize)
  }, [errorData])

  const selectMetric = (next: DispatchTrendMetric) => { setMetric(next); const option = METRICS.find((item) => item.value === next)?.options[0]; if (option) setAggregation(option.value) }
  const summary = trends.data ? `${new Date(visibleRange.from).toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit" })}–${new Date(visibleRange.to).toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit" })} · ${trends.data.bucket}` : "加载中"

  return <Card className="overflow-hidden border border-border py-2 shadow-none sm:py-3">
    <CardHeader className="gap-2 px-3 pb-2 sm:flex sm:flex-row sm:items-center sm:justify-between sm:px-4"><CardTitle className="flex items-center gap-1.5 text-sm font-semibold"><Activity className="size-3.5 text-brand" />调度情况</CardTitle><div className="flex flex-wrap items-center justify-end gap-1.5"><div className="inline-flex rounded-md border border-border bg-muted/30 p-0.5">{METRICS.map((item) => <button key={item.value} type="button" onClick={() => selectMetric(item.value)} className={cn("h-6 rounded px-2 text-[11px]", metric === item.value ? "bg-background font-semibold shadow-sm" : "text-muted-foreground")}>{item.label}</button>)}</div><select aria-label="统计口径" value={aggregation} onChange={(event) => setAggregation(event.target.value as DispatchTrendAggregation)} className="h-7 rounded-md border border-border bg-background px-2 text-[11px]">{activeMetric.options.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}</select><span className="text-[10px] text-success">● 实时</span></div></CardHeader>
    <CardContent className="px-3 pb-2 sm:px-4">{trends.error ? <p className="rounded-md border border-danger/25 bg-danger/5 p-3 text-sm text-danger">调度趋势加载失败：{trends.error}</p> : <div className="grid gap-4 lg:grid-cols-2 xl:grid-cols-3">
      <section className="min-w-0"><div className="mb-1 flex h-6 items-center justify-between"><h3 className="text-xs font-semibold">网关趋势</h3><span className="text-[10px] text-muted-foreground">{summary} · {activeMetric.options.find((item) => item.value === aggregation)?.label}</span></div><div className="grid gap-3 sm:grid-cols-[minmax(132px,0.34fr)_minmax(0,1fr)]"><div className="max-h-64 overflow-y-auto rounded-md border border-border/70 bg-muted/20 p-1.5">{groups.length === 0 ? <p className="p-2 text-[11px] text-muted-foreground">暂无网关数据</p> : groups.map((group, index) => <button type="button" key={group.gateway_group_id} onClick={() => setSelectedGatewayID((current) => current === group.gateway_group_id ? null : group.gateway_group_id)} aria-pressed={selectedGatewayID === group.gateway_group_id} className={cn("flex w-full items-start gap-1.5 rounded px-1.5 py-1.5 text-left text-[11px] transition-colors hover:bg-background", currentGateway?.gateway_group_id === group.gateway_group_id && "bg-background shadow-sm ring-1 ring-border")}><span className="mt-0.5 size-2.5 shrink-0 rounded-full" style={{ backgroundColor: COLORS[index % COLORS.length] }} /><span className="min-w-0"><span className="block truncate font-medium" title={group.gateway_group_name}>{group.gateway_group_name}</span><span className="block truncate text-[10px] text-muted-foreground">{group.routes?.length ?? 0} 条路由</span></span></button>)}</div><div ref={gatewayRef} className="h-64 min-h-[240px] w-full" /></div></section>
      <section id="dispatch-route-trend" className="min-w-0 border-t border-border pt-3 lg:border-l lg:border-t-0 lg:pl-4 lg:pt-0"><div className="mb-1 flex h-6 items-center justify-between"><h3 className="text-xs font-semibold">路由趋势</h3><span className="rounded bg-brand/10 px-1.5 py-1 text-[10px] text-brand">网关：{currentGateway?.gateway_group_name ?? "暂无"}</span></div><div className="grid gap-3 sm:grid-cols-[minmax(132px,0.34fr)_minmax(0,1fr)]"><div className="max-h-64 overflow-y-auto rounded-md border border-border/70 bg-muted/20 p-1.5">{routes.length === 0 ? <p className="p-2 text-[11px] text-muted-foreground">暂无路由数据</p> : routes.map((route, index) => <button type="button" key={route.route_id} onClick={() => setSelectedRouteID((current) => current === route.route_id ? null : route.route_id)} aria-pressed={selectedRouteID === route.route_id} className={cn("flex w-full items-start gap-1.5 rounded px-1.5 py-1.5 text-left text-[11px] transition-colors hover:bg-background", selectedRouteID === route.route_id && "bg-background shadow-sm ring-1 ring-border")}><span className="mt-0.5 size-2.5 shrink-0 rounded-full" style={{ backgroundColor: COLORS[index % COLORS.length] }} /><span className="min-w-0"><span className="block truncate font-medium" title={route.route_name}>{route.route_name}</span><span className="block truncate text-[10px] text-muted-foreground" title={route.provider_name}>{route.provider_name || `${route.points.length} 个时间点`}</span></span></button>)}</div><div ref={routeRef} className="h-64 min-h-[240px] w-full" /></div></section>
      <section className="min-w-0 border-t border-border pt-3 xl:border-l xl:border-t-0 xl:pl-4 xl:pt-0">
        <div className="mb-1 flex h-6 items-center justify-between"><h3 className="text-xs font-semibold">错误分布</h3><span className="text-[10px] text-muted-foreground">{errorData ? `${errorData.failed_attempts} 次失败尝试` : "加载中"}</span></div>
        {errors.error ? <p className="rounded-md border border-danger/25 bg-danger/5 p-3 text-[11px] text-danger">错误分布加载失败：{errors.error}</p> : !errorData || errorData.categories.length === 0 ? <div className="flex h-64 items-center justify-center rounded-md border border-border/70 bg-muted/20 text-[11px] text-muted-foreground">当前窗口没有失败记录</div> : <div className="grid h-64 grid-cols-[minmax(0,0.9fr)_minmax(0,1.1fr)] gap-2">
          <div className="flex flex-col">
            <div ref={errorRef} className="min-h-0 w-full flex-1" />
            <div className="grid grid-cols-3 gap-1 text-center">
              <div><div className="text-[13px] font-semibold tabular-nums text-danger">{(errorData.error_rate * 100).toFixed(2)}%</div><div className="text-[10px] text-muted-foreground">错误率</div></div>
              <div><div className="text-[13px] font-semibold tabular-nums">{errorData.final_failed}</div><div className="text-[10px] text-muted-foreground">最终失败</div></div>
              <div><div className="text-[13px] font-semibold tabular-nums text-success">{errorData.recovered_requests}</div><div className="text-[10px] text-muted-foreground">顺延救回</div></div>
            </div>
          </div>
          <div className="flex min-h-0 flex-col gap-1.5">
            <div className="flex flex-wrap gap-x-2 gap-y-1">{errorData.categories.map((category) => <span key={category.error_type} className="inline-flex items-center gap-1 text-[10px] text-muted-foreground"><span className="size-2 rounded-full" style={{ backgroundColor: ERROR_COLORS[category.error_type] ?? "#98a3b4" }} />{category.label} {category.count}</span>)}</div>
            <div className="min-h-0 flex-1 space-y-1 overflow-y-auto rounded-md border border-border/70 bg-muted/20 p-1.5">
              {errorData.samples.map((sample) => <div key={`${sample.error_type}-${sample.status_code}-${sample.message}`} className="rounded px-1 py-1 text-[10px] leading-tight">
                <div className="flex items-center justify-between gap-1"><span className="inline-flex items-center gap-1 font-medium" style={{ color: ERROR_COLORS[sample.error_type] ?? "#98a3b4" }}>{sample.status_code > 0 ? sample.status_code : "无响应"}</span><span className="tabular-nums text-muted-foreground">{sample.count} 次</span></div>
                <div className="truncate text-muted-foreground" title={sample.message}>{sample.message}</div>
              </div>)}
            </div>
          </div>
        </div>}
      </section>
    </div>}</CardContent>
  </Card>
}
