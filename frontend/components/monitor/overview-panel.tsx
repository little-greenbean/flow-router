"use client"

import { Activity } from "lucide-react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { BalanceOverview } from "@/components/monitor/balance-overview"
import { MultiplierChanges } from "@/components/monitor/multiplier-changes"
import { useDashboardSummary, useGatewayUsageStatsToday, useRateChanges } from "@/lib/queries"
import { formatTokens, money } from "@/lib/format"
import { cn } from "@/lib/utils"
import type { ReactNode } from "react"

/**
 * OverviewPanel 把原来独立的三块（KpiRow 六张大卡 + 余额概览 + 最近倍率变动）
 * 合并成一个栏目。KPI 去掉了大图标方块，改成紧凑数值瓦片，纵向占用从约 610px
 * 压到约 250px；趋势图和倍率变动沿用原组件的 bare 模式，逻辑不复制一份。
 */

function countTodayChanges(changes: { changed_at: string }[]) {
  const startOfDay = new Date()
  startOfDay.setHours(0, 0, 0, 0)
  return changes.filter((change) => new Date(change.changed_at) >= startOfDay).length
}

function Tile({ label, value, hint, tone }: { label: string; value: ReactNode; hint: ReactNode; tone?: string }) {
  return (
    <div className="min-w-0 rounded-md border border-border/70 bg-muted/20 px-2 py-1.5">
      <div className="truncate text-[10px] text-muted-foreground">{label}</div>
      <div className={cn("truncate text-base font-semibold tabular-nums leading-tight", tone)}>{value}</div>
      <div className="truncate text-[10px] text-muted-foreground">{hint}</div>
    </div>
  )
}

export function OverviewPanel() {
  const summary = useDashboardSummary()
  const recentChanges = useRateChanges(1, 100)
  const gatewayToday = useGatewayUsageStatsToday()

  const data = summary.data
  const total = data?.total_channels ?? 0
  const active = data?.active_channels ?? 0
  const failed = data?.failed_channels ?? 0
  const lowest = data?.lowest_balance ?? null
  const todayTokens = gatewayToday.data?.total_tokens ?? 0
  const todayInput = gatewayToday.data?.total_input_tokens ?? 0
  const todayOutput = gatewayToday.data?.total_output_tokens ?? 0
  const todayChangeCount = countTodayChanges(recentChanges.data?.items ?? [])

  return (
    <Card className="overflow-hidden border border-border py-2 shadow-none sm:py-3">
      <CardHeader className="gap-2 px-3 pb-2 sm:flex sm:flex-row sm:items-center sm:justify-between sm:px-4">
        <CardTitle className="flex items-center gap-1.5 text-sm font-semibold">
          <Activity className="size-3.5 text-brand" />
          {"总览"}
        </CardTitle>
        <span className="text-[10px] text-muted-foreground">{"余额与消费为最近 7 天"}</span>
      </CardHeader>
      <CardContent className="px-3 pb-2 sm:px-4">
        <div className="grid gap-4 lg:grid-cols-12">
          <section className="min-w-0 lg:col-span-4">
            <div className="mb-1.5 flex h-6 items-center justify-between">
              <h3 className="text-xs font-semibold">{"关键指标"}</h3>
              <span className="text-[10px] text-muted-foreground">{`${total} 个渠道`}</span>
            </div>
            <div className="grid h-52 grid-cols-2 grid-rows-3 gap-1.5">
              <Tile
                label="总余额"
                value={money(data?.total_balance ?? 0)}
                hint={lowest ? <>{"最低 "}<span className="text-warning">{money(lowest.balance)}</span>{` · ${lowest.name}`}</> : "—"}
              />
              <Tile
                label="今日总消费"
                value={money(data?.today_total_cost ?? 0)}
                hint={(data?.today_total_cost ?? 0) > 0 ? "按实际扣费统计" : "今日暂无消费"}
              />
              <Tile
                label="累计消费"
                value={money(data?.total_cost ?? 0)}
                hint={(data?.total_cost ?? 0) > 0 ? "全渠道累计实际扣费" : "暂无累计消费"}
              />
              <Tile
                label="今日 Token"
                value={formatTokens(todayTokens)}
                hint={todayTokens > 0 ? `入 ${formatTokens(todayInput)} / 出 ${formatTokens(todayOutput)}` : "网关今日暂无调用"}
              />
              <Tile
                label="渠道状态"
                value={<>{active}<span className="text-xs font-normal text-muted-foreground">{` / ${total}`}</span></>}
                hint={<><span className="text-success">{`${active} 健康`}</span>{failed > 0 ? <>{" · "}<span className="text-danger">{`${failed} 失败`}</span></> : null}</>}
              />
              <Tile
                label="今日倍率变动"
                value={todayChangeCount}
                tone={todayChangeCount > 0 ? "text-danger" : undefined}
                hint={todayChangeCount > 0 ? `检测到 ${todayChangeCount} 次变动` : "今日无变动"}
              />
            </div>
          </section>

          <section className="min-w-0 border-t border-border pt-3 lg:col-span-5 lg:border-l lg:border-t-0 lg:pl-4 lg:pt-0">
            <div className="mb-1.5 flex h-6 items-center justify-between">
              <h3 className="text-xs font-semibold">{"余额与消费趋势"}</h3>
              <span className="text-[10px] text-muted-foreground">{"最近 7 天"}</span>
            </div>
            <div className="h-52">
              <BalanceOverview bare />
            </div>
          </section>

          <section className="min-w-0 border-t border-border pt-3 lg:col-span-3 lg:border-l lg:border-t-0 lg:pl-4 lg:pt-0">
            <div className="h-[15.5rem]">
              <MultiplierChanges bare />
            </div>
          </section>
        </div>
      </CardContent>
    </Card>
  )
}
