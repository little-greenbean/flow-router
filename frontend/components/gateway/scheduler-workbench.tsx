import { useMemo } from "react"
import {
  Activity,
  AlertTriangle,
  ArrowRight,
  BrainCircuit,
  Check,
  CircleOff,
  Gauge,
  Pause,
  Route,
  ShieldCheck,
  Timer,
  X,
} from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Progress } from "@/components/ui/progress"
import type {
  GatewayGroup,
  GatewayProviderOption,
  GatewayRoute,
  RateSnapshot,
} from "@/lib/api-types"
import {
  deriveSchedulerSnapshot,
  formatSchedulerPercent,
  formatSchedulerRate,
  type SchedulerRouteRow,
} from "./scheduler-view-utils"

type SchedulerWorkbenchProps = {
  group: GatewayGroup | null
  routes: Partial<GatewayRoute>[]
  sourceGroupsByChannel: Record<number, RateSnapshot[]>
  providers: GatewayProviderOption[]
}

function stateMeta(state: SchedulerRouteRow["state"]) {
  switch (state) {
    case "primary":
      return {
        label: "主路由",
        className:
          "border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-900/60 dark:bg-emerald-950/40 dark:text-emerald-300",
        icon: Check,
      }
    case "fallback":
      return {
        label: "备用",
        className:
          "border-sky-200 bg-sky-50 text-sky-700 dark:border-sky-900/60 dark:bg-sky-950/40 dark:text-sky-300",
        icon: ArrowRight,
      }
    case "paused":
      return {
        label: "暂停",
        className:
          "border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-900/60 dark:bg-amber-950/40 dark:text-amber-300",
        icon: Pause,
      }
    case "disabled":
      return {
        label: "禁用",
        className:
          "border-border bg-muted/60 text-muted-foreground",
        icon: CircleOff,
      }
    default:
      return {
        label: "不可调度",
        className:
          "border-red-200 bg-red-50 text-red-700 dark:border-red-900/60 dark:bg-red-950/40 dark:text-red-300",
        icon: X,
      }
  }
}

function RouteChainItem({ row }: { row: SchedulerRouteRow }) {
  const meta = stateMeta(row.state)
  const StateIcon = meta.icon
  return (
    <div className="min-w-0 flex-1 rounded-xl border border-border bg-background p-3 shadow-xs">
      <div className="flex items-start justify-between gap-2">
        <div className="flex min-w-0 items-center gap-2">
          <div className="flex size-7 shrink-0 items-center justify-center rounded-lg bg-muted text-xs font-semibold text-foreground">
            {row.state === "primary" ? "1" : "→"}
          </div>
          <div className="min-w-0">
            <p className="truncate text-sm font-medium text-foreground">{row.sourceLabel}</p>
            <p className="truncate text-[11px] text-muted-foreground">
              路由 #{row.route.id ?? "—"} · {row.protocolLabel}
            </p>
          </div>
        </div>
        <Badge variant="outline" className={`shrink-0 gap-1 text-[10px] ${meta.className}`}>
          <StateIcon className="size-3" />
          {meta.label}
        </Badge>
      </div>
      <div className="mt-3 grid grid-cols-3 gap-2 text-[11px]">
        <div>
          <p className="text-muted-foreground">有效倍率</p>
          <p className="mt-0.5 font-mono font-medium">{formatSchedulerRate(row.rate)}</p>
        </div>
        <div>
          <p className="text-muted-foreground">权重</p>
          <p className="mt-0.5 font-mono font-medium">{row.weight}</p>
        </div>
        <div>
          <p className="text-muted-foreground">状态</p>
          <p className="mt-0.5 truncate font-medium">{row.reason}</p>
        </div>
      </div>
    </div>
  )
}

