import { useState, type Dispatch, type SetStateAction } from "react"
import { Check, ChevronDown, Play, Plus } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
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
import { Switch } from "@/components/ui/switch"
import { cn } from "@/lib/utils"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { formatRatio } from "@/lib/format"
import type {
  GatewayProviderOption,
  GatewayRateConvertMode,
  GatewayRoute,
  GatewayRouteSourceKind,
  GatewayUpstreamProtocol,
  GatewayUserAgentMode,
  RateSnapshot,
} from "@/lib/api-types"
import {
  emptyRoute,
  formatRate,
  hasRoutePauseError,
  isRouteTempPaused,
  routeAccountRate,
  routeSourceKind,
  sourceGroupOptionValue,
  sourceGroupSelectValue,
} from "./gateway-utils"

type ChannelOption = { id: number; name: string }

type SortedRouteRow = {
  route: Partial<GatewayRoute>
  index: number
  rate: number
}

const UA_MODE_OPTIONS: {
  value: GatewayUserAgentMode
  label: string
  description: string
}[] = [
  {
    value: "passthrough",
    label: "透传",
    description: "原样转发客户端 UA",
  },
  {
    value: "group",
    label: "网关组",
    description: "使用组级 User-Agent；组未填写则透传",
  },
  {
    value: "custom",
    label: "自定义",
    description: "本路由自定义 User-Agent，留空则透传",
  },
]

/**
 * 路由 User-Agent：外观像 Select；用 Popover 承载选项，
 * 选「自定义」后在选项下方展开输入框（Radix Select 内嵌 input 无法可靠聚焦输入）。
 */
function RouteUserAgentSelect({
  mode,
  custom,
  onModeChange,
  onCustomChange,
}: {
  mode: GatewayUserAgentMode
  custom: string
  onModeChange: (mode: GatewayUserAgentMode) => void
  onCustomChange: (value: string) => void
}) {
  const [open, setOpen] = useState(false)
  const customTrim = custom.trim()
  const modeLabel =
    UA_MODE_OPTIONS.find((o) => o.value === mode)?.label ?? "透传"
  const triggerTitle =
    mode === "custom" && customTrim ? `自定义：${customTrim}` : modeLabel

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <button
          type="button"
          title={triggerTitle}
          className={cn(
            "border-input bg-transparent shadow-xs dark:bg-input/30 dark:hover:bg-input/50",
            "focus-visible:border-ring focus-visible:ring-ring/50",
            "flex h-9 w-[6.5rem] items-center justify-between gap-1 rounded-md border px-3 py-2 text-sm whitespace-nowrap outline-none focus-visible:ring-[3px]",
          )}
        >
          <span className="min-w-0 flex-1 truncate text-left">{modeLabel}</span>
          <ChevronDown className="size-4 shrink-0 opacity-50" />
        </button>
      </PopoverTrigger>
      <PopoverContent
        align="start"
        className="w-64 p-1"
        // 选「自定义」时由 input autoFocus 接管，避免 Popover 抢焦点到触发器
        onOpenAutoFocus={(e) => {
          if (mode === "custom") e.preventDefault()
        }}
      >
        <div
          className="flex flex-col gap-0.5"
          role="listbox"
          aria-label="User-Agent 策略"
        >
          {UA_MODE_OPTIONS.map((opt) => {
            const selected = mode === opt.value
            return (
              <button
                key={opt.value}
                type="button"
                role="option"
                aria-selected={selected}
                className={cn(
                  "hover:bg-accent hover:text-accent-foreground relative flex w-full cursor-default flex-col items-start rounded-sm py-1.5 pr-8 pl-2 text-left text-sm outline-hidden",
                  selected && "bg-accent/60",
                )}
                onClick={() => {
                  onModeChange(opt.value)
                  if (opt.value !== "custom") {
                    setOpen(false)
                  }
                }}
              >
                <span className="font-medium leading-none">{opt.label}</span>
                <span className="mt-0.5 text-[11px] leading-snug font-normal text-muted-foreground">
                  {opt.description}
                </span>
                {selected ? (
                  <Check className="absolute top-2 right-2 size-4" />
                ) : null}
              </button>
            )
          })}
        </div>
        {mode === "custom" ? (
          <div className="mt-1 border-t border-border px-2 pt-2 pb-2">
            <Input
              key="ua-custom-input"
              autoFocus
              className="h-8 w-full text-xs"
              value={custom}
              placeholder="自定义 User-Agent，留空则透传"
              onChange={(e) => onCustomChange(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  e.preventDefault()
                  setOpen(false)
                }
                e.stopPropagation()
              }}
            />
            <p className="mt-1 text-[10px] leading-4 text-muted-foreground">
              回车或点击外部关闭
            </p>
          </div>
        ) : null}
      </PopoverContent>
    </Popover>
  )
}

