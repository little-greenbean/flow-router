"use client"

import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { Link } from "react-router-dom"
import { Activity, ChevronRight, Copy, ExternalLink } from "lucide-react"
import * as echarts from "echarts"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { useGatewayDispatchFlow, useGatewayDispatchRawErrors } from "@/lib/queries"
import { useRefreshTick } from "@/lib/refresh-context"
import type {
  GatewayDispatchFlow,
  GatewayDispatchFlowNode,
  GatewayDispatchRawError,
  GatewayDispatchWindow,
} from "@/lib/api-types"
import {
  DISPATCH_RANGE_OPTIONS,
  dispatchRangeMinutes,
  dispatchRoutePath,
  flowShare,
  formatDuration,
  formatErrorClock,
  formatPercent,
} from "@/components/monitor/dispatch-health-utils"
import { cn } from "@/lib/utils"

/**
 * 调度情况面板 = 流向 + 原文。
 *
 * 上半部分用桑基图画请求怎么流：默认把窗口内所有请求按网关分流、再分到三种结局；
 * 点某个网关就下钻进去，按「第几跳打在哪条路由上」逐层铺开。顺延本来就是一条链上的
 * 流动，用桑基画比任何汇总表都直白——线有多粗就是有多少请求真的走了这条路。
 *
 * 下半部分是原始报错，刻意不归类、不合并、不截断：排查时要的就是上游原话。
 */

const FLOW_COLORS = {
  root: "#2878bd",
  gateway: "#2878bd",
  route: "#5a7fa6",
  overflow: "#7b61b3",
  direct: "#3f9d6a",
  recovered: "#db7b2b",
  failed: "#c45050",
} as const

function nodeColor(node: GatewayDispatchFlowNode): string {
  if (node.kind === "outcome") {
    return FLOW_COLORS[(node.outcome ?? "failed") as keyof typeof FLOW_COLORS] ?? FLOW_COLORS.failed
  }
  return FLOW_COLORS[node.kind as keyof typeof FLOW_COLORS] ?? FLOW_COLORS.route
}

/** 图高按最挤的那一列算：节点多就长一点，免得标签叠成一团。 */
function flowHeight(flow: GatewayDispatchFlow | null | undefined): number {
  if (!flow || flow.nodes.length === 0) return 280
  const perDepth = new Map<number, number>()
  for (const node of flow.nodes) perDepth.set(node.depth, (perDepth.get(node.depth) ?? 0) + 1)
  const widest = Math.max(...perDepth.values())
  return Math.min(640, Math.max(280, widest * 34 + 60))
}

function flowOption(flow: GatewayDispatchFlow) {
  const labels = new Map(flow.nodes.map((node) => [node.id, node.label]))
  const total = flow.requests
  // 最后一列的标签默认画在节点右边，会被画布裁掉，翻到左边去
  const maxDepth = flow.nodes.reduce((max, node) => Math.max(max, node.depth), 0)
  return {
    animationDuration: 240,
    tooltip: {
      trigger: "item" as const,
      confine: true,
      borderColor: "#dde3ea",
      textStyle: { fontSize: 11 },
      formatter: (params: unknown) => {
        const item = params as {
          dataType?: string
          data?: Record<string, unknown>
          value?: number
        }
        if (item.dataType === "edge") {
          const data = item.data as { source: string; target: string; value: number; failed?: boolean }
          const arrow = data.failed ? "→（失败后转走）→" : "→"
          return `${labels.get(data.source) ?? data.source} ${arrow} ${labels.get(data.target) ?? data.target}<br/><b>${data.value}</b> 请求 · ${formatPercent(flowShare(data.value, total))}`
        }
        const data = item.data as { raw?: GatewayDispatchFlowNode; hint?: string }
        const raw = data.raw
        if (!raw) return ""
        const value = Number(item.value ?? raw.value ?? 0)
        const lines = [`<b>${raw.label}</b>`, `${value} 请求 · 占全部 ${formatPercent(flowShare(value, total))}`]
        if (data.hint) lines.push(data.hint)
        return lines.join("<br/>")
      },
    },
    series: [
      {
        type: "sankey" as const,
        left: 8,
        right: 8,
        top: 10,
        bottom: 10,
        nodeGap: 10,
        nodeWidth: 12,
        nodeAlign: "justify" as const,
        draggable: false,
        emphasis: { focus: "adjacency" as const },
        // 不要把节点字段摊到 data 上：我们的 label 是字符串，ECharts 的 label 是配置对象，
        // 摊平会互相覆盖（点一下就把 label 对象当成名字塞进 React，直接白屏）。
        // 原始节点整个挂在 raw 上，tooltip 和 click 都从 raw 取。
        data: flow.nodes.map((node) => ({
          raw: node,
          name: node.id,
          value: node.value,
          depth: node.depth,
          itemStyle: { color: nodeColor(node), borderWidth: 0 },
          label: {
            fontSize: 10,
            color: "#4a5462",
            position: node.depth === maxDepth ? ("left" as const) : ("right" as const),
            // 用 id 当 name 保证唯一（同一条路由在不同跳是不同节点），
            // 所以标签必须自己给，不能让 ECharts 直接画 name
            formatter: () => node.label,
          },
          hint: node.kind === "route"
            ? `第 ${node.hop} 跳${node.alive === false ? " · 路由已删除" : " · 点击跳转到该路由"}`
            : node.kind === "gateway" && flow.scope === "all"
            ? "点击下钻到这个网关"
            : node.kind === "overflow"
            ? "更深的顺延都收在这里"
            : undefined,
        })),
        links: flow.links.map((link) => ({
          ...link,
          lineStyle: {
            color: link.failed ? FLOW_COLORS.failed : FLOW_COLORS.route,
            opacity: link.failed ? 0.34 : 0.2,
            curveness: 0.5,
          },
        })),
      },
    ],
  }
}