export function SchedulerWorkbench({
  group,
  routes,
  sourceGroupsByChannel,
  providers,
}: SchedulerWorkbenchProps) {
  const snapshot = useMemo(
    () => deriveSchedulerSnapshot(group, routes, sourceGroupsByChannel, providers),
    [group, providers, routes, sourceGroupsByChannel],
  )

  const eligibleCount = snapshot.eligible.length
  const policy = [
    group?.retry_enabled === false ? "失败不重试" : `同路由重试 ${group?.retry_count ?? 0} 次`,
    group?.failover_enabled === false
      ? "不顺延"
      : `最多顺延 ${group?.failover_max ?? 0} 次`,
    group?.failover_on_4xx ? "4xx 可顺延" : "4xx 默认直返",
    `冷却 ${group?.cooldown_seconds ?? 0}s`,
  ]

  return (
    <div className="space-y-4">
      <Card className="overflow-hidden border-border shadow-none">
        <CardHeader className="border-b border-border bg-muted/20 px-4 py-4 sm:px-5">
          <div className="flex flex-wrap items-start justify-between gap-3">
            <div className="flex min-w-0 items-start gap-3">
              <div className="flex size-9 shrink-0 items-center justify-center rounded-xl bg-primary text-primary-foreground">
                <Gauge className="size-4" />
              </div>
              <div className="min-w-0">
                <CardTitle className="text-base">组内调度</CardTitle>
                <p className="mt-1 text-xs text-muted-foreground">
                  {group ? `${group.name} · 规则执行视图` : "选择一个网关组查看调度"}
                </p>
              </div>
            </div>
            <Badge variant="outline" className="gap-1.5 border-border bg-background text-xs">
              <Activity className="size-3.5 text-emerald-600" />
              规则模式
            </Badge>
          </div>
        </CardHeader>
        <CardContent className="space-y-4 px-4 py-4 sm:px-5">
          <div className="grid gap-2 sm:grid-cols-5">
            {[
              ["Gateway Key", "鉴权 / 配额"],
              ["Gateway Group", "当前分组"],
              ["Hard filters", "启用 / 密钥 / 暂停"],
              ["Rate sort", group?.rate_sort_direction === "desc" ? "高倍率优先" : "低倍率优先"],
              ["Failover", group?.failover_enabled === false ? "关闭" : "顺延备用"],
            ].map(([title, value], index) => (
              <div key={title} className="relative rounded-lg border border-border bg-background px-3 py-2.5">
                <p className="font-mono text-[10px] uppercase tracking-[0.08em] text-muted-foreground">
                  {title}
                </p>
                <p className="mt-1 text-xs font-medium text-foreground">{value}</p>
                {index < 4 ? (
                  <ArrowRight className="absolute -right-2.5 top-1/2 hidden size-4 -translate-y-1/2 bg-background text-muted-foreground sm:block" />
                ) : null}
              </div>
            ))}
          </div>

          <div className="flex flex-wrap gap-1.5">
            {policy.map((item) => (
              <Badge key={item} variant="secondary" className="rounded-md px-2 py-1 text-[11px] font-normal">
                {item}
              </Badge>
            ))}
            {group?.first_token_timeout_sec ? (
              <Badge variant="outline" className="gap-1 rounded-md px-2 py-1 text-[11px] font-normal">
                <Timer className="size-3" /> 首字 {group.first_token_timeout_sec}s
              </Badge>
            ) : null}
          </div>
        </CardContent>
      </Card>

      <div className="grid gap-3 sm:grid-cols-4">
        {[
          ["可调度", eligibleCount, "emerald"],
          ["临时暂停", snapshot.pausedCount, "amber"],
          ["禁用", snapshot.disabledCount, "muted"],
          ["配置路由", snapshot.rows.length, "sky"],
        ].map(([label, value, tone]) => (
          <div key={label} className="rounded-xl border border-border bg-background px-4 py-3 shadow-xs">
            <p className="text-xs text-muted-foreground">{label}</p>
            <p className={`mt-1 text-xl font-semibold ${tone === "emerald" ? "text-emerald-600 dark:text-emerald-400" : tone === "amber" ? "text-amber-600 dark:text-amber-400" : "text-foreground"}`}>
              {value}
            </p>
          </div>
        ))}
      </div>

      {snapshot.eligible.length > 0 ? (
        <Card className="overflow-hidden border-border shadow-none">
          <CardHeader className="border-b border-border px-4 py-4 sm:px-5">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <div>
                <CardTitle className="flex items-center gap-2 text-sm">
                  <Route className="size-4 text-primary" />
                  当前尝试链
                </CardTitle>
                <p className="mt-1 text-xs text-muted-foreground">
                  首条可调度路由成功时不会主动访问后续路由；只有失败才顺延。
                </p>
              </div>
              <Badge variant="outline" className="text-[10px]">
                {eligibleCount} 条可用
              </Badge>
            </div>
          </CardHeader>
          <CardContent className="space-y-3 px-4 py-4 sm:px-5">
            <div className="flex flex-col gap-2 lg:flex-row lg:items-stretch">
              {snapshot.eligible.map((row, index) => (
                <div key={row.route.id ?? row.index} className="flex min-w-0 flex-1 items-center gap-2">
                  <RouteChainItem row={row} />
                  {index < snapshot.eligible.length - 1 ? (
                    <ArrowRight className="hidden size-4 shrink-0 text-muted-foreground lg:block" />
                  ) : null}
                </div>
              ))}
            </div>
          </CardContent>
        </Card>
      ) : (
        <Card className="border-dashed border-border shadow-none">
          <CardContent className="flex flex-col items-center justify-center gap-2 py-12 text-center">
            <AlertTriangle className="size-5 text-amber-500" />
            <p className="text-sm font-medium">当前没有可调度路由</p>
            <p className="max-w-md text-xs leading-5 text-muted-foreground">
              检查路由是否启用、已绑定上游密钥，或是否仍处于临时暂停窗口。
            </p>
          </CardContent>
        </Card>
      )}

      <Card className="overflow-hidden border-border shadow-none">
        <CardHeader className="border-b border-border px-4 py-4 sm:px-5">
          <div className="flex flex-wrap items-start justify-between gap-2">
            <div>
              <CardTitle className="text-sm">路由规则明细</CardTitle>
              <p className="mt-1 text-xs text-muted-foreground">
                权重占比仅作配置预览；当前运行时仍按倍率、权重、Position 排序。
              </p>
            </div>
            <Badge variant="outline" className="gap-1.5 text-[10px]">
              <ShieldCheck className="size-3" /> 硬规则优先
            </Badge>
          </div>
        </CardHeader>
        <CardContent className="overflow-x-auto px-0 py-0">
          <table className="w-full min-w-[760px] text-left text-xs">
            <thead className="bg-muted/25 text-[10px] uppercase tracking-[0.06em] text-muted-foreground">
              <tr>
                <th className="px-4 py-3 font-medium sm:px-5">顺序</th>
                <th className="px-3 py-3 font-medium">上游</th>
                <th className="px-3 py-3 font-medium">倍率</th>
                <th className="px-3 py-3 font-medium">权重预览</th>
                <th className="px-3 py-3 font-medium">协议</th>
                <th className="px-3 py-3 font-medium">并发配置</th>
                <th className="px-3 py-3 font-medium">状态</th>
                <th className="px-4 py-3 font-medium sm:px-5">原因</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {snapshot.rows.map((row) => {
                const meta = stateMeta(row.state)
                const StateIcon = meta.icon
                const activeIndex = snapshot.eligible.findIndex((item) => item.index === row.index)
                return (
                  <tr key={row.route.id ?? row.index} className="align-middle">
                    <td className="px-4 py-3 font-mono text-muted-foreground sm:px-5">
                      {activeIndex >= 0 ? `#${activeIndex + 1}` : "—"}
                    </td>
                    <td className="max-w-[230px] px-3 py-3">
                      <p className="truncate font-medium text-foreground">{row.sourceLabel}</p>
                      <p className="mt-0.5 truncate text-[11px] text-muted-foreground">
                        route {row.route.id ?? "—"}
                      </p>
                    </td>
                    <td className="px-3 py-3 font-mono">{formatSchedulerRate(row.rate)}</td>
                    <td className="w-[145px] px-3 py-3">
                      <div className="flex items-center gap-2">
                        <Progress value={row.configuredShare * 100} className="h-1.5" />
                        <span className="w-8 shrink-0 text-right font-mono text-[11px]">
                          {row.state === "primary" || row.state === "fallback"
                            ? formatSchedulerPercent(row.configuredShare)
                            : "—"}
                        </span>
                      </div>
                    </td>
                    <td className="px-3 py-3 text-muted-foreground">{row.protocolLabel}</td>
                    <td className="px-3 py-3 font-mono text-muted-foreground">
                      {row.route.concurrency || "—"}
                    </td>
                    <td className="px-3 py-3">
                      <Badge variant="outline" className={`gap-1 text-[10px] ${meta.className}`}>
                        <StateIcon className="size-3" /> {meta.label}
                      </Badge>
                    </td>
                    <td className="max-w-[180px] truncate px-4 py-3 text-muted-foreground sm:px-5">
                      {row.reason}
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </CardContent>
      </Card>

      <div className="grid gap-3 lg:grid-cols-2">
        <Card className="border-border shadow-none">
          <CardContent className="flex items-start gap-3 px-4 py-4 sm:px-5">
            <div className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-muted text-muted-foreground">
              <ShieldCheck className="size-4" />
            </div>
            <div className="min-w-0">
              <p className="text-sm font-medium">规则边界</p>
              <p className="mt-1 text-xs leading-5 text-muted-foreground">
                权限、模型能力、密钥、启用状态、并发和故障转移由网关确定性执行；前端只展示密钥绑定信息，凭据有效性仍由后端校验。
              </p>
            </div>
          </CardContent>
        </Card>
        <Card className="border-dashed border-border bg-muted/15 shadow-none">
          <CardContent className="flex items-start gap-3 px-4 py-4 sm:px-5">
            <div className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
              <BrainCircuit className="size-4" />
            </div>
            <div className="min-w-0">
              <div className="flex flex-wrap items-center gap-2">
                <p className="text-sm font-medium">智能评分层</p>
                <Badge variant="secondary" className="text-[10px]">未接入</Badge>
              </div>
              <p className="mt-1 text-xs leading-5 text-muted-foreground">
                后续 AI 只可基于延迟、错误率、成本和负载调整候选权重，结果需经过上方硬规则过滤后才可生效。
              </p>
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
