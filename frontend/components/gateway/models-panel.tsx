import { DatabaseZap, Loader2, Plus, RefreshCw } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import { cn } from "@/lib/utils"
import type {
  GatewayModelListItem,
  GatewayModelTestResult,
  GatewayRoute,
} from "@/lib/api-types"
import {
  ModelMappingEditor,
  type MappingRow,
} from "./model-mapping-editor"
import {
  findSourceTest,
  resolveModelSources,
  sourceTagTone,
  testBusyKey,
} from "./gateway-utils"

const MODELS_MODE_OPTIONS = [
  {
    value: "auto",
    label: "auto · 实时聚合",
    description:
      "GET /v1/models 按路由实时拉取各上游模型并去重映射，不读下方列表",
    hint: "公开模型目录来自各路由上游的实时聚合；下方列表仅作管理/测试参考，保存后也不影响 /v1/models。",
  },
  {
    value: "manual",
    label: "manual · 仅列表",
    description: "GET /v1/models 只返回下方已保存的模型列表",
    hint: "公开模型目录完全由下方列表决定；需「从渠道同步」或手动添加后点保存才会生效。",
  },
  {
    value: "hybrid",
    label: "hybrid · 聚合∪补充目录",
    description: "实时聚合上游模型，并合并自定义和官方目录项",
    hint: "公开模型目录 = 上游实时聚合 ∪ 下方「自定义」模型 ∪「官方目录」模型。",
  },
] as const

function modelsModeHint(mode: string): string {
  return (
    MODELS_MODE_OPTIONS.find((o) => o.value === mode)?.hint ??
    MODELS_MODE_OPTIONS[0].hint
  )
}

type ModelsPanelProps = {
  busy: boolean
  modelsMode: string
  onModelsModeChange: (v: string) => void
  onSyncModels: () => void
  onOpenCatalog: () => void
  onSave: () => void
  customModel: string
  onCustomModelChange: (v: string) => void
  onAddCustomModel: () => void
  modelItems: GatewayModelListItem[]
  setModelItems: (items: GatewayModelListItem[]) => void
  routeDrafts: Partial<GatewayRoute>[]
  channelNameByID: Map<number, string>
  providerNameByID?: Map<number, string>
  modelTestResults: Record<string, GatewayModelTestResult[]>
  modelTesting: string | null
  onRunModelTestFor: (modelID: string, routeID?: number) => void
  onOpenModelTest: (m: GatewayModelListItem) => void
  mappingRows: MappingRow[]
  onMappingRowsChange: (rows: MappingRow[]) => void
  modelSuggestions: string[]
}