type RoutesPanelProps = {
  busy: boolean
  rateSort: string
  onRateSortChange: (v: string) => void
  /** 组级开关状态（编辑组对话框配置）；仅用于说明文案，路由栏不提供开关 */
  rateResortEnabled: boolean
  onSaveSort: () => void
  routeDrafts: Partial<GatewayRoute>[]
  setRouteDrafts: Dispatch<SetStateAction<Partial<GatewayRoute>[]>>
  sortedRouteRows: SortedRouteRow[]
  channelList: ChannelOption[]
  providerOptions: GatewayProviderOption[]
  sourceGroupsByChannel: Record<number, RateSnapshot[]>
  onLoadSourceGroups: (channelID: number) => void
  onLoadProviderOptions: () => void
  onSaveRoutes: () => void
  onEnsureKeys: () => void
  onClearRoutePause: (routeID?: number) => void
  onShowPauseError: (route: Partial<GatewayRoute>) => void
  highlightedRouteID?: number | null
}

export function RoutesPanel({
  busy,
  rateSort,
  onRateSortChange,
  rateResortEnabled,
  onSaveSort,
  routeDrafts,
  setRouteDrafts,
  sortedRouteRows,
  channelList,
  providerOptions,
  sourceGroupsByChannel,
  onLoadSourceGroups,
  onLoadProviderOptions,
  onSaveRoutes,
  onEnsureKeys,
  onClearRoutePause,
  onShowPauseError,
  highlightedRouteID,
}: RoutesPanelProps) {
  return (
    <div className="space-y-4">
<Card className="overflow-hidden border-border shadow-none">
  <CardContent className="space-y-3 p-4 sm:p-5">
    <div className="flex flex-wrap items-center gap-2">
      <div className="flex flex-wrap items-center gap-2">
        <Label className="shrink-0 whitespace-nowrap text-sm">
          倍率排序
        </Label>
        <Select value={rateSort} onValueChange={onRateSortChange}>
          <SelectTrigger className="h-9 w-[6.5rem]">
            <SelectValue placeholder="排序" />
          </SelectTrigger>
          <SelectContent className="min-w-[11rem]">
            <SelectItem value="asc" description="低倍率先试">
              升序
            </SelectItem>
            <SelectItem value="desc" description="高倍率先试">
              降序
            </SelectItem>
          </SelectContent>
        </Select>
        <Button
          size="sm"
          variant="outline"
          className="h-9"
          disabled={busy}
          onClick={() => void onSaveSort()}
        >
          保存排序
        </Button>
      </div>
      <div className="flex-1" />
      <Button
        size="sm"
        variant="outline"
        className="h-9"
        onClick={() => setRouteDrafts([...routeDrafts, emptyRoute()])}
      >
        <Plus className="size-3.5" /> 添加路由
      </Button>
      <Button
        size="sm"
        className="h-9"
        onClick={() => void onSaveRoutes()}
        disabled={busy}
      >
        保存路由
      </Button>
      <Button
        size="sm"
        variant="secondary"
        className="h-9"
        onClick={() => void onEnsureKeys()}
        disabled={busy}
      >
        <Play className="size-3.5" /> 确保上游密钥
      </Button>
    </div>
    <p className="text-[11px] leading-5 text-muted-foreground">
      列表顺序 = 尝试顺序：按账号计费倍率
      {rateSort === "desc" ? "从高到低" : "从低到高"}
      排列；同倍率时权重大优先。
      {rateResortEnabled
        ? "已在编辑组中开启「渠道分组价格倍率重排」：倍率扫描结束后会按源分组实时倍率自动重写顺序与计费倍率。"
        : "未开启价格倍率重排时，仅保存路由 / 改排序方向会落库顺序；可在编辑组中开启。"}
      点「保存排序」写入组配置；「保存路由」也会按当前排序写回 position。
    </p>
    <div className="overflow-x-auto">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>类型</TableHead>
            <TableHead>来源</TableHead>
            <TableHead>源分组</TableHead>
            <TableHead>上游协议</TableHead>
            <TableHead>User-Agent</TableHead>
            <TableHead>权重</TableHead>
            <TableHead className="min-w-28">倍率换算</TableHead>
            <TableHead className="min-w-32">账号计费倍率</TableHead>
            <TableHead>启用</TableHead>
            <TableHead>上游 Key</TableHead>
            <TableHead />
          </TableRow>
        </TableHeader>
        <TableBody>
          {sortedRouteRows.map(({ route: r, index: idx }) => {
            const kind = routeSourceKind(r)
            const chId = Number(r.source_channel_id) || 0
            const providerID = Number(r.gateway_provider_id) || 0
            const sgs = sourceGroupsByChannel[chId] ?? []
            const provider = providerOptions.find((p) => p.id === providerID)
            const calculatedRate =
              kind === "provider"
                ? (r.rate_convert_mode as string) === "custom"
                  ? Number(r.rate_convert_value) ||
                    provider?.default_billing_rate ||
                    1
                  : provider?.default_billing_rate || 1
                : routeAccountRate(r, sgs)
            const uaMode: GatewayUserAgentMode =
              r.user_agent_mode === "group" || r.user_agent_mode === "custom"
                ? r.user_agent_mode
                : "passthrough"
            return (
              <TableRow
                key={r.id ?? `new-${idx}`}
                id={r.id ? `gateway-route-${r.id}` : undefined}
                className={cn(
                  "transition-colors duration-300",
                  highlightedRouteID === r.id &&
                    "bg-primary/10 shadow-[inset_3px_0_0_hsl(var(--primary))] ring-1 ring-inset ring-primary/40",
                )}
              >
                <TableCell>
                  <Select
                    value={kind}
                    onValueChange={(v) => {
                      const nextKind = v as GatewayRouteSourceKind
                      setRouteDrafts((prev) => {
                        const next = [...prev]
                        if (nextKind === "provider") {
                          next[idx] = {
                            ...next[idx],
                            source_kind: "provider",
                            source_channel_id: 0,
                            source_group_id: null,
                            source_group_name: "",
                            rate_convert_mode: "custom",
                            rate_convert_value:
                              next[idx].rate_convert_value ?? 1,
                          }
                        } else {
                          next[idx] = {
                            ...next[idx],
                            source_kind: "monitor",
                            gateway_provider_id: 0,
                            rate_convert_mode: "raw",
                          }
                        }
                        return next
                      })
                      if (nextKind === "provider") void onLoadProviderOptions()
                    }}
                  >
                    <SelectTrigger className="w-28">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="monitor">监控渠道</SelectItem>
                      <SelectItem value="provider">直连渠道</SelectItem>
                    </SelectContent>
                  </Select>
                </TableCell>
                <TableCell>
                  {kind === "provider" ? (
                    <Select
                      value={providerID ? String(providerID) : ""}
                      onValueChange={(v) => {
                        const id = Number(v)
                        const p = providerOptions.find((x) => x.id === id)
                        setRouteDrafts((prev) => {
                          const next = [...prev]
                          next[idx] = {
                            ...next[idx],
                            source_kind: "provider",
                            gateway_provider_id: id,
                            source_channel_id: 0,
                            rate_convert_mode: "custom",
                            rate_convert_value:
                              p?.default_billing_rate ?? 1,
                            upstream_protocol:
                              (p?.upstream_protocol as GatewayUpstreamProtocol) ||
                              next[idx].upstream_protocol ||
                              "auto",
                          }
                          return next
                        })
                      }}
                    >
                      <SelectTrigger className="min-w-36">
                        <SelectValue placeholder="选择直连渠道" />
                      </SelectTrigger>
                      <SelectContent>
                        {providerOptions.map((p) => (
                          <SelectItem key={p.id} value={String(p.id)}>
                            {p.name}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  ) : (
                    <Select
                      value={chId ? String(chId) : ""}
                      onValueChange={(v) => {
                        const id = Number(v)
                        void onLoadSourceGroups(id)
                        setRouteDrafts((prev) => {
                          const next = [...prev]
                          next[idx] = {
                            ...next[idx],
                            source_kind: "monitor",
                            source_channel_id: id,
                            gateway_provider_id: 0,
                            source_group_id: null,
                            source_group_name: "",
                          }
                          return next
                        })
                      }}
                    >
                      <SelectTrigger className="min-w-32">
                        <SelectValue placeholder="选择监控渠道" />
                      </SelectTrigger>
                      <SelectContent>
                        {channelList.map((ch) => (
                          <SelectItem key={ch.id} value={String(ch.id)}>
                            {ch.name}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  )}
                </TableCell>
                <TableCell>
                  {kind === "provider" ? (
                    <span className="text-xs text-muted-foreground">—</span>
                  ) : (
                  <Select
                    value={sourceGroupSelectValue(r)}
                    onValueChange={(value) => {
                      if (value === "none") {
                        setRouteDrafts((prev) => {
                          const next = [...prev]
                          next[idx] = {
                            ...next[idx],
                            source_group_id: null,
                            source_group_name: "",
                          }
                          return next
                        })
                        return
                      }
                      const sg = sgs.find(
                        (g) => sourceGroupOptionValue(g) === value,
                      )
                      setRouteDrafts((prev) => {
                        const next = [...prev]
                        next[idx] = {
                          ...next[idx],
                          // sub2api 有 remote_group_id，newapi 常仅有名称；两者都要落库名称便于展示
                          source_group_id:
                            sg?.remote_group_id != null
                              ? sg.remote_group_id
                              : null,
                          source_group_name: (sg?.model_name ?? "").trim(),
                        }
                        return next
                      })
                    }}
                  >
                    <SelectTrigger className="min-w-36">
                      {(() => {
                        const selected = sgs.find(
                          (g) =>
                            sourceGroupOptionValue(g) ===
                            sourceGroupSelectValue(r),
                        )
                        if (selected) {
                          return (
                            <SelectValue>
                              {selected.model_name} ·{" "}
                              {formatRatio(selected.ratio)}
                            </SelectValue>
                          )
                        }
                        return <SelectValue placeholder="源分组" />
                      })()}
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="none">不绑定分组</SelectItem>
                      {sgs.map((g) => (
                        <SelectItem
                          key={sourceGroupOptionValue(g)}
                          value={sourceGroupOptionValue(g)}
                        >
                          <span className="flex flex-col items-start">
                            <span>
                              {g.model_name} · {formatRatio(g.ratio)}
                            </span>
                            {g.description ? (
                              <span className="max-w-96 whitespace-normal break-words text-[11px] text-muted-foreground">
                                {g.description}
                              </span>
                            ) : null}
                          </span>
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  )}
                </TableCell>
                <TableCell>
                  <Select
                    value={(r.upstream_protocol as string) || "auto"}
                    onValueChange={(v) => {
                      setRouteDrafts((prev) => {
                        const next = [...prev]
                        next[idx] = {
                          ...next[idx],
                          upstream_protocol: v as GatewayUpstreamProtocol,
                        }
                        return next
                      })
                    }}
                  >
                    <SelectTrigger className="w-36">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent className="min-w-[14rem]">
                      <SelectItem value="auto" description="入站协议 + 模型启发">
                        自动
                      </SelectItem>
                      <SelectItem
                        value="openai_chat"
                        description="/v1/chat/completions"
                      >
                        OpenAI Chat
                      </SelectItem>
                      <SelectItem
                        value="openai_responses"
                        description="/v1/responses"
                      >
                        OpenAI Responses
                      </SelectItem>
                      <SelectItem value="anthropic" description="/v1/messages">
                        Anthropic
                      </SelectItem>
                      <SelectItem value="openai" description="兼容，等同 Chat">
                        openai
                      </SelectItem>
                    </SelectContent>
                  </Select>
                </TableCell>
                <TableCell>
                  <RouteUserAgentSelect
                    mode={uaMode}
                    custom={r.user_agent_custom ?? ""}
                    onModeChange={(mode) => {
                      setRouteDrafts((prev) => {
                        const next = [...prev]
                        next[idx] = {
                          ...next[idx],
                          user_agent_mode: mode,
                          // 切走自定义时保留草稿，避免来回切换丢内容；保存时仍按 mode 清空
                          user_agent_custom: next[idx].user_agent_custom ?? "",
                        }
                        return next
                      })
                    }}
                    onCustomChange={(value) => {
                      setRouteDrafts((prev) => {
                        const next = [...prev]
                        next[idx] = {
                          ...next[idx],
                          user_agent_custom: value,
                        }
                        return next
                      })
                    }}
                  />
                </TableCell>
                <TableCell>
                  <Input
                    className="w-16"
                    type="number"
                    value={r.weight ?? 1}
                    onChange={(e) => {
                      setRouteDrafts((prev) => {
                        const next = [...prev]
                        next[idx] = {
                          ...next[idx],
                          weight: Number(e.target.value) || 1,
                        }
                        return next
                      })
                    }}
                  />
                </TableCell>
                <TableCell>
                  <Select
                    value={(r.rate_convert_mode as string) || "raw"}
                    onValueChange={(v) => {
                      setRouteDrafts((prev) => {
                        const next = [...prev]
                        next[idx] = {
                          ...next[idx],
                          rate_convert_mode: v as GatewayRateConvertMode,
                        }
                        return next
                      })
                    }}
                  >
                    <SelectTrigger className="w-28">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="raw">原值</SelectItem>
                      <SelectItem value="multiply_100">x100</SelectItem>
                      <SelectItem value="divide_100">/100</SelectItem>
                      <SelectItem value="custom">自定义</SelectItem>
                    </SelectContent>
                  </Select>
                </TableCell>
                <TableCell>
                  <Input
                    className="w-32"
                    type="number"
                    step="0.01"
                    title="账号计费倍率（原值显示源分组倍率）"
                    value={
                      (r.rate_convert_mode as string) === "custom"
                        ? String(r.rate_convert_value ?? 0)
                        : formatRate(calculatedRate)
                    }
                    disabled={(r.rate_convert_mode as string) !== "custom"}
                    onChange={(e) => {
                      setRouteDrafts((prev) => {
                        const next = [...prev]
                        next[idx] = {
                          ...next[idx],
                          rate_convert_value: Number(e.target.value || 0),
                        }
                        return next
                      })
                    }}
                  />
                </TableCell>
                <TableCell>
                  <Switch
                    checked={r.enabled !== false}
                    onCheckedChange={(v) => {
                      setRouteDrafts((prev) => {
                        const next = [...prev]
                        next[idx] = { ...next[idx], enabled: v }
                        return next
                      })
                    }}
                  />
                </TableCell>
                <TableCell className="text-xs max-w-36">
                  <div className="truncate">
                    {kind === "provider" ? (
                      provider ? (
                        <span title={provider.base_url}>
                          {provider.name}
                          {provider.api_key_hint
                            ? ` · ${provider.api_key_hint}`
                            : ""}
                        </span>
                      ) : (
                        <span className="text-muted-foreground">
                          未选直连渠道
                        </span>
                      )
                    ) : (
                      r.source_api_key_name || (
                        <span className="text-muted-foreground">未确保</span>
                      )
                    )}
                  </div>
                  {hasRoutePauseError(r) && (
                    <div className="mt-1 flex flex-wrap items-center gap-1">
                      {isRouteTempPaused(r.temp_unschedulable_until) ? (
                        <Badge variant="destructive" title={
                          r.temp_unschedulable_until
                            ? `冷却至 ${new Date(r.temp_unschedulable_until).toLocaleString("zh-CN")}`
                            : undefined
                        }>
                          暂停中
                          {r.temp_unschedulable_until
                            ? ` · ${new Date(r.temp_unschedulable_until).toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit", second: "2-digit" })}`
                            : ""}
                        </Badge>
                      ) : (
                        <Badge variant="secondary">已恢复</Badge>
                      )}
                      <Button
                        size="sm"
                        variant="outline"
                        className="h-6 px-1.5 text-xs"
                        onClick={() => onShowPauseError(r)}
                      >
                        错误
                      </Button>
                      {r.id ? (
                        <Button
                          size="sm"
                          variant="outline"
                          className="h-6 px-1.5 text-xs"
                          onClick={() => void onClearRoutePause(r.id)}
                        >
                          清除
                        </Button>
                      ) : null}
                    </div>
                  )}
                </TableCell>
                <TableCell className="text-right">
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() =>
                      setRouteDrafts((prev) => prev.filter((_, i) => i !== idx))
                    }
                  >
                    删除
                  </Button>
                </TableCell>
              </TableRow>
            )
          })}
          {routeDrafts.length === 0 && (
            <TableRow>
              <TableCell colSpan={11} className="text-center text-muted-foreground">
                添加路由后保存。监控渠道需「确保上游密钥」；直连渠道使用已配置的 Key
              </TableCell>
            </TableRow>
          )}
        </TableBody>
      </Table>
    </div>
  </CardContent>
</Card>

    </div>
  )
}
