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
  GatewayDispatchFlowHighlight,
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
 * 下钻某个网关后，上方会出现按顺延次数分的高亮按钮：点一下不重新请求数据、也不
 * 重新铺一张图，只把「顺延了这么多次的链，从入口到结局的完整路径」调亮、其余调暗
 * （成员由后端按整条链算好，见 highlights）。可选次数取自这批数据里实际出现过的
 * 次数，不是写死的列表。
 *
 * 高亮只认点击，hover 一律不改图（emphasis.focus:'none'）。桑基图上一个节点同时
 * 属于很多条链，"我正悬停的那条链"根本没有唯一解——ECharts 的 adjacency 只看
 * "这个节点挨着哪些边"，trajectory 更是把经过它的所有链全铺开，两种都会点亮一堆
 * 别人的链，还会把点出来的高亮冲掉。悬停就只出 tooltip，别的什么都不动。
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

const NODE_WIDTH = 13
const NODE_GAP = 16
/** 结局列的标签画在节点左边，跟倒数第二列的右侧标签抢同一条缝，给它留一块固定的地 */
const OUTCOME_LABEL_RESERVE = 62

function flowOption(
  flow: GatewayDispatchFlow,
  chartHeight: number,
  chartWidth: number,
  highlight: GatewayDispatchFlowHighlight | null,
  animate: boolean,
) {
  const nodeByID = new Map(flow.nodes.map((node) => [node.id, node]))
  const total = flow.requests
  // 高亮的是「这个顺延次数的链，从入口到结局的完整路径」，成员在后端已经按整条链
  // 并好了；这里只管按成员身份分两档不透明度，别的不管——不用 ECharts 自带的
  // emphasis.focus:'adjacency' 去猜，因为它是按"这个节点挨着哪些边"算的，多条链
  // 共享同一个入口节点时会把不属于这次高亮的边也一起点亮，等于又把片段以外的
  // 东西带出来了
  const highlightNodes = highlight ? new Set(highlight.node_ids) : null
  const highlightLinks = highlight ? new Set(highlight.link_keys) : null
  // 最后一列的标签默认画在节点右边，会被画布裁掉，翻到左边去
  const maxDepth = flow.nodes.reduce((max, node) => Math.max(max, node.depth), 0)

  /**
   * 标签为什么改成"截断"而不是上一版的"矮节点干脆不画"：
   *
   * 桑基的 nodeGap 是**硬约束**——sankeyLayout.js 里 resolveCollisions 每一轮都会把
   * 同一列的节点重新撑开到至少 nodeGap，minKy 又保证了最挤的那列刚好放得下，所以
   * 同列两个标签垂直方向永远隔着 16px，10px 的字压根叠不到一起。按节点高度藏标签
   * 是在治一个不存在的病，代价却极大：ECharts 的像素/值比例是**全图统一**的
   * （minKy 取所有列里最小的那个），第 1 跳只要有个几千请求的大路由，后面几跳那些
   * 几十请求的节点就只有零点几个像素高，于是「顺延到底走了哪几条路由」——正是这张
   * 图最该说清楚的事——名字全没了。
   *
   * 真正会叠的是**横向**：标签画在节点右边，名字一长就压到下一列的节点和标签上。
   * 所以按列间距给每个标签一个宽度上限，超出的截断，全名留给 tooltip。
   */
  const layoutWidth = Math.max(0, chartWidth - 20) // 跟下面 series 的 left/right 对齐
  const columnGap = maxDepth > 0 ? (layoutWidth - NODE_WIDTH) / maxDepth : layoutWidth
  const labelWidth = (depth: number): number => {
    // 结局列是我们自己的短词（一次过 / 顺延后成功 / 最终失败），按预留的那块地算
    if (depth === maxDepth) return Math.max(48, OUTCOME_LABEL_RESERVE)
    const reserve = depth === maxDepth - 1 ? OUTCOME_LABEL_RESERVE : 0
    return Math.max(48, columnGap - NODE_WIDTH - 18 - reserve)
  }

  return {
    // 只在数据真的变了（换网关/换时间窗口/首次加载）才播入场动画；
    // 单纯切换高亮不该有整张图重新长出来的动画，那看着就是「重新生成了一张图」
    animationDuration: animate ? 240 : 0,
    animationDurationUpdate: animate ? 240 : 0,
    tooltip: {
      trigger: "item" as const,
      confine: true,
      // ECharts 默认把 tooltip 挂到 document.body 上（appendToBody 默认 true），
      // 定位算的是页面坐标——真出过偏差的话，一个跟卡片毫不相关的小色块就会飘在
      // 页面别的地方，confine:true 按理说会把它按住，但既然挂在 body 上就已经跳出了
      // 咱们卡片自己的 DOM 子树，Card 上的 overflow-hidden 对它完全不起作用。
      // 改成挂在图表自己的容器里（下面 chartRef 那层给了 position:relative +
      // overflow:hidden），tooltip 就只能在这层容器内部定位、被这层裁切，
      // 无论算出什么坐标都跳不出这张卡片。
      appendToBody: false,
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
        nodeGap: NODE_GAP,
        nodeWidth: NODE_WIDTH,
        nodeAlign: "justify" as const,
        draggable: false,
        // hover 只出 tooltip：focus:'none' 不虚化任何东西；正高亮着的时候连自身的
        // emphasis 样式都关掉，免得鼠标一扫就把调暗的节点重新点亮，跟高亮打架
        emphasis: { focus: "none" as const, disabled: highlight != null },
        // 不要把节点字段摊到 data 上：我们的 label 是字符串，ECharts 的 label 是配置对象，
        // 摊平会互相覆盖（点一下就把 label 对象当成名字塞进 React，直接白屏）。
        // 原始节点整个挂在 raw 上，tooltip 和 click 都从 raw 取。
        data: flow.nodes.map((node) => {
          const dimmed = highlightNodes != null && !highlightNodes.has(node.id)
          return {
            raw: node,
            name: node.id,
            value: node.value,
            depth: node.depth,
            itemStyle: { color: nodeColor(node), borderWidth: 0, borderRadius: 2, opacity: dimmed ? 0.15 : 1 },
            label: {
              fontSize: 10,
              color: "#3b444f",
              opacity: dimmed ? 0.35 : 1,
              position: node.depth === maxDepth ? ("left" as const) : ("right" as const),
              // 标签会压在色带上，加一圈白描边才读得清
              textBorderColor: "rgba(255,255,255,.92)",
              textBorderWidth: 3,
              // 长名字压到下一列去正是之前那个"重叠"，按列间距截断；全名看 tooltip
              width: labelWidth(node.depth),
              overflow: "truncate" as const,
              ellipsis: "…",
              // 用 id 当 name 保证唯一（同一条路由在不同跳是不同节点），
              // 所以标签必须自己给，不能让 ECharts 直接画 name
              formatter: () => node.label,
            },
          }
        }),
        links: flow.links.map((link) => {
          // 成功的流量跟着来源上色（顺着颜色能看出这股流量是谁发出去的），
          // 失败转走的一律标红——「哪条路在往外甩」是这张图最该一眼看到的事
          const source = nodeByID.get(link.source)
          const color = link.failed ? FLOW_COLORS.failed : source ? nodeColor(source) : FLOW_COLORS.entry
          const baseOpacity = link.failed ? 0.42 : 0.24
          const inHighlight = highlightLinks?.has(`${link.source}|${link.target}`)
          // 命中的连线适当调亮一点，在被调暗的背景里才立得住；没命中的直接压得很淡
          const opacity = highlightLinks == null ? baseOpacity : inHighlight ? Math.min(1, baseOpacity + 0.35) : 0.03
          return {
            ...link,
            lineStyle: { color, opacity, curveness: 0.5 },
          }
        }),
      },
    ],
  }
}

