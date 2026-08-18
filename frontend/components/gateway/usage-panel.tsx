import { useEffect, useMemo, useState } from "react"
import { Check, ChevronsUpDown, Loader2, RefreshCw, Search } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandSeparator,
} from "@/components/ui/command"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { formatDurationMS, formatTokens, money } from "@/lib/format"
import { cn } from "@/lib/utils"
import type {
  GatewayGroup,
  GatewayKey,
  GatewayUsageModelOption,
  GatewayUsagePage,
  GatewayUsageStats,
} from "@/lib/api-types"
import { GatewayUsageTable } from "./usage-table"

/** 解析密钥筛选值：all / 数字 id / 按名称或前缀匹配组内密钥 */
function resolveUsageKeyFilter(
  raw: string,
  keys: GatewayKey[],
): { keyID: string; label: string } {
  const v = raw.trim()
  if (!v || v === "all") return { keyID: "all", label: "全部密钥" }
  if (/^\d+$/.test(v)) {
    const byID = keys.find((k) => String(k.id) === v)
    if (byID) {
      return {
        keyID: String(byID.id),
        label: byID.key_prefix
          ? `${byID.name} · ${byID.key_prefix}`
          : byID.name,
      }
    }
    return { keyID: v, label: `密钥 #${v}` }
  }
  const lower = v.toLowerCase()
  const byName = keys.find(
    (k) =>
      k.name.toLowerCase() === lower ||
      (k.key_prefix || "").toLowerCase() === lower ||
      (k.key_prefix || "").toLowerCase().startsWith(lower) ||
      k.name.toLowerCase().includes(lower),
  )
  if (byName) {
    return {
      keyID: String(byName.id),
      label: byName.key_prefix
        ? `${byName.name} · ${byName.key_prefix}`
        : byName.name,
    }
  }
  // 未匹配到：仍按数字失败时无法筛选；返回 all 并保留展示文案无意义
  // 这里把自定义串当作无效，调用方应提示；或尝试 id
  return { keyID: "all", label: v }
}