type ErrorScope = { gatewayID: number; gatewayName: string; routeID?: number; routeLabel?: string }

const RAW_LIMITS = [50, 100, 200]

function RawErrorRow({ item }: { item: GatewayDispatchRawError }) {
  const [open, setOpen] = useState(false)
  const [copied, setCopied] = useState(false)
  const extras = [item.detail, item.upstream_body, item.upstream_headers].filter(Boolean)

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(JSON.stringify(item, null, 2))
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    } catch {
      // 剪贴板被浏览器挡了就算了，展开的原文照样能手动选
      setCopied(false)
    }
  }

  return (
    <div className="border-t border-border/60 px-2 py-1.5 first:border-t-0">
      <div className="flex flex-wrap items-center gap-x-2 gap-y-0.5 text-[10px] text-muted-foreground">
        <span className="tabular-nums text-foreground">{formatErrorClock(item.timestamp)}</span>
        <span className={cn("rounded px-1 font-medium tabular-nums text-white",
          item.status_code >= 500 || item.status_code === 0 ? "bg-warning" : "bg-danger")}>
          {item.status_code > 0 ? item.status_code : "无响应"}
        </span>
        {item.error_type ? <span className="rounded bg-muted px-1">{item.error_type}</span> : null}
        <span className="truncate" title={`${item.gateway_group_name} · ${item.route_label ?? ""}`}>
          {item.gateway_group_name}
          {item.route_label ? <> · <span className="text-foreground">{item.route_label}</span></> : null}
        </span>
        {item.model ? <span className="truncate">{item.model}</span> : null}
        <span title="同一 request_id 下的第几次尝试">
          第 {item.attempt} 次{item.attempt_kind && item.attempt_kind !== "primary" ? ` · ${item.attempt_kind}` : ""}
        </span>
        <span>{formatDuration(item.duration_ms)}</span>
        <span className="ml-auto flex items-center gap-1">
          {extras.length > 0 ? (
            <button type="button" onClick={() => setOpen((value) => !value)}
              className="rounded px-1 hover:bg-muted hover:text-foreground">
              {open ? "收起原文" : `展开原文（${extras.length}）`}
            </button>
          ) : null}
          <button type="button" onClick={copy} title="复制整条记录（JSON）"
            className="inline-flex items-center gap-0.5 rounded px-1 hover:bg-muted hover:text-foreground">
            <Copy className="size-2.5" />{copied ? "已复制" : "复制"}
          </button>
        </span>
      </div>
      {/* 原文按上游返回的样子铺开：不截断、不改写、不合并 */}
      <pre className="mt-0.5 max-h-28 overflow-auto whitespace-pre-wrap break-all font-mono text-[10px] leading-tight text-foreground">
        {item.message || "（上游没有返回错误正文）"}
      </pre>
      {open ? (
        <div className="mt-1 space-y-1">
          {item.upstream_url ? <div className="font-mono text-[10px] text-muted-foreground">{item.upstream_url}</div> : null}
          {[["error_detail", item.detail], ["upstream_error_body", item.upstream_body], ["upstream_error_headers", item.upstream_headers]]
            .filter(([, value]) => Boolean(value))
            .map(([label, value]) => (
              <div key={label as string}>
                <div className="text-[9px] uppercase tracking-wide text-muted-foreground">{label}</div>
                <pre className="max-h-40 overflow-auto whitespace-pre-wrap break-all rounded bg-muted/40 p-1 font-mono text-[10px] leading-tight">
                  {value}
                </pre>
              </div>
            ))}
        </div>
      ) : null}
    </div>
  )
}