export function DispatchHealthPanel() {
  const [rangeValue, setRangeValue] = useState<GatewayDispatchWindow>("1h")
  const [drillGateway, setDrillGateway] = useState<number | null>(null)
  // null = 不高亮；否则是选中的顺延次数——对应后端按整条链路径分好组的 highlights
  const [highlightFailovers, setHighlightFailovers] = useState<number | null>(null)
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

  // 顺延次数按钮：直接用后端算好的分组（每组已经是「顺延了这么多次的链，从入口到
  // 结局完整路径」），前端不用再猜——下钻到具体网关时才有（全部网关视图里恒为空）
  const highlightGroups = flowData?.highlights ?? []

  // 换了网关/时间窗口后，之前选的那个次数可能已经不存在了，清掉免得按钮选中态
  // 和实际高亮对不上
  useEffect(() => {
    if (highlightFailovers != null && !highlightGroups.some((group) => group.failovers === highlightFailovers)) {
      setHighlightFailovers(null)
    }
  }, [highlightGroups, highlightFailovers])

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
  const activeHighlight = highlightFailovers != null
    ? highlightGroups.find((group) => group.failovers === highlightFailovers) ?? null
    : null
  // 记录上一次真正建过图的 flowData，用来分辨"数据变了"和"只是切了高亮"——
  // 后者不该重放入场动画，那看着就跟重新生成了一张图一样
  const lastFlowDataRef = useRef<GatewayDispatchFlow | null>(null)

  useEffect(() => {
    if (!chartRef.current) return
    // requests===0 时 nodes 里可能还剩一个孤零零的网关/根节点（没有 links）——同一个判断
    // 口径要跟下面渲染空态用的条件对齐，否则这里会去 setOption 一张没有连线的图，
    // 虽然反正盖在浮层底下看不见，但没必要让 ECharts 空跑
    if (!flowData || flowData.requests === 0) {
      chart.current?.dispose()
      chart.current = null
      lastFlowDataRef.current = null
      return
    }
    if (!chart.current) chart.current = echarts.init(chartRef.current)
    const instance = chart.current
    // 高亮成员（node_ids/link_keys）是后端按整条链路径算好的，这里只负责按成员
    // 身份把 itemStyle/lineStyle 的不透明度分两档——换句话说，切高亮时这次
    // setOption 用的还是同一份 flowData，节点数量、连线、value 全都没变，
    // ECharts 算出来的坐标也就跟上一次分毫不差，只是颜色深浅不同，肉眼看不出
    // "图被重新画了一张"。真正数据变化（换网关/时间窗口）才会经过下面的动画。
    const dataChanged = lastFlowDataRef.current !== flowData
    lastFlowDataRef.current = flowData
    // 先 resize 再 setOption：容器高度会随节点数变，不 resize 的话画布还是旧尺寸，
    // 内容会被压扁并溢出到下面的区块上；而且标签的截断宽度是按画布实际宽度算的，
    // 必须拿到 resize 之后的尺寸才对得上
    const draw = (animate: boolean) => {
      instance.resize()
      instance.setOption(flowOption(flowData, height, instance.getWidth(), activeHighlight, animate), true)
    }
    draw(dataChanged)
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
    // 窗口宽度变了列间距也变了，标签的截断宽度得跟着重算，所以整张重画（不带动画）
    const resize = () => draw(false)
    window.addEventListener("resize", resize)
    return () => window.removeEventListener("resize", resize)
  }, [flowData, height, handleNodeClick, activeHighlight])

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
      {/* 顺延次数高亮：点一下把「顺延了这么多次」的链完整路径（入口到结局，不是
          某一跳的片段）点亮、其余调暗，不重新拉数据、不重新铺图。可选项就是这批
          数据里实际出现过的顺延次数，不是写死的列表；只有下钻到具体网关时才有
          意义（全部网关视图里节点没有"顺延几次"这个概念） */}
      {drillGateway != null && highlightGroups.length > 0 ? (
        <div className="mb-1.5 flex flex-wrap items-center gap-1.5">
          <span className="text-[10px] text-muted-foreground">顺延次数</span>
          <div className="inline-flex flex-wrap rounded-md border border-border bg-muted/30 p-0.5">
            <button type="button" onClick={() => setHighlightFailovers(null)}
              className={cn("h-6 rounded px-1.5 text-[11px]", highlightFailovers == null ? "bg-background font-semibold shadow-sm" : "text-muted-foreground")}>
              全部
            </button>
            {highlightGroups.map((group) => (
              <button key={group.failovers} type="button"
                onClick={() => setHighlightFailovers((current) => current === group.failovers ? null : group.failovers)}
                title={`高亮这 ${group.requests} 条顺延了 ${group.failovers} 次的请求链（入口到结局的完整路径）。注意线宽始终是该段的总流量，高亮只改深浅`}
                className={cn("h-6 rounded px-1.5 text-[11px] tabular-nums", highlightFailovers === group.failovers ? "bg-background font-semibold shadow-sm" : "text-muted-foreground")}>
                {group.failovers} 次
                <span className="ml-1 opacity-55">{group.requests}</span>
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
                {/* position:relative + overflow:hidden 是给上面 appendToBody:false 的
                    tooltip 用的：tooltip 会挂在这层容器内部，被这层裁切，无论算出
                    什么坐标都跳不出这张卡片 */}
                <div ref={chartRef} style={{ height, position: "relative", overflow: "hidden" }} className="w-full" />
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
