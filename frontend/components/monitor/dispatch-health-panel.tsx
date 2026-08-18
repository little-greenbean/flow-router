"use client"

import { useMemo, useState } from "react"
import { Activity, Route, Timer } from "lucide-react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { useGatewayDispatchStats } from "@/lib/queries"
import type { GatewayDispatchWindow } from "@/lib/api-types"
import { cn } from "@/lib/utils"
import {
  DISPATCH_WINDOW_OPTIONS,
  failureRateTone,
  formatFailureRate,
  formatFirstToken,
} from "@/components/monitor/dispatch-health-utils"

const failureToneClass = {
  success: "bg-success/10 text-success ring-success/20",
  warning: "bg-warning/10 text-warning ring-warning/20",
  danger: "bg-danger/10 text-danger ring-danger/20",
}

export function DispatchHealthPanel() {
  const [window, setWindow] = useState<GatewayDispatchWindow>("5m")
  const stats = useGatewayDispatchStats(window)
  const groups = stats.data?.groups ?? []
  const summary = useMemo(() => {
    let routes = 0
    let attempts = 0
    for (const group of groups) {
      routes += group.routes.length
      attempts += group.routes.reduce((sum, route) => sum + route.total_attempts, 0)
    }
    return { routes, attempts }
  }, [groups])

  return (
    <Card className="overflow-hidden border border-border py-4 shadow-none sm:py-6">
      <CardHeader className="gap-3 px-4 pb-3 sm:flex sm:flex-row sm:items-center sm:justify-between sm:px-6">
        <div className="min-w-0">
          <CardTitle className="flex items-center gap-2 text-base font-semibold">
            <Activity className="size-4 text-brand" />
            调度情况
          </CardTitle>
          <p className="mt-1 text-xs text-muted-foreground">
            {groups.length > 0
              ? `${groups.length} 个网关组 · ${summary.routes} 条活跃路由 · ${summary.attempts} 次尝试`
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
                    "h-7 rounded px-2.5 text-xs font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
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
          <div className="divide-y divide-border border-t border-border">
            {groups.map((group) => {
              const attempts = group.routes.reduce((sum, route) => sum + route.total_attempts, 0)
              return (
                <section key={group.gateway_group_id} aria-labelledby={`dispatch-group-${group.gateway_group_id}`}>
                  <div className="flex items-center justify-between gap-3 bg-muted/20 px-4 py-2.5 sm:px-6">
                    <div className="flex min-w-0 items-center gap-2">
                      <span className="size-2 shrink-0 rounded-full bg-brand" />
                      <h3 id={`dispatch-group-${group.gateway_group_id}`} className="truncate text-sm font-semibold text-foreground">
                        {group.gateway_group_name}
                      </h3>
                    </div>
                    <span className="shrink-0 text-xs tabular-nums text-muted-foreground">
                      {group.routes.length} 条路由 · {attempts} 次尝试
                    </span>
                  </div>

                  <div className="overflow-x-auto">
                    <table className="w-full min-w-160 table-fixed text-left">
                      <thead>
                        <tr className="border-t border-border/70 text-[11px] text-muted-foreground">
                          <th className="w-[46%] px-4 py-2 font-medium sm:px-6">路由</th>
                          <th className="w-[16%] px-3 py-2 text-right font-medium">尝试次数</th>
                          <th className="w-[18%] px-3 py-2 text-right font-medium">失败率</th>
                          <th className="w-[20%] px-4 py-2 text-right font-medium sm:px-6">平均首字时间</th>
                        </tr>
                      </thead>
                      <tbody className="divide-y divide-border/70">
                        {group.routes.map((route) => {
                          const tone = failureRateTone(route.failure_rate)
                          return (
                            <tr key={route.route_id} className="hover:bg-muted/15">
                              <td className="px-4 py-3 sm:px-6">
                                <div className="flex min-w-0 items-center gap-2.5">
                                  <div className="flex size-7 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground">
                                    <Route className="size-3.5" />
                                  </div>
                                  <div className="min-w-0">
                                    <p className="truncate text-sm font-medium text-foreground">{route.route_name}</p>
                                    <p className="truncate text-xs text-muted-foreground">
                                      {route.provider_name || "未记录上游名称"}
                                    </p>
                                  </div>
                                </div>
                              </td>
                              <td className="px-3 py-3 text-right text-sm tabular-nums text-foreground">
                                {route.total_attempts}
                              </td>
                              <td className="px-3 py-3 text-right">
                                <span
                                  className={cn(
                                    "inline-flex min-w-16 justify-center rounded px-2 py-1 text-xs font-semibold tabular-nums ring-1 ring-inset",
                                    failureToneClass[tone],
                                  )}
                                  title={`${route.failed_attempts} / ${route.total_attempts} 次失败`}
                                >
                                  {formatFailureRate(route.failure_rate)}
                                </span>
                              </td>
                              <td className="px-4 py-3 text-right sm:px-6">
                                <div className="inline-flex items-center justify-end gap-1.5 text-sm tabular-nums text-foreground">
                                  <Timer className="size-3.5 text-muted-foreground" />
                                  {formatFirstToken(route.average_first_token_ms)}
                                </div>
                                <p className="mt-0.5 text-[10px] tabular-nums text-muted-foreground">
                                  {route.first_token_samples > 0 ? `${route.first_token_samples} 个样本` : "无首字样本"}
                                </p>
                              </td>
                            </tr>
                          )
                        })}
                      </tbody>
                    </table>
                  </div>
                </section>
              )
            })}
          </div>
        )}
      </CardContent>
    </Card>
  )
}
