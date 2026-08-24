"use client"

import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { useNavigate } from "react-router-dom"
import { Activity } from "lucide-react"
import * as echarts from "echarts"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { useGatewayDispatchFlow } from "@/lib/queries"
import { useRefreshTick } from "@/lib/refresh-context"
import type {
  GatewayDispatchFlow,
  GatewayDispatchFlowNode,
  GatewayDispatchWindow,
} from "@/lib/api-types"
import {
  DISPATCH_RANGE_OPTIONS,
  dispatchRangeMinutes,
  dispatchRoutePath,
  dispatchUsagePath,
  flowShare,
  formatPercent,
  routeColorIndex,
} from "@/components/monitor/dispatch-health-utils"
import { cn } from "@/lib/utils"

/**
 * 调度情况面板 = 一张请求流向桑基图。
 *
 * 顺延本来就是一条链上的流动，用桑基画比任何汇总表都直白——线有多粗就是有多少
 * 请求真的走了这条路。默认按网关分流，点 tag 直接换网关；下钻后按「第几跳打在
 * 哪条路由上」逐层铺开。图上每类节点都是一个动作：路由 → 跳到路由配置，
 * 结局 → 跳到使用记录并带上对应筛选。
 *
 * 下钻某个网关后，上方会出现按跳数分的高亮按钮：点一下不重新请求数据、也不
 * 重新铺一张图，只是在当前这张图上把该跳的节点和相邻连线点亮、其余调暗
 * （ECharts 的 highlight/downplay action，配合已有的 emphasis.focus:'adjacency'）。
 * 可选的跳数直接取自当前已经加载的数据里实际出现过的跳数，不是固定列表。
 */

/**
 * 路由配色。同一条路由不管出现在第几跳都用同一个颜色（见 routeColorIndex），
 * 顺着颜色就能把一条路由在图上串起来。挑的是同明度的一组，混在一张图里不打架。
 */
const ROUTE_PALETTE = [
  "#4c6ef5", "#12b886", "#f59f00", "#7048e8", "#0ca678",
  "#e8590c", "#1c7ed6", "#ae3ec9", "#087f5b", "#d6336c",
]

const FLOW_COLORS = {
  entry: "#364fc7",
  overflow: "#868e96",
  direct: "#2f9e44",
  recovered: "#f08c00",
  failed: "#e03131",
} as const

// 结局 → 使用记录的结果筛选。注意「最终失败」在图上是链级（最后一次尝试失败），
// 而使用记录的 fail 是尝试级且不含客户端断开，两者不是严格等价——但 fail 是
// 唯一能把所有真失败行都捞出来的筛选，跳过去正是要看这些行。
const OUTCOME_USAGE_FILTER: Record<string, string> = {
  direct: "success",
  recovered: "multi_success",
  failed: "fail",
}

const OUTCOME_HINT: Record<string, string> = {
  direct: "点击查看这些成功请求",
  recovered: "点击查看含重试/顺延且最终成功的请求",
  failed: "点击跳到使用记录，按失败筛选",
}

function nodeColor(node: GatewayDispatchFlowNode): string {
  switch (node.kind) {
    case "route":
      return ROUTE_PALETTE[routeColorIndex(node.route_id ?? 0, ROUTE_PALETTE.length)]
    case "outcome":
      return FLOW_COLORS[(node.outcome ?? "failed") as keyof typeof FLOW_COLORS] ?? FLOW_COLORS.failed
    case "overflow":
      return FLOW_COLORS.overflow
    default:
      return FLOW_COLORS.entry
  }
}

/**
 * 图高按最挤的那一列算。一定要留够：节点被压扁之后标签会叠在一起，
 * 甚至溢出画布压到下面的内容上。
 */
function flowHeight(flow: GatewayDispatchFlow | null | undefined): number {
  if (!flow || flow.nodes.length === 0) return 300
  const perDepth = new Map<number, number>()
  for (const node of flow.nodes) perDepth.set(node.depth, (perDepth.get(node.depth) ?? 0) + 1)
  const widest = Math.max(...perDepth.values())
  return Math.min(900, Math.max(300, widest * 44 + 90))
}