function UsageKeyCombobox({
  keys,
  groups,
  value,
  onChange,
}: {
  keys: GatewayKey[]
  groups: GatewayGroup[]
  value: string
  onChange: (keyID: string) => void
}) {
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState("")

  useEffect(() => {
    if (!open) setQuery("")
  }, [open])

  const groupNameByID = useMemo(() => {
    const m = new Map<number, string>()
    for (const g of groups) m.set(g.id, g.name)
    return m
  }, [groups])

  const selected = useMemo(() => {
    if (!value || value === "all") return null
    return keys.find((k) => String(k.id) === value) ?? null
  }, [keys, value])

  // 触发器只展示密钥名称；下拉 option 仍为双行详情
  const label =
    !value || value === "all"
      ? "全部密钥"
      : selected
        ? selected.name
        : /^\d+$/.test(value)
          ? `密钥 #${value}`
          : value

  const q = query.trim()
  const qLower = q.toLowerCase()
  const filtered = !q
    ? keys
    : keys.filter((k) => {
        const gn = (groupNameByID.get(k.group_id) || "").toLowerCase()
        return (
          k.name.toLowerCase().includes(qLower) ||
          (k.key_prefix || "").toLowerCase().includes(qLower) ||
          String(k.id).includes(q) ||
          gn.includes(qLower)
        )
      })

  const exactInList =
    !!q &&
    keys.some(
      (k) =>
        String(k.id) === q ||
        k.name.toLowerCase() === qLower ||
        (k.key_prefix || "").toLowerCase() === qLower,
    )
  const customResolved = q && !exactInList ? resolveUsageKeyFilter(q, keys) : null
  const showCustom =
    !!customResolved &&
    customResolved.keyID !== "all" &&
    customResolved.keyID !== value

  function pick(keyID: string) {
    onChange(keyID)
    setOpen(false)
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          type="button"
          variant="outline"
          role="combobox"
          aria-expanded={open}
          className="h-9 w-full justify-between bg-background px-3 font-normal"
        >
          <span className="min-w-0 truncate text-left">{label}</span>
          <ChevronsUpDown className="ml-2 size-3.5 shrink-0 opacity-50" />
        </Button>
      </PopoverTrigger>
      <PopoverContent
        className="w-[var(--radix-popover-trigger-width)] p-0"
        align="start"
      >
        <Command shouldFilter={false}>
          <CommandInput
            value={query}
            onValueChange={setQuery}
            placeholder="搜索或输入密钥名 / 前缀 / ID"
          />
          <CommandList>
            <CommandEmpty>无匹配密钥</CommandEmpty>
            <CommandGroup>
              <CommandItem value="__all__" onSelect={() => pick("all")}>
                <Check
                  className={cn(
                    "size-4",
                    !value || value === "all" ? "opacity-100" : "opacity-0",
                  )}
                />
                全部密钥
              </CommandItem>
            </CommandGroup>
            {filtered.length > 0 || showCustom ? <CommandSeparator /> : null}
            <CommandGroup>
              {filtered.map((k) => {
                const groupLabel =
                  k.group_id > 0
                    ? groupNameByID.get(k.group_id) || `组#${k.group_id}`
                    : ""
                return (
                  <CommandItem
                    key={k.id}
                    value={`${k.id} ${k.name} ${k.key_prefix || ""} ${groupLabel}`}
                    onSelect={() => pick(String(k.id))}
                    className="items-start py-2"
                  >
                    <Check
                      className={cn(
                        "mt-0.5 size-4 shrink-0",
                        value === String(k.id) ? "opacity-100" : "opacity-0",
                      )}
                    />
                    <div className="min-w-0 flex-1 space-y-0.5">
                      <div className="flex items-baseline justify-between gap-2">
                        <span className="min-w-0 truncate font-medium leading-tight">
                          {k.name}
                        </span>
                        {groupLabel ? (
                          <span className="shrink-0 text-[11px] leading-tight text-muted-foreground">
                            {groupLabel}
                          </span>
                        ) : null}
                      </div>
                      {k.key_prefix ? (
                        <div className="truncate font-mono text-[11px] leading-tight text-muted-foreground">
                          {k.key_prefix}
                        </div>
                      ) : null}
                    </div>
                  </CommandItem>
                )
              })}
              {showCustom && customResolved ? (
                <CommandItem
                  value={`custom ${q}`}
                  onSelect={() => pick(customResolved.keyID)}
                  className="items-start py-2"
                >
                  <Check className="mt-0.5 size-4 shrink-0 opacity-0" />
                  <div className="min-w-0 flex-1 space-y-0.5">
                    <div className="truncate text-sm leading-tight">
                      使用自定义
                    </div>
                    <div className="truncate font-mono text-[11px] leading-tight text-muted-foreground">
                      {customResolved.label}
                    </div>
                  </div>
                </CommandItem>
              ) : null}
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  )
}

type UsageQueryOpts = {
  groupID: string
  keyID: string
  model: string
  requestID: string
  success: string
  from: string
  to: string
  page: number
  pageSize: number
}

type UsagePanelProps = {
  usage: GatewayUsagePage | null
  usageStats: GatewayUsageStats | null
  usageLoading: boolean
  groups: GatewayGroup[]
  usageKeys: GatewayKey[]
  usageGroupFilter: string
  setUsageGroupFilter: (v: string) => void
  usageKeyFilter: string
  setUsageKeyFilter: (v: string) => void
  usageModelFilter: string
  setUsageModelFilter: (v: string) => void
  usageModelOptions: GatewayUsageModelOption[]
  usageRequestIDFilter: string
  setUsageRequestIDFilter: (v: string) => void
  usageSuccessFilter: string
  setUsageSuccessFilter: (v: string) => void
  usageFrom: string
  setUsageFrom: (v: string) => void
  usageTo: string
  setUsageTo: (v: string) => void
  setUsagePage: (v: number) => void
  usagePageSize: number
  setUsagePageSize: (v: number) => void
  usageQueryOpts: (page?: number, pageSize?: number) => UsageQueryOpts
  loadUsage: (opts?: Partial<UsageQueryOpts> & { groupID?: string | number | null }) => void
  refreshUsage: (page?: number) => void
  goUsagePage: (p: number) => void
}

