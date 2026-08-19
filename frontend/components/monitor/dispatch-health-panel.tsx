"use client"

import { useMemo, useState } from "react"
import { Activity, Route, Timer } from "lucide-react"
import { useNavigate } from "react-router-dom"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { useGatewayDispatchStats } from "@/lib/queries"
import type { GatewayDispatchWindow } from "@/lib/api-types"
import { cn } from "@/lib/utils"
import {
  DISPATCH_WINDOW_OPTIONS,
  chunkDispatchGroups,
  failureRateTone,
  formatDispatchRouteMetric,
  formatDispatchRouteSource,
  formatDispatchRouteGroup,
  formatBillingRate,
  dispatchRoutePath,
  formatFailureRate,
  formatFirstToken,
  metricBarPercent,
  isDispatchRouteNavigable,
} from "@/components/monitor/dispatch-health-utils"

const failureTextClass = {
  success: "text-success",
  warning: "text-warning",
  danger: "text-danger",
}

const failureBarClass = {
  success: "bg-success",
  warning: "bg-warning",
  danger: "bg-danger",
}

export function DispatchHealthPanel() {
  const navigate = useNavigate()
  const [window, setWindow] = useState<GatewayDispatchWindow>("5m")
  const stats = useGatewayDispatchStats(window)
  const groups = stats.data?.groups ?? []
  const routeCount = useMemo(
    () => groups.reduce((sum, group) => sum + group.routes.length, 0),
    [groups],
  )

  return (
    <Card className="overflow-hidden border border-border py-2 shadow-none sm:py-3">
      <CardHeader className="gap-1.5 px-3 pb-1.5 sm:flex sm:flex-row sm:items-center sm:justify-between sm:px-4">
        <div className="min-w-0">
          <CardTitle className="flex items-center gap-1.5 text-sm font-semibold">
            <Activity className="size-3.5 text-brand" />
            调度情况
          </CardTitle>
          <p className="mt-0.5 text-[11px] text-muted-foreground">
            {groups.length > 0
              ? `${groups.length} 个网关组 · ${routeCount} 条统计路由`
              : "按路由尝试统计失败率与平均首字时间"}
          </p>
        </div>

        <div className="max-w-full overflow-x-auto pb-1 sm:pb-0" aria-label="统计时间窗口">
          <div className="inline-flex min-w-max items-center rounded-md border border-border bg-muted/30 p-0.5" role="radiogroup">
            {DISPATCH_WINDOW_OPTIONS.map((option) => {
              const active = option.value === window
              return (
                <button
                  key={option.value}
                  type="button"
                  role="radio"
                  aria-checked={active}
                  onClick={() => setWindow(option.value)}
                  className={cn(
                    "h-6 rounded px-2 text-[11px] font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                    active
                      ? "bg-background text-foreground shadow-sm ring-1 ring-border"
                      : "text-muted-foreground hover:bg-background/60 hover:text-foreground",
                  )}
                >
                  {option.label}
                </button>
              )
            })}
          </div>
        </div>
      </CardHeader>

      <CardContent className="px-0 pb-0">
        {stats.loading && !stats.data ? (
          <div className="flex h-32 items-center justify-center text-xs text-muted-foreground">
            正在加载调度数据...
          </div>
        ) : stats.error ? (
          <div className="mx-4 rounded-md border border-danger/25 bg-danger/5 px-3 py-4 text-sm text-danger sm:mx-6">
            调度数据加载失败：{stats.error}
          </div>
        ) : groups.length === 0 ? (
          <div className="flex h-32 flex-col items-center justify-center gap-2 text-muted-foreground">
            <Route className="size-5" />
            <p className="text-sm">当前时间窗口暂无调度记录</p>
          </div>
        ) : (
          <div className="space-y-2 border-t border-border px-2.5 py-2 sm:px-3">
            {chunkDispatchGroups(groups).map((groupRow, rowIndex) => (
              <div key={`dispatch-group-row-${rowIndex}`} className="grid min-w-0 gap-2 md:grid-cols-2 xl:grid-cols-3">
                {groupRow.map((group) => {
                  const groupAttempts = group.routes.reduce((sum, route) => sum + route.total_attempts, 0)
                  return (
                    <section
                      key={group.gateway_group_id}
                      aria-labelledby={`dispatch-group-${group.gateway_group_id}`}
                      className="min-w-0 rounded-md border border-border bg-background"
                    >
                    <div className="flex items-center justify-between gap-2 border-b border-border bg-muted/20 px-2.5 py-1.5">
                      <div className="flex min-w-0 items-center gap-2">
                        <span className="size-1.5 shrink-0 rounded-full bg-brand" />
                        <h3 id={`dispatch-group-${group.gateway_group_id}`} className="truncate text-xs font-semibold text-foreground">
                          {group.gateway_group_name}
                        </h3>
                      </div>
                      <span className="shrink-0 text-[10px] tabular-nums text-muted-foreground">{group.routes.length} 条路由</span>
                    </div>

                    <div className="divide-y divide-border">
                      {group.routes.map((route) => {
                        const tone = failureRateTone(route.failure_rate)
                        const routeNavigable = isDispatchRouteNavigable(route)
                        return (
                          <div
                            key={route.route_id}
                            className="grid min-w-0 grid-cols-1 items-center gap-2 px-2.5 py-1.5 md:grid-cols-[minmax(110px,1fr)_minmax(165px,1.1fr)]"
                            title={formatDispatchRouteMetric(route)}
                          >
                            <div className="flex min-w-0 items-center gap-2">
                              <div className="flex size-5 shrink-0 items-center justify-center rounded bg-muted text-muted-foreground">
                                <Route className="size-2.5" />
                              </div>
                              {routeNavigable ? (
                                <button
                                  type="button"
                                  className="min-w-0 text-left outline-none focus-visible:rounded-sm focus-visible:ring-2 focus-visible:ring-ring/60"
                                  onClick={() => navigate(dispatchRoutePath(group.gateway_group_id, route.route_id))}
                                  title={`打开 ${formatDispatchRouteSource(route)} · 源分组 ${formatDispatchRouteGroup(route)}`}
                                >
                                  <p className="whitespace-normal break-words text-[11px] font-medium leading-4 text-foreground hover:text-brand">
                                    {formatDispatchRouteSource(route)}
                                  </p>
                                  <p className="whitespace-normal break-words text-[10px] leading-3 text-muted-foreground">
                                    源分组 {formatDispatchRouteGroup(route)} · 成本 {formatBillingRate(route.billing_rate_multiplier)}
                                  </p>
                                </button>
                              ) : (
                                <div className="min-w-0 text-left" title="历史路由，当前配置已删除">
                                <p className="whitespace-normal break-words text-[11px] font-medium leading-4 text-foreground hover:text-brand">
                                  {formatDispatchRouteSource(route)}
                                </p>
                                <p className="whitespace-normal break-words text-[10px] leading-3 text-muted-foreground">
                                  历史路由 · {formatDispatchRouteGroup(route)} · 成本 {formatBillingRate(route.billing_rate_multiplier)}
                                </p>
                                </div>
                              )}
                            </div>
                            <div className="min-w-0 space-y-1">
                              <div
                                className="flex min-w-0 items-center gap-1.5"
                                title={`调用 ${route.total_attempts} 次，占组内 ${metricBarPercent(route.total_attempts, groupAttempts)}%`}
                              >
                                <span className="shrink-0 text-[9px] text-muted-foreground">调用</span>
                                <div className="h-1 min-w-0 flex-1 overflow-hidden rounded-full bg-muted">
                                  <span
                                    className="block h-full rounded-full bg-brand transition-[width]"
                                    style={{ width: `${metricBarPercent(route.total_attempts, groupAttempts)}%` }}
                                  />
                                </div>
                                <span className="shrink-0 text-[10px] font-semibold tabular-nums text-foreground">
                                  {metricBarPercent(route.total_attempts, groupAttempts)}%
                                </span>
                              </div>
                              <div className="flex min-w-0 items-center gap-1.5">
                                <span className="shrink-0 text-[9px] text-muted-foreground">失败</span>
                                <div className="h-1 min-w-0 flex-1 overflow-hidden rounded-full bg-muted">
                                  <span
                                    className={cn("block h-full rounded-full transition-[width]", failureBarClass[tone])}
                                    style={{ width: `${metricBarPercent(route.failure_rate)}%` }}
                                  />
                                </div>
                                <span className={cn("shrink-0 text-[10px] font-semibold tabular-nums", failureTextClass[tone])}>
                                  {formatFailureRate(route.failure_rate)}
                                </span>
                              </div>
                              <div className="flex min-w-0 items-center gap-1.5">
                                <span className="inline-flex shrink-0 items-center gap-0.5 text-[9px] text-muted-foreground">
                                  <Timer className="size-2.5" />
                                  首字
                                </span>
                                <div className="h-1 min-w-0 flex-1 overflow-hidden rounded-full bg-muted">
                                  <span
                                    className="block h-full rounded-full bg-brand/70 transition-[width]"
                                    style={{ width: `${metricBarPercent(route.average_first_token_ms, 1600)}%` }}
                                  />
                                </div>
                                <span className="shrink-0 text-[10px] font-semibold tabular-nums text-foreground">
                                  {formatFirstToken(route.average_first_token_ms)}
                                </span>
                              </div>
                            </div>
                          </div>
                        )
                      })}
                    </div>
                    </section>
                  )
                })}
              </div>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  )
}