function flowOption(flow: GatewayDispatchFlow, chartHeight: number) {
  const nodeByID = new Map(flow.nodes.map((node) => [node.id, node]))
  const total = flow.requests
  // 最后一列的标签默认画在节点右边，会被画布裁掉，翻到左边去
  const maxDepth = flow.nodes.reduce((max, node) => Math.max(max, node.depth), 0)

  /**
   * ECharts 的桑基布局用的是**全图统一**的像素/值比例（源码 sankeyLayout.js 里的
   * minKy）：先给每一列各自算一个「(可用高度 - 本列间隙) / 本列 value 总和」，
   * 取全图最小的那个当缩放比例，所有节点都按它换算高度——不是每列单独把自己
   * 占满。这就是为什么第 1 跳里挤着一个 value=685 的大路由时，第 4 跳那两个
   * value=4/5 的小路由，无论整张图有多高都只有一两像素，标签必然叠成一团
   * （ECharts 的 sankey 系列不支持 labelLayout 防重叠，试过没用）。
   * 这里把 minKy 按同样的公式复现一遍，用估算出的真实像素高度决定要不要画
   * 标签——节点本身、连线、hover tooltip、点击都不受影响，只是常驻文字不占地方。
   */
  const depthTotals = new Map<number, number>()
  const depthCounts = new Map<number, number>()
  for (const node of flow.nodes) {
    depthTotals.set(node.depth, (depthTotals.get(node.depth) ?? 0) + node.value)
    depthCounts.set(node.depth, (depthCounts.get(node.depth) ?? 0) + 1)
  }
  const usableHeight = chartHeight - 28
  let minKy = Infinity
  for (const [depth, columnTotal] of depthTotals) {
    if (columnTotal <= 0) continue
    const gapTotal = Math.max(0, (depthCounts.get(depth) ?? 1) - 1) * 16
    const ky = Math.max(0, usableHeight - gapTotal) / columnTotal
    if (ky < minKy) minKy = ky
  }
  if (!Number.isFinite(minKy)) minKy = 0
  const MIN_LABEL_HEIGHT = 13
  const estimatedNodeHeight = (node: GatewayDispatchFlowNode): number => node.value * minKy

  return {
    animationDuration: 240,
    tooltip: {
      trigger: "item" as const,
      confine: true,
      borderColor: "#dde3ea",
      textStyle: { fontSize: 11 },
      formatter: (params: unknown) => {
        const item = params as { dataType?: string; data?: Record<string, unknown>; value?: number }
        if (item.dataType === "edge") {
          const data = item.data as { source: string; target: string; value: number; failed?: boolean }
          const arrow = data.failed ? "→（失败后转走）→" : "→"
          const source = nodeByID.get(data.source)?.label ?? data.source
          const target = nodeByID.get(data.target)?.label ?? data.target
          return `${source} ${arrow} ${target}<br/><b>${data.value}</b> 请求 · ${formatPercent(flowShare(data.value, total))}`
        }
        const raw = (item.data as { raw?: GatewayDispatchFlowNode })?.raw
        if (!raw) return ""
        const value = Number(item.value ?? raw.value ?? 0)
        const lines = [`<b>${raw.label}</b>`, `${value} 请求 · 占全部 ${formatPercent(flowShare(value, total))}`]
        const hint = raw.kind === "route"
          ? `第 ${raw.hop} 跳${raw.alive === false ? " · 路由已删除" : " · 点击跳转到该路由"}`
          : raw.kind === "gateway" && flow.scope === "all"
          ? "点击下钻到这个网关"
          : raw.kind === "overflow"
          ? "更深的顺延都收在这里"
          : raw.kind === "outcome"
          ? OUTCOME_HINT[raw.outcome ?? ""] ?? ""
          : ""
        if (hint) lines.push(hint)
        return lines.join("<br/>")
      },
    },
    series: [
      {
        type: "sankey" as const,
        left: 10,
        right: 10,
        top: 14,
        bottom: 14,
        nodeGap: 16,
        nodeWidth: 13,
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
          itemStyle: { color: nodeColor(node), borderWidth: 0, borderRadius: 2 },
          label: {
            fontSize: 10,
            color: "#3b444f",
            position: node.depth === maxDepth ? ("left" as const) : ("right" as const),
            // 标签会压在色带上，加一圈白描边才读得清
            textBorderColor: "rgba(255,255,255,.92)",
            textBorderWidth: 3,
            // 用 id 当 name 保证唯一（同一条路由在不同跳是不同节点），
            // 所以标签必须自己给，不能让 ECharts 直接画 name；
            // 节点太薄时干脆不给文字，省得跟邻居叠在一起（hover 还能看 tooltip）
            formatter: () => estimatedNodeHeight(node) >= MIN_LABEL_HEIGHT ? node.label : "",
          },
        })),
        links: flow.links.map((link) => {
          // 成功的流量跟着来源上色（顺着颜色能看出这股流量是谁发出去的），
          // 失败转走的一律标红——「哪条路在往外甩」是这张图最该一眼看到的事
          const source = nodeByID.get(link.source)
          const color = link.failed ? FLOW_COLORS.failed : source ? nodeColor(source) : FLOW_COLORS.entry
          return {
            ...link,
            lineStyle: { color, opacity: link.failed ? 0.42 : 0.24, curveness: 0.5 },
          }
        }),
      },
    ],
  }
}