export function DispatchHealthPanel() {
  const [rangeValue, setRangeValue] = useState<GatewayDispatchWindow>("1h")
  const [drillGateway, setDrillGateway] = useState<{ id: number; name: string } | null>(null)
  const [scope, setScope] = useState<ErrorScope | null>(null)
  const [rawLimit, setRawLimit] = useState(RAW_LIMITS[0])
  const tick = useRefreshTick()

  // 跟着全局刷新 tick 一起前滚，窗口才是"最近 N"而不是"打开页面那一刻起的 N"
  const range = useMemo(() => {
    const to = new Date()
    const from = new Date(to.getTime() - dispatchRangeMinutes(rangeValue) * 60_000)
    return { from: from.toISOString(), to: to.toISOString() }
  }, [rangeValue, tick])

  const flow = useGatewayDispatchFlow(range.from, range.to, drillGateway?.id)
  const rawErrors = useGatewayDispatchRawErrors(
    range.from, range.to, scope?.gatewayID, scope?.routeID, rawLimit,
  )

  const chartRef = useRef<HTMLDivElement | null>(null)
  const chart = useRef<echarts.ECharts | null>(null)
  const flowData = flow.data

  // 下钻后如果那个网关在新窗口里没数据了，退回全部网关，免得卡在空图上
  useEffect(() => {
    if (!flowData || !drillGateway) return
    if (flowData.scope === "gateway" && flowData.requests === 0) {
      setDrillGateway(null)
      setScope(null)
    }
  }, [flowData, drillGateway])

  const handleNodeClick = useCallback((node: GatewayDispatchFlowNode) => {
    if (node.kind === "gateway" && !drillGateway) {
      setDrillGateway({ id: node.gateway_group_id ?? 0, name: node.label })
      setScope({ gatewayID: node.gateway_group_id ?? 0, gatewayName: node.label })
      return
    }
    if (node.kind === "route" && drillGateway) {
      // 点路由不跳走，只把下面的原始报错收窄到这条路由——跳转另有链接
      setScope((current) => current?.routeID === node.route_id
        ? { gatewayID: drillGateway.id, gatewayName: drillGateway.name }
        : { gatewayID: drillGateway.id, gatewayName: drillGateway.name, routeID: node.route_id, routeLabel: node.label })
    }
  }, [drillGateway])

  useEffect(() => {
    if (!chartRef.current) return
    if (!flowData || flowData.nodes.length === 0) {
      chart.current?.dispose()
      chart.current = null
      return
    }
    if (!chart.current) chart.current = echarts.init(chartRef.current)
    const instance = chart.current
    instance.setOption(flowOption(flowData), true)
    const onClick = (params: { dataType?: string; data?: unknown }) => {
      if (params.dataType !== "node") return
      const raw = (params.data as { raw?: GatewayDispatchFlowNode })?.raw
      if (raw) handleNodeClick(raw)
    }
    instance.off("click")
    instance.on("click", onClick)
    const resize = () => instance.resize()
    window.addEventListener("resize", resize)
    return () => window.removeEventListener("resize", resize)
  }, [flowData, handleNodeClick])

  useEffect(() => () => { chart.current?.dispose(); chart.current = null }, [])

  const outcomes = useMemo(() => {
    const result = { direct: 0, recovered: 0, failed: 0 }
    for (const node of flowData?.nodes ?? []) {
      if (node.kind === "outcome" && node.outcome && node.outcome in result) {
        result[node.outcome as keyof typeof result] = node.value
      }
    }
    return result
  }, [flowData])

  const height = flowHeight(flowData)
  const rawScopeLabel = scope?.routeLabel
    ? `${scope.gatewayName} · ${scope.routeLabel}`
    : scope
    ? scope.gatewayName
    : "全部网关"

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
      {/* ---- 请求流向 ---- */}
      <div className="mb-1 flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-center gap-1 text-xs">
          <h3 className="font-semibold">请求流向</h3>
          <button type="button" onClick={() => { setDrillGateway(null); setScope(null) }}
            className={cn("rounded px-1.5 py-0.5 text-[10px]", drillGateway ? "text-muted-foreground hover:bg-muted" : "bg-brand/10 font-medium text-brand")}>
            全部网关
          </button>
          {drillGateway ? <>
            <ChevronRight className="size-3 text-muted-foreground" />
            <span className="max-w-52 truncate rounded bg-brand/10 px-1.5 py-0.5 text-[10px] font-medium text-brand">{drillGateway.name}</span>
            {scope?.routeID ? <>
              <ChevronRight className="size-3 text-muted-foreground" />
              <span className="max-w-52 truncate rounded bg-muted px-1.5 py-0.5 text-[10px]">{scope.routeLabel}</span>
              <Link to={dispatchRoutePath(drillGateway.id, scope.routeID)}
                className="inline-flex items-center gap-0.5 rounded px-1 py-0.5 text-[10px] text-brand hover:underline">
                <ExternalLink className="size-2.5" />打开路由
              </Link>
            </> : null}
          </> : null}
        </div>
        <div className="flex items-center gap-2 text-[10px] text-muted-foreground">
          {flowData ? <>
            <span><b className="tabular-nums text-foreground">{flowData.requests}</b> 请求 / {flowData.attempts} 次尝试</span>
            <span style={{ color: FLOW_COLORS.direct }}>一次过 {outcomes.direct}</span>
            <span style={{ color: FLOW_COLORS.recovered }}>顺延后成功 {outcomes.recovered}</span>
            <span style={{ color: FLOW_COLORS.failed }}>最终失败 {outcomes.failed}</span>
            {flowData.max_hops > 1 ? <span>最深 {flowData.max_hops} 跳</span> : null}
          </> : null}
        </div>
      </div>
      <p className="mb-1 text-[10px] text-muted-foreground">
        {drillGateway
          ? "每一列是第几跳，线越粗走的请求越多；红色的线是「这一跳失败后转走的」。点路由可把下面的报错收窄到它。"
          : "点任意网关可下钻，看它内部顺延到哪几条路由。"}
      </p>

      {flow.error
        ? <p className="rounded-md border border-danger/25 bg-danger/5 p-3 text-[11px] text-danger">请求流向加载失败：{flow.error}</p>
        : !flowData
        ? <div className="flex h-56 items-center justify-center rounded-md border border-border/70 bg-muted/20 text-[11px] text-muted-foreground">加载中…</div>
        : flowData.nodes.length === 0
        ? <div className="flex h-56 items-center justify-center rounded-md border border-border/70 bg-muted/20 text-[11px] text-muted-foreground">当前窗口没有调度记录</div>
        : <div className="rounded-md border border-border/70 bg-muted/10">
            <div ref={chartRef} style={{ height }} className="w-full" />
          </div>}

      {/* ---- 原始报错 ---- */}
      <div className="mt-3 border-t border-border pt-3">
        <div className="mb-1.5 flex flex-wrap items-center justify-between gap-2">
          <h3 className="text-xs font-semibold">
            原始报错
            <span className="ml-1.5 font-normal text-[10px] text-muted-foreground">上游原话，不归类不合并不截断</span>
          </h3>
          <div className="flex items-center gap-2 text-[10px] text-muted-foreground">
            <span>范围 <b className="text-foreground">{rawScopeLabel}</b></span>
            {rawErrors.data ? <span>共 <b className="tabular-nums text-foreground">{rawErrors.data.total}</b> 条失败尝试</span> : null}
            <span className="inline-flex rounded border border-border bg-muted/30 p-0.5">
              {RAW_LIMITS.map((limit) => (
                <button key={limit} type="button" onClick={() => setRawLimit(limit)}
                  className={cn("rounded px-1.5 py-0.5", rawLimit === limit ? "bg-background font-medium text-foreground shadow-sm" : "")}>
                  最近 {limit}
                </button>
              ))}
            </span>
          </div>
        </div>
        {rawErrors.error
          ? <p className="rounded-md border border-danger/25 bg-danger/5 p-3 text-[11px] text-danger">原始报错加载失败：{rawErrors.error}</p>
          : !rawErrors.data
          ? <div className="flex h-44 items-center justify-center rounded-md border border-border/70 bg-muted/20 text-[11px] text-muted-foreground">加载中…</div>
          : rawErrors.data.items.length === 0
          ? <div className="flex h-44 items-center justify-center rounded-md border border-border/70 bg-muted/20 text-[11px] text-muted-foreground">当前范围没有失败记录</div>
          : <div className="max-h-[30rem] overflow-auto rounded-md border border-border/70">
              {rawErrors.data.items.map((item) => <RawErrorRow key={item.id} item={item} />)}
              {rawErrors.data.total > rawErrors.data.items.length ? (
                <p className="border-t border-border/60 px-2 py-1.5 text-[10px] text-muted-foreground">
                  仅显示最近 {rawErrors.data.items.length} 条，窗口内共 {rawErrors.data.total} 条——换更长的「最近 N」或缩短时间粒度可以看更多。
                </p>
              ) : null}
            </div>}
      </div>
    </CardContent>
  </Card>
}