export function ModelsPanel({
  busy,
  modelsMode,
  onModelsModeChange,
  onSyncModels,
  onOpenCatalog,
  onSave,
  customModel,
  onCustomModelChange,
  onAddCustomModel,
  modelItems,
  setModelItems,
  routeDrafts,
  channelNameByID,
  providerNameByID,
  modelTestResults,
  modelTesting,
  onRunModelTestFor,
  onOpenModelTest,
  mappingRows,
  onMappingRowsChange,
  modelSuggestions,
}: ModelsPanelProps) {
  return (
    <div className="space-y-4">
<Card className="overflow-hidden border-border shadow-none">
  <CardContent className="space-y-6 p-4 sm:p-5">
    <div className="space-y-1.5">
      <div className="flex flex-wrap items-center gap-3">
        <Label className="shrink-0">模型列表模式</Label>
        <Select value={modelsMode} onValueChange={onModelsModeChange}>
          <SelectTrigger className="w-[13.5rem]">
            <SelectValue placeholder="选择模式" />
          </SelectTrigger>
          <SelectContent className="min-w-[22rem]">
            {MODELS_MODE_OPTIONS.map((opt) => (
              <SelectItem
                key={opt.value}
                value={opt.value}
                description={opt.description}
              >
                {opt.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Button size="sm" onClick={() => void onSave()} disabled={busy}>
          保存
        </Button>
      </div>
      <p className="max-w-3xl text-[11px] leading-relaxed text-muted-foreground">
        {modelsModeHint(modelsMode)}
      </p>
    </div>

    <div className="flex flex-wrap items-center gap-2">
      <Input
        className="h-9 max-w-xs"
        placeholder="自定义模型 ID"
        value={customModel}
        onChange={(e) => onCustomModelChange(e.target.value)}
        onKeyDown={(e) => e.key === "Enter" && onAddCustomModel()}
      />
      <Button className="h-9" variant="outline" onClick={onAddCustomModel}>
        <Plus className="size-3.5" /> 添加自定义
      </Button>
      <Button
        className="h-9"
        variant="outline"
        onClick={() => void onSyncModels()}
        disabled={busy}
      >
        <RefreshCw className="size-3.5" /> 从渠道同步去重
      </Button>
      <Button
        className="h-9"
        variant="outline"
        onClick={() => void onOpenCatalog()}
        disabled={busy}
      >
        <DatabaseZap className="size-3.5" /> 同步官方模型目录
      </Button>
    </div>

    <div className="w-full min-w-0 space-y-2">
      {/*
        table-fixed + 容器 min-w-0：列宽按可见区域分配，
        「渠道-分组」标签在格内换行，不把整表撑出横向滚动。
      */}
      <div className="w-full min-w-0 overflow-hidden rounded-md border">
        <Table className="w-full table-fixed">
          <TableHeader>
            <TableRow>
              <TableHead className="w-[28%]">模型 ID</TableHead>
              <TableHead className="w-[42%]">渠道-分组</TableHead>
              <TableHead className="w-14 px-1 text-center">状态</TableHead>
              <TableHead className="w-[22%] text-right">操作</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {modelItems.map((m) => {
              const sources = resolveModelSources(
                m,
                routeDrafts,
                channelNameByID,
                providerNameByID,
              )
              const tests = modelTestResults[m.id] ?? []
              const okN = tests.filter((t) => t.ok).length
              return (
                <TableRow key={m.id}>
                  <TableCell className="align-top">
                    <div className="flex min-w-0 flex-wrap items-center gap-1.5">
                      <code className="max-w-full truncate text-sm" title={m.id}>
                        {m.id}
                      </code>
                      <Badge
                        variant={m.source === "custom" ? "secondary" : "outline"}
                        className="h-5 shrink-0 px-1.5 text-[10px] font-normal"
                      >
                        {m.source === "custom"
                          ? "自定义"
                          : m.source === "catalog"
                            ? "官方目录"
                            : "同步"}
                      </Badge>
                    </div>
                  </TableCell>
                  <TableCell className="align-top">
                    {sources.length === 0 ? (
                      <span className="text-xs text-muted-foreground">
                        未配置可用路由
                      </span>
                    ) : (
                      <div className="flex max-w-full flex-wrap content-start gap-1">
                        {sources.map((s) => {
                          const tr = findSourceTest(s, tests)
                          const tone = sourceTagTone(tr)
                          const tagBusy =
                            s.route_id != null &&
                            modelTesting === testBusyKey(m.id, s.route_id)
                          return (
                            <Tooltip key={s.key}>
                              <TooltipTrigger asChild>
                                <button
                                  type="button"
                                  disabled={!!modelTesting}
                                  className={cn(
                                    "inline-flex max-w-full items-center gap-1 rounded-full border px-1.5 py-0 text-left text-[11px] font-normal transition-colors",
                                    "cursor-pointer outline-none focus-visible:ring-2 focus-visible:ring-ring",
                                    "disabled:cursor-wait disabled:opacity-80",
                                    tone.className,
                                    tagBusy && "ring-1 ring-primary/40",
                                  )}
                                  onClick={() => {
                                    if (s.route_id != null) {
                                      void onRunModelTestFor(m.id, s.route_id)
                                    } else {
                                      onOpenModelTest(m)
                                    }
                                  }}
                                >
                                  {tagBusy ? (
                                    <Loader2 className="size-3 shrink-0 animate-spin" />
                                  ) : null}
                                  <span className="min-w-0 break-all">
                                    {s.label}
                                  </span>
                                </button>
                              </TooltipTrigger>
                              <TooltipContent
                                side="top"
                                className="max-w-xs space-y-1 p-2.5 text-xs"
                              >
                                <div className="font-medium">{s.label}</div>
                                {s.sourceTip ? (
                                  <div className="text-muted-foreground">
                                    {s.sourceTip}
                                  </div>
                                ) : null}
                                <div className="text-muted-foreground">
                                  {tagBusy ? "测试中…" : tone.summary}
                                </div>
                                {tr && !tagBusy ? (
                                  <div className="space-y-0.5 border-t border-border/60 pt-1 text-[11px] text-muted-foreground">
                                    {tr.upstream_model ? (
                                      <div>上游模型：{tr.upstream_model}</div>
                                    ) : null}
                                    {tr.upstream_path ? (
                                      <div>路径：{tr.upstream_path}</div>
                                    ) : null}
                                    {tr.status_code ? (
                                      <div>HTTP {tr.status_code}</div>
                                    ) : null}
                                    {tr.ok ? (
                                      <div>延迟 {tr.latency_ms}ms</div>
                                    ) : tr.error ? (
                                      <div className="break-words text-destructive">
                                        {tr.error}
                                      </div>
                                    ) : null}
                                  </div>
                                ) : null}
                                {!tagBusy ? (
                                  <div className="border-t border-border/60 pt-1 text-[11px] text-muted-foreground">
                                    点击直接测试（不打开弹窗）
                                  </div>
                                ) : null}
                              </TooltipContent>
                            </Tooltip>
                          )
                        })}
                      </div>
                    )}
                  </TableCell>
                  <TableCell className="px-1 text-center align-top text-xs">
                    {tests.length === 0 ? (
                      <span className="text-muted-foreground">—</span>
                    ) : (
                      <span
                        className={cn(
                          "font-medium tabular-nums",
                          okN === tests.length
                            ? "text-emerald-600 dark:text-emerald-400"
                            : okN > 0
                              ? "text-amber-600 dark:text-amber-400"
                              : "text-destructive",
                        )}
                      >
                        {okN}/{tests.length}
                      </span>
                    )}
                  </TableCell>
                  <TableCell className="align-top text-right">
                    <div className="flex flex-wrap items-center justify-end gap-1">
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() => onOpenModelTest(m)}
                      >
                        测试
                      </Button>
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() =>
                          onMappingRowsChange([
                            ...mappingRows,
                            { from: m.id, to: "" },
                          ])
                        }
                      >
                        映射
                      </Button>
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() =>
                          setModelItems(
                            modelItems.filter((x) => x.id !== m.id),
                          )
                        }
                      >
                        删除
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              )
            })}
            {modelItems.length === 0 && (
              <TableRow>
                <TableCell
                  colSpan={4}
                  className="text-center text-muted-foreground"
                >
                  同步渠道模型或添加自定义模型
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div>
      <ul className="w-full max-w-full space-y-0.5 text-[11px] leading-5 text-muted-foreground">
        <li>• 禁用路由不展示（不可调度）</li>
        <li>• 未配置路由时，提示先在「路由」里配渠道并启用</li>
        <li>• 直连 provider 会显示渠道名（有的话）或 直连 #id</li>
        <li>
          • 添加自定义模型后，无需再同步，即可在「渠道-分组」列看到全部渠道并直接测试
        </li>
      </ul>
    </div>

    <ModelMappingEditor
      rows={mappingRows}
      onChange={onMappingRowsChange}
      modelSuggestions={modelSuggestions}
    />
  </CardContent>
</Card>

    </div>
  )
}