export function DispatchHealthPanel() {
  const [rangeValue, setRangeValue] = useState<GatewayDispatchWindow>("1h")
  const [drillGateway, setDrillGateway] = useState<number | null>(null)
  // null = 不高亮任何一跳；否则是被选中的跳数（从当前数据里实际出现过的跳数中选）
  const [highlightHop, setHighlightHop] = useState<number | null>(null)
  const tick = useRefreshTick()
  const navigate = useNavigate()

  // 跟着全局刷新 tick 一起前滚，窗口才是"最近 N"而不是"打开页面那一刻起的 N"
  const range = useMemo(() => {
    const to = new Date()
    const from = new Date(to.getTime() - dispatchRangeMinutes(rangeValue) * 60_000)
    return { from, to, fromISO: from.toISOString(), toISO: to.toISOString() }
  }, [rangeValue, tick])

  const flow = useGatewayDispatchFlow(range.fromISO, range.toISO, drillGateway ?? undefined)
  const flowData = flow.data
  const chartRef = useRef<HTMLDivElement | null>(null)
  const chart = useRef<echarts.ECharts | null>(null)

  // 下钻的网关在新窗口里没流量了就退回全部，免得卡在空图上
  useEffect(() => {
    if (!flowData || drillGateway == null) return
    if (!flowData.gateways.some((gateway) => gateway.gateway_group_id === drillGateway)) {
      setDrillGateway(null)
    }
  }, [flowData, drillGateway])

  // 下钻到该网关内部才有「跳」这个概念——路由节点才带 hop，网关/结局节点没有
  const availableHops = useMemo(() => {
    if (!flowData) return []
    const hops = new Set<number>()
    for (const node of flowData.nodes) {
      if (node.kind === "route" && node.hop) hops.add(node.hop)
    }
    return [...hops].sort((a, b) => a - b)
  }, [flowData])

  // 换了网关/时间窗口后，之前选的那一跳可能已经不存在了（比如原来选第 4 跳，
  // 新数据最深只到第 2 跳），清掉免得按钮选中态和实际高亮对不上
  useEffect(() => {
    if (highlightHop != null && !availableHops.includes(highlightHop)) {
      setHighlightHop(null)
    }
  }, [availableHops, highlightHop])

  const handleNodeClick = useCallback((node: GatewayDispatchFlowNode) => {
    if (node.kind === "gateway" && node.gateway_group_id) {
      setDrillGateway((current) => current === node.gateway_group_id ? null : node.gateway_group_id!)
      return
    }
    if (node.kind === "route" && node.route_id && node.gateway_group_id && node.alive !== false) {
      navigate(dispatchRoutePath(node.gateway_group_id, node.route_id))
      return
    }
    if (node.kind === "outcome" && node.outcome) {
      const result = OUTCOME_USAGE_FILTER[node.outcome]
      if (!result) return
      navigate(dispatchUsagePath({
        group: drillGateway ?? undefined, result, from: range.from, to: range.to,
      }))
    }
  }, [drillGateway, navigate, range.from, range.to])

  const height = flowHeight(flowData)

  useEffect(() => {
    if (!chartRef.current) return
    // requests===0 时 nodes 里可能还剩一个孤零零的网关/根节点（没有 links）——同一个判断
    // 口径要跟下面渲染空态用的条件对齐，否则这里会去 setOption 一张没有连线的图，
    // 虽然反正盖在浮层底下看不见，但没必要让 ECharts 空跑
    if (!flowData || flowData.requests === 0) {
      chart.current?.dispose()
      chart.current = null
      return
    }
    if (!chart.current) chart.current = echarts.init(chartRef.current)
    const instance = chart.current
    instance.setOption(flowOption(flowData, height), true)
    // 容器高度会随节点数变，必须显式 resize——只 setOption 的话画布还是旧尺寸，
    // 内容会被压扁并溢出到下面的区块上
    instance.resize()
    const nodeByID = new Map(flowData.nodes.map((node) => [node.id, node]))
    const onClick = (params: { dataType?: string; data?: unknown }) => {
      if (params.dataType === "node") {
        const raw = (params.data as { raw?: GatewayDispatchFlowNode })?.raw
        if (raw) handleNodeClick(raw)
        return
      }
      if (params.dataType === "edge") {
        // 小节点的连线又细又难点中，点线本身也该算数：落到它流向的那个节点上
        // （分流线接的是路由/结局，直接导航过去；退回 source 只是兜底，理论上不会用到）
        const edge = params.data as { source?: string; target?: string }
        const target = edge.target ? nodeByID.get(edge.target) : undefined
        const source = edge.source ? nodeByID.get(edge.source) : undefined
        const resolved = target ?? source
        if (resolved) handleNodeClick(resolved)
      }
    }
    instance.off("click")
    instance.on("click", onClick)
    const resize = () => instance.resize()
    window.addEventListener("resize", resize)
    return () => window.removeEventListener("resize", resize)
  }, [flowData, height, handleNodeClick])

  useEffect(() => () => { chart.current?.dispose(); chart.current = null }, [])

  // 高亮某一跳：只在当前这张已经画好的图上调 ECharts 的 highlight/downplay action，
  // 不调 setOption、不碰数据——既不重新请求也不重新铺一张图。故意跟上面那个建图的
  // effect 分开，这样点高亮按钮时只会走这条轻量路径。
  useEffect(() => {
    const instance = chart.current
    if (!instance || !flowData) return
    instance.dispatchAction({ type: "downplay", seriesIndex: 0 })
    if (highlightHop == null) return
    const dataIndex = flowData.nodes
      .map((node, index) => (node.hop === highlightHop ? index : -1))
      .filter((index) => index >= 0)
    if (dataIndex.length > 0) {
      instance.dispatchAction({ type: "highlight", seriesIndex: 0, dataIndex })
    }
  }, [flowData, highlightHop])

  const outcomes = useMemo(() => {
    const result = { direct: 0, recovered: 0, failed: 0 }
    for (const node of flowData?.nodes ?? []) {
      if (node.kind === "outcome" && node.outcome && node.outcome in result) {
        result[node.outcome as keyof typeof result] = node.value
      }
    }
    return result
  }, [flowData])

  const gateways = flowData?.gateways ?? []

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
      <div className="mb-1 flex flex-wrap items-center justify-between gap-x-3 gap-y-1">
        <div className="flex min-w-0 flex-wrap items-center gap-1">
          <h3 className="mr-1 shrink-0 text-xs font-semibold">请求流向</h3>
          {/* 网关 tag：直接切图，不用回到全部再点一次 */}
          <button type="button" onClick={() => setDrillGateway(null)}
            className={cn("rounded px-1.5 py-0.5 text-[10px]", drillGateway == null ? "bg-brand/10 font-medium text-brand" : "text-muted-foreground hover:bg-muted")}>
            全部网关
          </button>
          {gateways.map((gateway) => (
            <button key={gateway.gateway_group_id} type="button"
              onClick={() => setDrillGateway(gateway.gateway_group_id)}
              title={`${gateway.name} · ${gateway.requests} 请求`}
              className={cn("max-w-44 truncate rounded px-1.5 py-0.5 text-[10px]",
                drillGateway === gateway.gateway_group_id
                  ? "bg-brand/10 font-medium text-brand"
                  : "text-muted-foreground hover:bg-muted")}>
              {gateway.name}
              <span className="ml-1 tabular-nums opacity-60">{gateway.requests}</span>
            </button>
          ))}
        </div>
        {flowData ? (
          <div className="flex shrink-0 items-center gap-2 text-[10px] text-muted-foreground">
            <span><b className="tabular-nums text-foreground">{flowData.requests}</b> 请求 / {flowData.attempts} 次尝试</span>
            <span style={{ color: FLOW_COLORS.direct }}>一次过 {outcomes.direct}</span>
            <span style={{ color: FLOW_COLORS.recovered }}>顺延后成功 {outcomes.recovered}</span>
            <span style={{ color: FLOW_COLORS.failed }}>最终失败 {outcomes.failed}</span>
            {flowData.max_hops > 1 ? <span>最深 {flowData.max_hops} 跳</span> : null}
          </div>
        ) : null}
      </div>
      {/* 顺延次数高亮：只在当前这张图上把选中的那一跳点亮、其余调暗，不重新拉数据、
          不重新铺图。可选项就是这批数据里实际出现过的跳数，不是写死的列表；只有
          下钻到具体网关时才有意义（全部网关视图里节点没有"跳"这个概念） */}
      {drillGateway != null && availableHops.length > 0 ? (
        <div className="mb-1.5 flex flex-wrap items-center gap-1.5">
          <span className="text-[10px] text-muted-foreground">顺延次数</span>
          <div className="inline-flex flex-wrap rounded-md border border-border bg-muted/30 p-0.5">
            <button type="button" onClick={() => setHighlightHop(null)}
              className={cn("h-6 rounded px-1.5 text-[11px]", highlightHop == null ? "bg-background font-semibold shadow-sm" : "text-muted-foreground")}>
              全部
            </button>
            {availableHops.map((hop) => (
              <button key={hop} type="button" onClick={() => setHighlightHop((current) => current === hop ? null : hop)}
                title={`高亮第 ${hop} 跳（该跳是这条链的第 ${hop - 1} 次顺延，如果中途没有同路由重试的话）`}
                className={cn("h-6 rounded px-1.5 text-[11px] tabular-nums", highlightHop === hop ? "bg-background font-semibold shadow-sm" : "text-muted-foreground")}>
                {hop - 1} 次
              </button>
            ))}
          </div>
        </div>
      ) : null}

      {flow.error
        ? <p className="rounded-md border border-danger/25 bg-danger/5 p-3 text-[11px] text-danger">请求流向加载失败：{flow.error}</p>
        : (() => {
            // 图表容器故意**始终挂载**，用浮层盖住加载中/空态，而不是像别处那样直接换成
            // 另一棵子树：那样切换时 chart 容器会被卸载，chartRef 变 null，图表初始化的
            // effect 里 `if (!chartRef.current) return` 会在 dispose 之前就退出，旧的
            // ECharts 实例既没释放也绑定不到新容器上——时间窗口在"有数据"和"没数据"之间
            // 来回切，图表会卡死在最后一次成功渲染的画面上。
            const empty = !flowData || flowData.requests === 0
            const emptyText = !flowData ? "加载中…" : "当前窗口没有调度记录"
            return (
              <div className="relative rounded-md border border-border/70 bg-background">
                <div ref={chartRef} style={{ height }} className="w-full" />
                {empty ? (
                  <div className="absolute inset-0 flex items-center justify-center bg-muted/20 text-[11px] text-muted-foreground">
                    {emptyText}
                  </div>
                ) : null}
              </div>
            )
          })()}
    </CardContent>
  </Card>
}