export function UsagePanel({
  usage,
  usageStats,
  usageLoading,
  groups,
  usageKeys,
  usageGroupFilter,
  setUsageGroupFilter,
  usageKeyFilter,
  setUsageKeyFilter,
  usageModelFilter,
  setUsageModelFilter,
  usageModelOptions,
  usageRequestIDFilter,
  setUsageRequestIDFilter,
  usageSuccessFilter,
  setUsageSuccessFilter,
  usageFrom,
  setUsageFrom,
  usageTo,
  setUsageTo,
  setUsagePage,
  usagePageSize,
  setUsagePageSize,
  usageQueryOpts,
  loadUsage,
  refreshUsage,
  goUsagePage,
}: UsagePanelProps) {
  return (
    <div className="space-y-4">
{usageStats ? (
  <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-6">
    {[
      {
        label: "请求数",
        value: String(usageStats.total_requests ?? 0),
        hint: `成功 ${usageStats.success_count ?? 0} · 失败 ${usageStats.error_count ?? 0}`,
      },
      {
        label: "Tokens",
        value: formatTokens(usageStats.total_tokens ?? 0),
        // 互斥桶：入/出/读/写（sub2api 同款 K/M 格式）
        hintAlways: `入 ${formatTokens(usageStats.total_input_tokens ?? 0)} · 出 ${formatTokens(usageStats.total_output_tokens ?? 0)} · 读 ${formatTokens(usageStats.total_cache_read_tokens ?? 0)} · 写 ${formatTokens(usageStats.total_cache_creation_tokens ?? 0)}`,
      },
      {
        label: "费用",
        value: money(usageStats.total_actual_cost),
        hint: `标准 ${money(usageStats.total_cost)}`,
      },
      {
        label: "平均耗时",
        // 与表格延迟列一致：&lt;1s 显示 ms，否则 x.xxs（如 16.71s）
        value: formatDurationMS(usageStats.average_duration_ms ?? 0),
        hint: "",
      },
      {
        label: "RPM",
        value: String(usageStats.rpm ?? 0),
        hint: "近 5 分钟均值",
      },
      {
        label: "TPM",
        value: String(usageStats.tpm ?? 0),
        hint: "入+出 · 近 5 分钟",
      },
    ].map((tile) => (
      <div
        key={tile.label}
        className="flex min-h-[4.75rem] min-w-0 flex-col justify-between gap-0.5 rounded-xl border border-border bg-muted/20 px-3 py-2.5"
      >
        <span className="text-[11px] text-muted-foreground">{tile.label}</span>
        <div className="truncate text-xl font-semibold tabular-nums tracking-tight">
          {tile.value}
        </div>
        {tile.label === "Tokens" ? (
          <div
            className="truncate text-[11px] tabular-nums text-muted-foreground"
            title={tile.hintAlways}
          >
            {tile.hintAlways}
          </div>
        ) : tile.hint ? (
          <div className="truncate text-[11px] text-muted-foreground">{tile.hint}</div>
        ) : (
          <div className="h-3.5" />
        )}
      </div>
    ))}
  </div>
) : null}

<Card className="overflow-hidden border-border shadow-none">
  <CardContent className="space-y-3 p-4 sm:p-5">
    {/* 筛选条：网格对齐，快捷时间 + 查询/重置 */}
    <div className="space-y-3 rounded-xl border border-border bg-muted/15 p-3 sm:p-4">
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 2xl:grid-cols-7">
        <div className="space-y-1.5">
          <Label className="text-xs font-medium text-muted-foreground">
            网关组
          </Label>
          <Select
            value={usageGroupFilter}
            onValueChange={(v) => {
              // 组变更：密钥联动重置为全部，并立即按新组查询
              setUsageGroupFilter(v)
              setUsageKeyFilter("all")
              setUsagePage(1)
              void loadUsage({
                ...usageQueryOpts(1, usagePageSize),
                groupID: v,
                keyID: "all",
              })
            }}
          >
            <SelectTrigger className="h-9 w-full bg-background">
              <SelectValue placeholder="全部网关组" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">全部网关组</SelectItem>
              {groups.map((g) => (
                <SelectItem key={g.id} value={String(g.id)}>
                  {g.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div className="space-y-1.5">
          <Label className="text-xs font-medium text-muted-foreground">
            密钥
          </Label>
          <UsageKeyCombobox
            keys={usageKeys}
            groups={groups}
            value={usageKeyFilter}
            onChange={(v) => {
              setUsageKeyFilter(v)
              setUsagePage(1)
              void loadUsage({
                ...usageQueryOpts(1, usagePageSize),
                keyID: v,
              })
            }}
          />
        </div>
        <div className="space-y-1.5">
          <Label className="text-xs font-medium text-muted-foreground">
            模型
          </Label>
          <Select
            value={usageModelFilter || "all"}
            onValueChange={(v) => {
              setUsageModelFilter(v)
              setUsagePage(1)
              void loadUsage({
                ...usageQueryOpts(1, usagePageSize),
                model: v,
              })
            }}
          >
            <SelectTrigger className="h-9 w-full bg-background">
              <SelectValue placeholder="全部模型" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">全部模型</SelectItem>
              {usageModelOptions.map((m) => (
                <SelectItem key={m.model} value={m.model}>
                  <span className="truncate font-mono text-xs">{m.model}</span>
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div className="space-y-1.5">
          <Label className="text-xs font-medium text-muted-foreground">
            Request ID
          </Label>
          <Input
            className="h-9 w-full bg-background font-mono text-xs"
            placeholder="支持部分匹配"
            value={usageRequestIDFilter}
            onChange={(e) => setUsageRequestIDFilter(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                setUsagePage(1)
                void loadUsage(usageQueryOpts(1, usagePageSize))
              }
            }}
          />
        </div>
        <div className="space-y-1.5">
          <Label className="text-xs font-medium text-muted-foreground">
            结果
          </Label>
          <Select
            value={usageSuccessFilter}
            onValueChange={setUsageSuccessFilter}
          >
            <SelectTrigger className="h-9 w-full bg-background">
              <SelectValue placeholder="全部结果" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">全部结果</SelectItem>
              <SelectItem value="success">仅成功</SelectItem>
              <SelectItem value="client">客户端断开</SelectItem>
              <SelectItem value="fail">仅失败</SelectItem>
              <SelectItem value="multi">含重试 / 顺延</SelectItem>
              <SelectItem value="multi_success">
                顺延后成功（如 2/2 · 顺延）
              </SelectItem>
              <SelectItem value="multi_fail">重试/顺延后仍失败</SelectItem>
            </SelectContent>
          </Select>
        </div>
        <div className="space-y-1.5">
          <Label className="text-xs font-medium text-muted-foreground">
            开始时间
          </Label>
          <Input
            type="datetime-local"
            step={1}
            className="h-9 w-full bg-background font-mono text-xs sm:text-sm"
            value={usageFrom}
            onChange={(e) => setUsageFrom(e.target.value)}
          />
        </div>
        <div className="space-y-1.5">
          <Label className="text-xs font-medium text-muted-foreground">
            结束时间
          </Label>
          <Input
            type="datetime-local"
            step={1}
            className="h-9 w-full bg-background font-mono text-xs sm:text-sm"
            value={usageTo}
            onChange={(e) => setUsageTo(e.target.value)}
          />
        </div>
      </div>

      <div className="flex flex-wrap items-center justify-between gap-2 border-t border-border/60 pt-3">
        <div className="flex flex-wrap items-center gap-1.5">
          <span className="mr-1 text-[11px] text-muted-foreground">
            快捷：
          </span>
          {(
            [
              { id: "1h", label: "近 1 小时" },
              { id: "today", label: "今天" },
              { id: "7d", label: "近 7 天" },
              { id: "30d", label: "近 30 天" },
            ] as const
          ).map((p) => (
            <Button
              key={p.id}
              type="button"
              size="sm"
              variant="outline"
              className="h-7 px-2 text-xs"
              disabled={usageLoading}
              onClick={() => {
                const now = new Date()
                const pad = (n: number) => String(n).padStart(2, "0")
                // datetime-local 展示用本地时间（含秒，避免结束时间被截成整分）
                const toLocalInput = (d: Date) =>
                  `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
                const startOfDay = (d: Date) =>
                  new Date(d.getFullYear(), d.getMonth(), d.getDate(), 0, 0, 0, 0)
                // 今天 / 近 N 天：结束时间固定为「当天」23:59:59，覆盖整天记录
                const endOfToday = new Date(
                  now.getFullYear(),
                  now.getMonth(),
                  now.getDate(),
                  23,
                  59,
                  59,
                  0,
                )
                let from = new Date(now)
                let toEnd = new Date(now)
                if (p.id === "1h") {
                  from = new Date(now.getTime() - 3600_000)
                  // 近 1 小时仍以当前时刻为结束（秒取满当前秒）
                  toEnd = new Date(now)
                } else if (p.id === "today") {
                  from = startOfDay(now)
                  toEnd = endOfToday
                } else if (p.id === "7d") {
                  // 含今天共 7 个自然日：今天-6 天 00:00:00 ~ 今天 23:59:59
                  const day = startOfDay(now)
                  day.setDate(day.getDate() - 6)
                  from = day
                  toEnd = endOfToday
                } else {
                  // 含今天共 30 个自然日
                  const day = startOfDay(now)
                  day.setDate(day.getDate() - 29)
                  from = day
                  toEnd = endOfToday
                }
                const fromStr = toLocalInput(from)
                const toStr = toLocalInput(toEnd)
                setUsageFrom(fromStr)
                setUsageTo(toStr)
                setUsagePage(1)
                // 直接传本地串，由 loadUsage 统一转 ISO（勿依赖尚未 commit 的 state）
                void loadUsage({
                  groupID: usageGroupFilter,
                  keyID: usageKeyFilter,
                  model: usageModelFilter,
                  requestID: usageRequestIDFilter,
                  success: usageSuccessFilter,
                  from: fromStr,
                  to: toStr,
                  page: 1,
                  pageSize: usagePageSize,
                })
              }}
            >
              {p.label}
            </Button>
          ))}
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Button
            type="button"
            size="sm"
            variant="ghost"
            className="h-8"
            disabled={usageLoading}
            onClick={() => {
              setUsageGroupFilter("all")
              setUsageKeyFilter("all")
              setUsageModelFilter("all")
              setUsageRequestIDFilter("")
              setUsageSuccessFilter("all")
              setUsageFrom("")
              setUsageTo("")
              setUsagePage(1)
              void loadUsage({
                groupID: "all",
                keyID: "all",
                model: "all",
                requestID: "",
                success: "all",
                from: "",
                to: "",
                page: 1,
                pageSize: usagePageSize,
              })
            }}
          >
            重置
          </Button>
          <Button
            size="sm"
            variant="outline"
            className="h-8"
            disabled={usageLoading}
            onClick={() => refreshUsage()}
          >
            <RefreshCw
              className={cn("size-3.5", usageLoading && "animate-spin")}
            />
            刷新
          </Button>
          <Button
            size="sm"
            className="h-8 min-w-[4.5rem]"
            disabled={usageLoading}
            onClick={() => {
              setUsagePage(1)
              void loadUsage(usageQueryOpts(1, usagePageSize))
            }}
          >
            {usageLoading ? (
              <Loader2 className="size-3.5 animate-spin" />
            ) : (
              <Search className="size-3.5" />
            )}
            查询
          </Button>
        </div>
      </div>
    </div>

    {usageLoading && !usage ? (
      <div className="flex items-center justify-center gap-2 py-12 text-sm text-muted-foreground">
        <Loader2 className="size-4 animate-spin" />
        加载使用记录…
      </div>
    ) : (
      <>
        <div className={cn(usageLoading && "opacity-60 pointer-events-none")}>
          <GatewayUsageTable
            items={usage?.items ?? []}
            onCleaned={() => {
              setUsagePage(1)
              refreshUsage(1)
            }}
          />
        </div>
        {/* 分页：始终展示总数；多页时显示翻页与每页条数 */}
        <div className="flex flex-wrap items-center justify-between gap-3 border-t border-border/60 pt-3 text-sm">
          <div className="text-muted-foreground">
            {usageLoading ? (
              <span className="inline-flex items-center gap-1.5">
                <Loader2 className="size-3.5 animate-spin" />
                加载中…
              </span>
            ) : (
              <>
                共{" "}
                <span className="font-medium tabular-nums text-foreground">
                  {usage?.total ?? 0}
                </span>{" "}
                条
                {(usage?.pages ?? 0) > 0 ? (
                  <>
                    {" "}
                    · 第{" "}
                    <span className="font-medium tabular-nums text-foreground">
                      {usage?.page ?? 1}
                    </span>
                    /
                    <span className="tabular-nums">{usage?.pages ?? 1}</span>{" "}
                    页
                  </>
                ) : null}
                {usage?.page_size ? (
                  <span className="text-muted-foreground">
                    {" "}
                    · 本页 {usage.items?.length ?? 0} 条
                  </span>
                ) : null}
              </>
            )}
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <div className="flex items-center gap-1.5">
              <span className="text-xs text-muted-foreground">每页</span>
              <Select
                value={String(usagePageSize)}
                onValueChange={(v) => {
                  const size = Number(v) || 50
                  setUsagePageSize(size)
                  setUsagePage(1)
                  void loadUsage(usageQueryOpts(1, size))
                }}
              >
                <SelectTrigger className="h-8 w-[4.5rem]">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="20">20</SelectItem>
                  <SelectItem value="50">50</SelectItem>
                  <SelectItem value="100">100</SelectItem>
                  <SelectItem value="200">200</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <Button
              size="sm"
              variant="outline"
              className="h-8"
              disabled={
                usageLoading || !usage || (usage.page ?? 1) <= 1
              }
              onClick={() => goUsagePage(1)}
            >
              首页
            </Button>
            <Button
              size="sm"
              variant="outline"
              className="h-8"
              disabled={
                usageLoading || !usage || (usage.page ?? 1) <= 1
              }
              onClick={() => goUsagePage((usage?.page ?? 1) - 1)}
            >
              上一页
            </Button>
            <Button
              size="sm"
              variant="outline"
              className="h-8"
              disabled={
                usageLoading ||
                !usage ||
                (usage.page ?? 1) >= (usage.pages || 1)
              }
              onClick={() => goUsagePage((usage?.page ?? 1) + 1)}
            >
              下一页
            </Button>
            <Button
              size="sm"
              variant="outline"
              className="h-8"
              disabled={
                usageLoading ||
                !usage ||
                (usage.page ?? 1) >= (usage.pages || 1) ||
                (usage.pages ?? 0) <= 1
              }
              onClick={() => goUsagePage(usage?.pages ?? 1)}
            >
              末页
            </Button>
          </div>
        </div>
      </>
    )}
  </CardContent>
</Card>

    </div>
  )
}
