"use client"

import { useEffect, useMemo, useState } from "react"
import { ArrowDownRight, ArrowUpRight } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { ScrollArea } from "@/components/ui/scroll-area"
import { apiFetch } from "@/lib/api"
import type { RateChangeLog, RateChangeLogPage } from "@/lib/api-types"
import { channelTypeLabel, formatRatio, ratioDelta, relativeTime, shortTime } from "@/lib/format"
import { useChannels, useDashboardSummary } from "@/lib/queries"
import { cn } from "@/lib/utils"

const DIALOG_SIZE = 20

export function MultiplierChanges({ bare = false, title }: { bare?: boolean; title?: string } = {}) {
  const summary = useDashboardSummary()
  const channels = useChannels()
  const [detailOpen, setDetailOpen] = useState(false)
  const [page, setPage] = useState(1)
  const [feed, setFeed] = useState<RateChangeLog[]>([])
  const [feedMeta, setFeedMeta] = useState<{ total: number; pages: number }>({
    total: 0,
    pages: 1,
  })
  const [feedLoading, setFeedLoading] = useState(false)
  const [feedError, setFeedError] = useState<string | null>(null)

  const channelMap = useMemo(() => {
    const m = new Map<number, { name: string; type: string }>()
    for (const c of channels.data ?? []) m.set(c.id, { name: c.name, type: c.type })
    return m
  }, [channels.data])

  const items = summary.data?.recent_rate_changes ?? []

  function loadNextPage() {
    if (feedLoading || page >= feedMeta.pages) return
    setPage((prev) => prev + 1)
  }

  useEffect(() => {
    if (!detailOpen) return
    let cancelled = false
    setFeedLoading(true)
    setFeedError(null)
    apiFetch<RateChangeLogPage>(`/rate-changes?page=${page}&page_size=${DIALOG_SIZE}`)
      .then((res) => {
        if (cancelled) return
        const next = Array.isArray(res?.items) ? res.items : []
        setFeed((prev) => (page === 1 ? next : [...prev, ...next]))
        setFeedMeta({
          total: res?.total ?? 0,
          pages: Math.max(1, res?.pages ?? 1),
        })
      })
      .catch((e) => {
        if (cancelled) return
        const err = e as Error
        setFeedError(err.message || "加载倍率变动记录失败")
      })
      .finally(() => {
        if (!cancelled) setFeedLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [detailOpen, page])

  function openDetail() {
    setFeed([])
    setFeedMeta({ total: items.length, pages: 1 })
    setFeedError(null)
    setPage(1)
    setDetailOpen(true)
  }

  const list = summary.loading ? (
    <p className={cn("text-xs text-muted-foreground", bare ? "px-1 py-3" : "px-6 py-6")}>{"加载中…"}</p>
  ) : items.length === 0 ? (
    <p className={cn("text-xs text-muted-foreground", bare ? "px-1 py-3" : "px-6 py-6")}>{"暂无倍率变动记录"}</p>
  ) : (
    <ScrollArea type="hover" className={bare ? "h-full" : "max-h-80 lg:h-full lg:max-h-none"}>
      <ul className="divide-y divide-border">
        {items.map((item) => (
          <MultiplierChangeRow key={item.id} item={item} channelMap={channelMap} compact={bare} />
        ))}
      </ul>
    </ScrollArea>
  )

  return (
    <>
      {bare ? (
        <div className="flex h-full min-h-0 flex-col">
          <div className="mb-1.5 flex h-6 shrink-0 items-center justify-between">
            <h3 className="text-xs font-semibold">{title ?? "最近倍率变动"}</h3>
            <div className="flex items-center gap-1.5">
              <span className="text-[10px] text-muted-foreground">{items.length > 0 ? `${items.length} 条` : ""}</span>
              <Button size="sm" variant="ghost" className="h-6 px-1.5 text-[10px]" onClick={openDetail}>{"查看更多"}</Button>
            </div>
          </div>
          <div className="min-h-0 flex-1 overflow-hidden rounded-md border border-border/70 bg-muted/20">{list}</div>
        </div>
      ) : (
      <Card className="min-h-0 overflow-hidden border border-border py-4 shadow-none lg:h-100 sm:py-6">
        <CardHeader className="flex shrink-0 flex-row items-center justify-between px-4 pb-2 sm:px-6">
          <CardTitle className="text-base font-semibold">{"最近倍率变动"}</CardTitle>
          <div className="flex items-center gap-3">
            <span className="text-xs text-muted-foreground">{items.length > 0 ? `${items.length} 条` : ""}</span>
            <Button size="sm" variant="ghost" className="h-7 px-2 text-xs" onClick={openDetail}>
              {"查看更多"}
            </Button>
          </div>
        </CardHeader>
        <CardContent className="min-h-0 flex-1 px-0">{list}</CardContent>
      </Card>
      )}

      <Dialog open={detailOpen} onOpenChange={setDetailOpen}>
        <DialogContent className="max-h-[85vh] overflow-hidden sm:max-w-3xl">
          <DialogHeader>
            <DialogTitle>{"最近倍率变动"}</DialogTitle>
          </DialogHeader>
          <div
            className="max-h-[60vh] overflow-y-auto rounded-md border border-border"
            onScroll={(e) => {
              const target = e.currentTarget
              if (target.scrollTop + target.clientHeight >= target.scrollHeight - 32) {
                loadNextPage()
              }
            }}
          >
            <ul className="divide-y divide-border">
              {feed.map((item) => (
                <MultiplierChangeRow key={item.id} item={item} channelMap={channelMap} compact />
              ))}
              {feedLoading && feed.length === 0 ? (
                <li className="px-4 py-6 text-sm text-muted-foreground">{"加载中…"}</li>
              ) : null}
              {!feedLoading && feed.length === 0 ? (
                <li className="px-4 py-6 text-sm text-muted-foreground">{"暂无倍率变动记录"}</li>
              ) : null}
              {feedLoading && feed.length > 0 ? (
                <li className="px-4 py-3 text-xs text-muted-foreground">{"加载更多中…"}</li>
              ) : null}
            </ul>
          </div>
          {feedError ? <p className="text-sm text-danger">{feedError}</p> : null}
          <div className="flex items-center justify-between">
            <span className="text-xs text-muted-foreground">
              {feedMeta.total > 0 ? `已加载 ${feed.length} / ${feedMeta.total} 条` : ""}
            </span>
            <Button
              variant="outline"
              size="sm"
              disabled={feedLoading || page >= feedMeta.pages}
              onClick={loadNextPage}
            >
              {feedLoading && page > 1 ? "加载中…" : "查看更多"}
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </>
  )
}

function MultiplierChangeRow({
  item,
  channelMap,
  compact = false,
}: {
  item: RateChangeLog
  channelMap: Map<number, { name: string; type: string }>
  compact?: boolean
}) {
  const ch = channelMap.get(item.channel_id)
  const delta = ratioDelta(item.old_ratio, item.new_ratio)
  const isUp = delta.direction === "up"
  const chType = ch?.type ?? ""

  return (
    <li className={cn("flex items-start gap-2.5 sm:gap-3", compact ? "px-4 py-3" : "px-4 py-3.5 sm:px-6")}>
      <div className="flex flex-col items-center gap-0.5 pt-1">
        <span className={cn("size-2 rounded-full", isUp ? "bg-danger" : "bg-success")} />
      </div>
      <div className="shrink-0 text-[11px] leading-relaxed text-muted-foreground sm:text-xs">
        <p>{relativeTime(item.changed_at)}</p>
        <p>{shortTime(item.changed_at)}</p>
      </div>

      <div className="min-w-0 flex-1">
        <div className="flex min-w-0 flex-wrap items-center gap-2">
          <span className="truncate text-sm font-semibold text-foreground">{item.model_name}</span>
          <span
            className={cn(
              "inline-flex items-center rounded-md px-1.5 py-0.5 text-[10px] font-medium ring-1 ring-inset",
              chType === "newapi"
                ? "bg-brand/10 text-brand ring-brand/20"
                : "bg-foreground/5 text-foreground ring-border",
            )}
          >
            {ch?.name ?? `#${item.channel_id}`}
            {chType ? <span className="ml-1 opacity-60">{channelTypeLabel(chType)}</span> : null}
          </span>
        </div>
        <div className="mt-1.5 flex items-center text-xs">
          <div>
            <span className="text-muted-foreground">{"倍率"}</span>
            <p className="mt-0.5 tabular-nums">
              <span className="text-muted-foreground">
                {formatRatio(item.old_ratio)}
              </span>
              <span className="mx-1 text-muted-foreground">{"→"}</span>
              <span className={cn("font-medium", isUp ? "text-danger" : "text-success")}>
                {formatRatio(item.new_ratio)}
              </span>
            </p>
          </div>
        </div>
      </div>

      <div className="shrink-0 pt-0.5">
        <span
          className={cn(
            "inline-flex items-center gap-0.5 rounded-md px-2 py-1 text-xs font-semibold",
            isUp ? "bg-danger/10 text-danger" : "bg-success/10 text-success",
          )}
        >
          {isUp ? <ArrowUpRight className="size-3" /> : <ArrowDownRight className="size-3" />}
          {delta.pct}
        </span>
      </div>
    </li>
  )
}
