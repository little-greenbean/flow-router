import { toast } from "sonner"
import {
  CheckCircle2,
  Copy,
  FlaskConical,
  Loader2,
  Search,
  XCircle,
} from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { Textarea } from "@/components/ui/textarea"
import { copyText } from "@/lib/clipboard"
import { cn } from "@/lib/utils"
import type {
  GatewayEnsureKeysResult,
  GatewayGroup,
  GatewayKey,
  GatewayModelListItem,
  GatewayModelSyncResult,
  GatewayModelTestResult,
  GatewayRoute,
  ModelDefaultPrice,
} from "@/lib/api-types"
import {
  API_KEY_LENS,
  type APIKeyLen,
  findSourceTest,
  isRouteTempPaused,
  perTokenToMTok,
  resolveModelSources,
  sourceTagTone,
  testBusyKey,
  type GroupFormState,
  type KeyFormState,
} from "./gateway-utils"

export function ModelTestDialog({
  open,
  onOpenChange,
  modelTestTarget,
  modelTestDialogResults,
  modelTesting,
  routeDrafts,
  channelNameByID,
  providerNameByID,
  onRunModelTest,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  modelTestTarget: GatewayModelListItem | null
  modelTestDialogResults: GatewayModelTestResult[]
  modelTesting: string | null
  routeDrafts: Partial<GatewayRoute>[]
  channelNameByID: Map<number, string>
  providerNameByID?: Map<number, string>
  onRunModelTest: (routeID?: number) => void
}) {
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        onOpenChange(next)
      }}
    >
      <DialogContent className="flex max-h-[min(85dvh,720px)] w-[calc(100vw-1.5rem)] max-w-2xl min-w-0 flex-col gap-3 overflow-hidden">
        <DialogHeader className="min-w-0 shrink-0 space-y-1.5">
          <DialogTitle className="flex items-center gap-2">
            <FlaskConical className="size-4 shrink-0" />
            测试模型
          </DialogTitle>
          <DialogDescription className="min-w-0 break-words">
            {modelTestTarget ? (
              <>
                向各渠道发送最小探测，验证{" "}
                <code className="inline-block max-w-full break-all rounded bg-muted px-1 text-xs">
                  {modelTestTarget.id}
                </code>{" "}
                是否可用。多路由可批量或逐条测试。
              </>
            ) : (
              "选择模型后测试可用性"
            )}
          </DialogDescription>
        </DialogHeader>

        {modelTestTarget ? (
          (() => {
            const sources = resolveModelSources(
              modelTestTarget,
              routeDrafts,
              channelNameByID,
              providerNameByID,
            )
            const results = modelTestDialogResults
            const okN = results.filter((t) => t.ok).length
            const batchBusy =
              modelTesting === testBusyKey(modelTestTarget.id)
            return (
              <>
                <div className="flex min-w-0 shrink-0 flex-wrap items-center justify-between gap-2">
                  <div className="min-w-0 text-xs text-muted-foreground">
                    {sources.length} 个来源
                    {results.length > 0 ? (
                      <span
                        className={cn(
                          "ml-2 font-medium",
                          okN === results.length
                            ? "text-emerald-600 dark:text-emerald-400"
                            : okN > 0
                              ? "text-amber-600 dark:text-amber-400"
                              : "text-destructive",
                        )}
                      >
                        最近 {okN}/{results.length} 可用
                      </span>
                    ) : null}
                  </div>
                  <Button
                    size="sm"
                    className="shrink-0"
                    disabled={!!modelTesting || sources.length === 0}
                    onClick={() => void onRunModelTest()}
                  >
                    {batchBusy ? (
                      <Loader2 className="size-3.5 animate-spin" />
                    ) : (
                      <FlaskConical className="size-3.5" />
                    )}
                    {sources.length > 1 ? "批量测试全部" : "开始测试"}
                  </Button>
                </div>

                <div className="min-h-0 min-w-0 flex-1 overflow-y-auto overflow-x-hidden">
                  {sources.length === 0 ? (
                    <div className="rounded-md border px-4 py-10 text-center text-sm text-muted-foreground">
                      暂无关联路由。请先在「路由」中配置渠道/直连并启用，自定义模型会自动关联全部启用路由。
                    </div>
                  ) : (
                    <ul className="space-y-2">
                      {sources.map((s) => {
                        const tr = findSourceTest(s, results)
                        const tone = sourceTagTone(tr)
                        const singleBusy =
                          s.route_id != null &&
                          modelTesting ===
                            testBusyKey(modelTestTarget.id, s.route_id)
                        return (
                          <li
                            key={s.key}
                            className="min-w-0 rounded-lg border border-border bg-card p-3"
                          >
                            <div className="flex min-w-0 items-start justify-between gap-2">
                              <div className="min-w-0 flex-1 space-y-1">
                                <Badge
                                  variant="outline"
                                  className={cn(
                                    "max-w-full whitespace-normal break-all px-1.5 py-0.5 text-[11px] font-normal",
                                    tone.className,
                                  )}
                                >
                                  {s.label}
                                </Badge>
                                {tr?.upstream_model &&
                                tr.upstream_model !== modelTestTarget.id ? (
                                  <div className="break-all text-[11px] text-muted-foreground">
                                    上游 {tr.upstream_model}
                                  </div>
                                ) : null}
                              </div>
                              <Button
                                size="sm"
                                variant="outline"
                                className="shrink-0"
                                disabled={!!modelTesting || s.route_id == null}
                                title={
                                  s.route_id == null
                                    ? "缺少路由 ID，请重新同步模型"
                                    : undefined
                                }
                                onClick={() =>
                                  s.route_id != null && void onRunModelTest(s.route_id)
                                }
                              >
                                {singleBusy ? (
                                  <Loader2 className="size-3.5 animate-spin" />
                                ) : (
                                  <FlaskConical className="size-3.5" />
                                )}
                                测试
                              </Button>
                            </div>

                            <div className="mt-2 min-w-0">
                              {!tr ? (
                                <span className="text-xs text-muted-foreground">未测试</span>
                              ) : tr.ok ? (
                                <div className="space-y-0.5 text-xs">
                                  <div className="inline-flex items-center gap-1 font-medium text-emerald-600 dark:text-emerald-400">
                                    <CheckCircle2 className="size-3.5 shrink-0" />
                                    可用 · 首字 {tr.latency_ms}ms
                                    {tr.status_code ? ` · HTTP ${tr.status_code}` : ""}
                                  </div>
                                </div>
                              ) : (
                                <div className="min-w-0 space-y-1.5 text-xs">
                                  <div className="inline-flex items-center gap-1 font-medium text-destructive">
                                    <XCircle className="size-3.5 shrink-0" />
                                    失败
                                    {tr.status_code ? ` · ${tr.status_code}` : ""}
                                  </div>
                                  {tr.error ? (
                                    <pre
                                      className="max-h-28 w-full max-w-full overflow-x-hidden overflow-y-auto whitespace-pre-wrap break-words rounded-md border border-destructive/20 bg-destructive/5 px-2.5 py-2 font-mono text-[11px] leading-relaxed text-muted-foreground [overflow-wrap:anywhere]"
                                      title={tr.error}
                                    >
                                      {tr.error}
                                    </pre>
                                  ) : null}
                                </div>
                              )}
                            </div>
                          </li>
                        )
                      })}
                    </ul>
                  )}
                </div>
              </>
            )
          })()
        ) : null}

        <DialogFooter className="shrink-0">
          <Button
            variant="outline"
            onClick={() => onOpenChange(false)}
            disabled={!!modelTesting}
          >
            关闭
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
export function RoutePauseErrorDialog({
  pauseErrorRoute,
  onClose,
  onClearPause,
}: {
  pauseErrorRoute: Partial<GatewayRoute> | null
  onClose: () => void
  onClearPause: (id: number) => void
}) {
  return (
    <Dialog
      open={!!pauseErrorRoute}
      onOpenChange={(next) => {
        if (!next) onClose()
      }}
    >
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>路由错误信息</DialogTitle>
          <DialogDescription>
            最近一次上游失败详情。错误信息会保留到手动清除、连续 3 次成功自动清除，或下次失败覆盖。
          </DialogDescription>
        </DialogHeader>
        {pauseErrorRoute ? (
          <div className="space-y-3 text-sm">
            <div className="grid gap-2 sm:grid-cols-2">
              <div>
                <div className="text-[11px] text-muted-foreground">上游 Key</div>
                <div className="break-all font-medium">
                  {pauseErrorRoute.source_api_key_name || "—"}
                </div>
              </div>
              <div>
                <div className="text-[11px] text-muted-foreground">Route ID</div>
                <div className="font-mono tabular-nums">
                  {pauseErrorRoute.id ?? "—"}
                </div>
              </div>
              <div>
                <div className="text-[11px] text-muted-foreground">请求时间</div>
                <div className="font-mono text-xs">
                  {pauseErrorRoute.temp_unschedulable_at
                    ? new Date(pauseErrorRoute.temp_unschedulable_at).toLocaleString(
                        "zh-CN",
                      )
                    : "—"}
                </div>
              </div>
              <div>
                <div className="text-[11px] text-muted-foreground">请求 ID</div>
                <div className="break-all font-mono text-xs">
                  {pauseErrorRoute.temp_unschedulable_request_id?.trim() || "—"}
                </div>
              </div>
              <div>
                <div className="text-[11px] text-muted-foreground">调度状态</div>
                <div>
                  {isRouteTempPaused(pauseErrorRoute.temp_unschedulable_until)
                    ? "暂停中（不参与调度）"
                    : "已恢复（可调度）"}
                </div>
              </div>
              <div>
                <div className="text-[11px] text-muted-foreground">暂停至</div>
                <div className="font-mono text-xs">
                  {pauseErrorRoute.temp_unschedulable_until
                    ? new Date(pauseErrorRoute.temp_unschedulable_until).toLocaleString(
                        "zh-CN",
                      )
                    : "—"}
                </div>
              </div>
            </div>
            <div>
              <div className="mb-1 flex items-center justify-between gap-2">
                <div className="text-[11px] text-muted-foreground">错误信息</div>
                {pauseErrorRoute.temp_unschedulable_reason ? (
                  <Button
                    type="button"
                    size="sm"
                    variant="ghost"
                    className="h-6 px-2 text-xs"
                    onClick={() => {
                      const meta = [
                        `request_time: ${
                          pauseErrorRoute.temp_unschedulable_at
                            ? new Date(
                                pauseErrorRoute.temp_unschedulable_at,
                              ).toLocaleString("zh-CN")
                            : "—"
                        }`,
                        `request_id: ${
                          pauseErrorRoute.temp_unschedulable_request_id?.trim() ||
                          "—"
                        }`,
                        `route_id: ${pauseErrorRoute.id ?? "—"}`,
                        `source_key: ${
                          pauseErrorRoute.source_api_key_name || "—"
                        }`,
                        "",
                        pauseErrorRoute.temp_unschedulable_reason || "",
                      ].join("\n")
                      void copyText(meta)
                        .then(() => toast.success("已复制错误信息"))
                        .catch(() => toast.error("复制失败"))
                    }}
                  >
                    <Copy className="size-3" /> 复制
                  </Button>
                ) : null}
              </div>
              <pre className="max-h-64 overflow-auto whitespace-pre-wrap break-all rounded-md border bg-muted/30 p-3 font-mono text-[12px] leading-5 text-red-700 dark:text-red-300">
                {pauseErrorRoute.temp_unschedulable_reason?.trim() ||
                  "（无错误详情；若此前已自动清空，请看「使用记录」失败行）"}
              </pre>
            </div>
          </div>
        ) : null}
        <DialogFooter className="gap-2 sm:justify-between">
          <Button
            type="button"
            variant="outline"
            disabled={!pauseErrorRoute?.id}
            onClick={() => {
              const id = pauseErrorRoute?.id
              if (!id) return
              onClose()
              void onClearPause(id)
            }}
          >
            清除暂停
          </Button>
          <Button type="button" onClick={onClose}>
            关闭
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

export function EnsureKeysResultDialog({
  open,
  onOpenChange,
  ensureKeysResult,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  ensureKeysResult: GatewayEnsureKeysResult | null
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex max-h-[min(85dvh,640px)] w-full max-w-lg flex-col gap-3 overflow-hidden sm:max-w-xl">
        <DialogHeader className="shrink-0">
          <DialogTitle>确保上游密钥</DialogTitle>
          <DialogDescription>
            单条失败已跳过，不影响其它路由。Key 名按「渠道 + 源分组」统一，跨网关组可复用。
          </DialogDescription>
        </DialogHeader>
        {ensureKeysResult ? (
          <div className="min-h-0 flex-1 space-y-3 overflow-y-auto">
            <div className="flex flex-wrap gap-2 text-xs">
              <span className="rounded-md bg-emerald-100 px-2 py-1 text-emerald-800 dark:bg-emerald-900/40 dark:text-emerald-200">
                成功 {ensureKeysResult.ok_count}
              </span>
              <span className="rounded-md bg-red-100 px-2 py-1 text-red-800 dark:bg-red-900/40 dark:text-red-200">
                失败 {ensureKeysResult.fail_count}
              </span>
              <span className="rounded-md bg-muted px-2 py-1 text-muted-foreground">
                跳过 {ensureKeysResult.skip_count}
              </span>
            </div>
            <div className="overflow-x-auto rounded-md border">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>来源</TableHead>
                    <TableHead>状态</TableHead>
                    <TableHead>Key 名</TableHead>
                    <TableHead>说明</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {(ensureKeysResult.routes ?? []).map((r) => (
                    <TableRow key={r.route_id}>
                      <TableCell className="text-sm">
                        <div className="font-medium">
                          {r.label || `route#${r.route_id}`}
                        </div>
                        <div className="text-[11px] text-muted-foreground">
                          {r.source_kind === "provider" ? "直连" : "监控"}
                          {r.route_id ? ` · #${r.route_id}` : ""}
                        </div>
                      </TableCell>
                      <TableCell>
                        {r.skipped ? (
                          <Badge variant="secondary">跳过</Badge>
                        ) : r.ok ? (
                          <Badge className="bg-emerald-600 hover:bg-emerald-600">
                            成功
                          </Badge>
                        ) : (
                          <Badge variant="destructive">失败</Badge>
                        )}
                      </TableCell>
                      <TableCell className="max-w-[10rem] font-mono text-[11px]">
                        <span className="line-clamp-2 break-all" title={r.key_name}>
                          {r.key_name || "—"}
                        </span>
                      </TableCell>
                      <TableCell className="max-w-[12rem] text-xs text-muted-foreground">
                        <span
                          className="line-clamp-3 break-all"
                          title={r.error || r.skip_reason}
                        >
                          {r.error || r.skip_reason || (r.ok ? "已写入路由" : "—")}
                        </span>
                      </TableCell>
                    </TableRow>
                  ))}
                  {(ensureKeysResult.routes ?? []).length === 0 && (
                    <TableRow>
                      <TableCell
                        colSpan={4}
                        className="text-center text-muted-foreground"
                      >
                        组内无路由
                      </TableCell>
                    </TableRow>
                  )}
                </TableBody>
              </Table>
            </div>
          </div>
        ) : null}
        <DialogFooter className="shrink-0">
          <Button onClick={() => onOpenChange(false)}>关闭</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

export function ModelSyncResultDialog({
  open,
  onOpenChange,
  modelSyncResult,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  modelSyncResult: GatewayModelSyncResult | null
}) {
  const failedRoutes = (modelSyncResult?.routes ?? []).filter(
    (r) => !r.ok && !r.skipped && (r.error || "").trim(),
  )

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex max-h-[min(90dvh,720px)] w-full max-w-lg flex-col gap-3 overflow-hidden sm:max-w-2xl">
        <DialogHeader className="shrink-0">
          <DialogTitle>模型同步结果</DialogTitle>
          <DialogDescription>
            失败渠道已跳过，不影响其它渠道。合并后共{" "}
            {modelSyncResult?.model_count ?? 0} 个模型（已去重，自定义项保留）。
          </DialogDescription>
        </DialogHeader>
        {modelSyncResult ? (
          <div className="min-h-0 flex-1 space-y-3 overflow-y-auto">
            <div className="flex flex-wrap gap-2 text-xs">
              <span className="rounded-md bg-emerald-100 px-2 py-1 text-emerald-800 dark:bg-emerald-900/40 dark:text-emerald-200">
                成功 {modelSyncResult.ok_count}
              </span>
              <span className="rounded-md bg-red-100 px-2 py-1 text-red-800 dark:bg-red-900/40 dark:text-red-200">
                失败 {modelSyncResult.fail_count}
              </span>
              <span className="rounded-md bg-muted px-2 py-1 text-muted-foreground">
                跳过 {modelSyncResult.skip_count}
              </span>
            </div>
            <div className="overflow-x-auto rounded-md border">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>来源</TableHead>
                    <TableHead>状态</TableHead>
                    <TableHead className="text-right">模型数</TableHead>
                    <TableHead>说明</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {(modelSyncResult.routes ?? []).map((r) => {
                    const note = r.error || r.skip_reason || (r.ok ? "已合并" : "—")
                    return (
                      <TableRow key={r.route_id}>
                        <TableCell className="text-sm">
                          <div className="font-medium">{r.label || `route#${r.route_id}`}</div>
                          <div className="text-[11px] text-muted-foreground">
                            {r.source_kind === "provider" ? "直连" : "监控"}
                            {r.route_id ? ` · #${r.route_id}` : ""}
                          </div>
                        </TableCell>
                        <TableCell>
                          {r.skipped ? (
                            <Badge variant="secondary">跳过</Badge>
                          ) : r.ok ? (
                            <Badge className="bg-emerald-600 hover:bg-emerald-600">成功</Badge>
                          ) : (
                            <Badge variant="destructive">失败</Badge>
                          )}
                        </TableCell>
                        <TableCell className="text-right tabular-nums text-sm">
                          {r.ok ? r.model_count : "—"}
                        </TableCell>
                        <TableCell className="max-w-[20rem] text-xs text-muted-foreground">
                          <span
                            className={cn(
                              "break-all",
                              !r.ok && !r.skipped ? "whitespace-pre-wrap text-red-700 dark:text-red-300" : "line-clamp-3",
                            )}
                            title={note}
                          >
                            {note}
                          </span>
                        </TableCell>
                      </TableRow>
                    )
                  })}
                  {(modelSyncResult.routes ?? []).length === 0 && (
                    <TableRow>
                      <TableCell colSpan={4} className="text-center text-muted-foreground">
                        组内无启用路由
                      </TableCell>
                    </TableRow>
                  )}
                </TableBody>
              </Table>
            </div>

            {failedRoutes.length > 0 ? (
              <div className="space-y-2">
                <div className="text-sm font-medium text-foreground">失败详情</div>
                {failedRoutes.map((r) => {
                  const detail = (r.error || "").trim()
                  const title = r.label || `route#${r.route_id}`
                  return (
                    <div
                      key={`fail-${r.route_id}`}
                      className="rounded-md border border-red-200 bg-red-50/70 p-3 dark:border-red-900/50 dark:bg-red-950/30"
                    >
                      <div className="mb-1.5 flex items-start justify-between gap-2">
                        <div className="min-w-0">
                          <div className="truncate text-sm font-medium text-foreground">{title}</div>
                          <div className="text-[11px] text-muted-foreground">
                            {r.source_kind === "provider" ? "直连" : "监控"}
                            {r.route_id ? ` · route #${r.route_id}` : ""}
                          </div>
                        </div>
                        <Button
                          type="button"
                          variant="outline"
                          size="sm"
                          className="h-7 shrink-0 gap-1 px-2 text-xs"
                          onClick={() => {
                            void copyText(detail)
                              .then(() => toast.success("已复制错误详情"))
                              .catch(() => toast.error("复制失败"))
                          }}
                        >
                          <Copy className="size-3.5" />
                          复制
                        </Button>
                      </div>
                      <pre className="max-h-40 overflow-auto whitespace-pre-wrap break-all rounded-md bg-background/80 p-2 font-mono text-[11px] leading-relaxed text-red-800 dark:text-red-200">
                        {detail}
                      </pre>
                    </div>
                  )
                })}
              </div>
            ) : null}
          </div>
        ) : null}
        <DialogFooter className="shrink-0">
          <Button onClick={() => onOpenChange(false)}>关闭</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

export function GroupFormDialog({
  open,
  onOpenChange,
  editingGroup,
  groupForm,
  setGroupForm,
  busy,
  onSubmit,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  editingGroup: GatewayGroup | null
  groupForm: GroupFormState
  setGroupForm: (form: GroupFormState) => void
  busy: boolean
  onSubmit: () => void
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex max-h-[min(90dvh,800px)] w-[calc(100vw-1.5rem)] max-w-lg min-w-0 flex-col gap-3 overflow-hidden sm:max-w-lg">
        <DialogHeader className="shrink-0">
          <DialogTitle>{editingGroup ? "编辑网关组" : "新建网关组"}</DialogTitle>
          <DialogDescription>
            组是配置单元：路由、密钥、重试与冷却策略归属组。
          </DialogDescription>
        </DialogHeader>
        <div className="min-h-0 flex-1 space-y-3 overflow-y-auto overscroll-contain pr-0.5">
          <div className="space-y-1">
            <Label>名称</Label>
            <Input
              value={groupForm.name}
              onChange={(e) => setGroupForm({ ...groupForm, name: e.target.value })}
              placeholder="例如 production"
            />
          </div>
          <div className="space-y-1">
            <Label>描述</Label>
            <Textarea
              value={groupForm.description}
              onChange={(e) => setGroupForm({ ...groupForm, description: e.target.value })}
              placeholder="可选备注"
              rows={2}
            />
          </div>
          {editingGroup && (
            <div className="flex items-center gap-2">
              <Switch
                checked={groupForm.status === "active"}
                onCheckedChange={(v) =>
                  setGroupForm({ ...groupForm, status: v ? "active" : "disabled" })
                }
              />
              <Label>启用组</Label>
            </div>
          )}

          <div className="flex items-center justify-between gap-2 rounded-lg border border-border bg-muted/20 p-3">
            <div className="min-w-0 flex-1">
              <Label>渠道分组价格倍率重排</Label>
              <p className="text-[11px] leading-5 text-muted-foreground">
                开启后，倍率扫描结束时按源分组实时倍率重写路由顺序与账号计费倍率。
              </p>
            </div>
            <Switch
              className="shrink-0"
              checked={groupForm.rate_resort_enabled}
              onCheckedChange={(v) =>
                setGroupForm({ ...groupForm, rate_resort_enabled: v })
              }
            />
          </div>

          <div className="space-y-1 rounded-lg border border-border bg-muted/20 p-3">
            <Label>User-Agent</Label>
            <Input
              value={groupForm.user_agent}
              onChange={(e) =>
                setGroupForm({ ...groupForm, user_agent: e.target.value })
              }
              placeholder="例如 Mozilla/5.0 …（可选）"
            />
            <p className="text-[11px] leading-5 text-muted-foreground">
              路由 User-Agent 选「网关组」时使用。留空表示未配置组级
              UA：转发时透传客户端；模型测试与拉取模型列表无客户端时可回落系统默认
              User-Agent（设置页上游 UA）。
            </p>
          </div>

          <div className="space-y-3 rounded-lg border border-border bg-muted/20 p-3">
            <div className="text-sm font-medium">重试与顺延</div>
            <div className="flex items-center justify-between gap-2">
              <div className="min-w-0 flex-1">
                <Label>开启重试</Label>
                <p className="text-[11px] leading-5 text-muted-foreground">
                  关闭后上游失败直接回显错误，不重试、不顺延。
                </p>
              </div>
              <Switch
                className="shrink-0"
                checked={groupForm.retry_enabled}
                onCheckedChange={(v) =>
                  setGroupForm({ ...groupForm, retry_enabled: v })
                }
              />
            </div>
            <div className="grid gap-3 sm:grid-cols-2">
              <div className="space-y-1">
                <Label>重试次数</Label>
                <Input
                  type="number"
                  min={0}
                  max={10}
                  disabled={!groupForm.retry_enabled}
                  value={groupForm.retry_count}
                  onChange={(e) =>
                    setGroupForm({ ...groupForm, retry_count: e.target.value })
                  }
                />
                <p className="text-[11px] text-muted-foreground">
                  同一路由额外尝试次数（不含首次）
                </p>
              </div>
              <div className="space-y-1">
                <Label>失败冷却（秒）</Label>
                <Input
                  type="number"
                  min={0}
                  max={86400}
                  disabled={!groupForm.retry_enabled}
                  value={groupForm.cooldown_seconds}
                  onChange={(e) =>
                    setGroupForm({ ...groupForm, cooldown_seconds: e.target.value })
                  }
                />
                <p className="text-[11px] text-muted-foreground">
                  路由失败后暂停调度时长，0 表示不冷却
                </p>
              </div>
            </div>
            <div className="flex items-center justify-between gap-2">
              <div className="min-w-0 flex-1">
                <Label>顺延下一个接口</Label>
                <p className="text-[11px] leading-5 text-muted-foreground">
                  当前路由重试耗尽后，按倍率顺序切换下一条路由。
                </p>
              </div>
              <Switch
                className="shrink-0"
                checked={groupForm.failover_enabled}
                disabled={!groupForm.retry_enabled}
                onCheckedChange={(v) =>
                  setGroupForm({ ...groupForm, failover_enabled: v })
                }
              />
            </div>
            <div className="space-y-1">
              <Label>顺延次数</Label>
              <Input
                type="number"
                min={0}
                max={32}
                disabled={!groupForm.retry_enabled || !groupForm.failover_enabled}
                value={groupForm.failover_max}
                onChange={(e) =>
                  setGroupForm({ ...groupForm, failover_max: e.target.value })
                }
              />
              <p className="text-[11px] text-muted-foreground">
                首条之后最多再换几条路由（例如 8 表示最多共 9 条）
              </p>
            </div>
            <div className="flex items-center justify-between gap-2">
              <div className="min-w-0 flex-1">
                <Label>4xx 状态码顺延</Label>
                <p className="text-[11px] leading-5 text-muted-foreground">
                  默认仅网络错误、429、5xx 会重试/顺延。开启后 400/401/403/404
                  等 4xx 也按上方策略重试与顺延。
                </p>
              </div>
              <Switch
                className="shrink-0"
                checked={groupForm.failover_on_4xx}
                disabled={!groupForm.retry_enabled}
                onCheckedChange={(v) =>
                  setGroupForm({ ...groupForm, failover_on_4xx: v })
                }
              />
            </div>
            <p className="rounded-md border border-amber-200/80 bg-amber-50/80 px-2 py-1.5 text-[11px] leading-5 text-amber-900 dark:border-amber-900/50 dark:bg-amber-950/30 dark:text-amber-200">
              注意：顺延到其他渠道时，上游 prompt cache 通常无法命中，可能按全量输入重新计费；
              次数越多、触发越频繁，
              <strong>额外费用风险越高</strong>
              。4xx 顺延还可能把错误请求打到更多上游。
            </p>
            <div className="space-y-1 border-t border-border/60 pt-3">
              <Label>首字超时（秒）</Label>
              <Input
                type="number"
                min={0}
                max={300}
                step={1}
                value={groupForm.first_token_timeout_sec}
                onChange={(e) =>
                  setGroupForm({
                    ...groupForm,
                    first_token_timeout_sec: e.target.value,
                  })
                }
                placeholder="0"
              />
              <div className="space-y-1 text-[11px] leading-5 text-muted-foreground">
                <p>
                  <strong className="text-foreground/80">0</strong>
                  ＝关闭（默认）。填写
                  <strong className="text-foreground/80"> ≥1 </strong>
                  时：每条渠道各自独立计时，从<strong>发起上游请求起</strong>
                  到收到<strong>有效首字节/首字</strong>（含等响应头；SSE 注释/ping
                  不算）超过该秒数则主动断开，并按上方策略顺延下一条。
                </p>
                <p>
                  与用量里的「首字」耗时同一时钟。仅在「失败后还能换到其它渠道」时生效；
                  <strong className="text-foreground/80">本请求最后一条可试渠道不会套用首字超时</strong>
                  ，会老实等到上游正常响应或转发总超时。
                </p>
                <p className="rounded-md border border-amber-200/80 bg-amber-50/80 px-2 py-1.5 text-amber-900 dark:border-amber-900/50 dark:bg-amber-950/30 dark:text-amber-200">
                  注意：中间渠道超时断开后仍会顺延，上游可能已对中断请求计费，
                  <strong>可能造成重复请求与费用增加</strong>
                  。请仅在能接受额外成本时开启。
                </p>
              </div>
            </div>
          </div>
        </div>
        <DialogFooter className="shrink-0">
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <Button onClick={() => void onSubmit()} disabled={busy}>
            保存
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

export function KeyFormDialog({
  open,
  onOpenChange,
  editingKey,
  keyForm,
  setKeyForm,
  busy,
  onSubmit,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  editingKey: GatewayKey | null
  keyForm: KeyFormState
  setKeyForm: (form: KeyFormState) => void
  busy: boolean
  onSubmit: () => void
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="w-[calc(100vw-1.5rem)] max-w-lg min-w-0">
        <DialogHeader>
          <DialogTitle>{editingKey ? "编辑密钥" : "创建密钥"}</DialogTitle>
          <DialogDescription>
            配额为 0 表示不限制。IP 名单每行一个 IP 或 CIDR，保存为 JSON 数组。
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-3">
          <div className="space-y-1">
            <Label>名称</Label>
            <Input
              value={keyForm.name}
              onChange={(e) => setKeyForm({ ...keyForm, name: e.target.value })}
            />
          </div>
          <div className="space-y-1">
            <Label>配额（USD，0=无限）</Label>
            <Input
              type="number"
              step="0.01"
              value={keyForm.quota}
              onChange={(e) => setKeyForm({ ...keyForm, quota: e.target.value })}
            />
          </div>
          {editingKey && (
            <>
              <div className="flex items-center gap-2">
                <Switch
                  checked={keyForm.status === "active"}
                  onCheckedChange={(v) =>
                    setKeyForm({ ...keyForm, status: v ? "active" : "disabled" })
                  }
                />
                <Label>启用</Label>
              </div>
              <div className="flex items-center gap-2">
                <Checkbox
                  id="reset-quota"
                  checked={keyForm.reset_quota_used}
                  onCheckedChange={(v) =>
                    setKeyForm({ ...keyForm, reset_quota_used: v === true })
                  }
                />
                <Label htmlFor="reset-quota">重置已用配额</Label>
              </div>
            </>
          )}
          {!editingKey && (
            <div className="space-y-3">
              <div className="flex items-start gap-2.5">
                <Checkbox
                  id="use-custom-key"
                  className="mt-0.5"
                  checked={keyForm.use_custom_key}
                  onCheckedChange={(v) => {
                    const useCustom = v === true
                    setKeyForm({
                      ...keyForm,
                      use_custom_key: useCustom,
                      custom_key: useCustom ? keyForm.custom_key : "",
                    })
                  }}
                />
                <div className="min-w-0 space-y-0.5">
                  <Label htmlFor="use-custom-key" className="cursor-pointer">
                    自定义密钥
                  </Label>
                  <p className="text-[11px] leading-4 text-muted-foreground">
                    {keyForm.use_custom_key
                      ? "使用下方完整密钥字符串"
                      : "未勾选时按所选长度自动生成 sk-…"}
                  </p>
                </div>
              </div>

              {keyForm.use_custom_key ? (
                <div className="space-y-1.5 pl-6">
                  <Label htmlFor="custom-key-input" className="text-xs text-muted-foreground">
                    密钥内容
                  </Label>
                  <Input
                    id="custom-key-input"
                    value={keyForm.custom_key}
                    onChange={(e) =>
                      setKeyForm({ ...keyForm, custom_key: e.target.value })
                    }
                    placeholder="必填，例如 sk-xxxxxxxx"
                    className="h-9 font-mono text-sm"
                    spellCheck={false}
                    autoComplete="off"
                  />
                </div>
              ) : (
                <div className="space-y-1.5 pl-6">
                  <div className="flex flex-wrap items-baseline justify-between gap-x-2 gap-y-0.5">
                    <Label className="text-xs text-muted-foreground">
                      自动生成长度
                    </Label>
                    <span className="font-mono text-[11px] tabular-nums text-muted-foreground">
                      sk- + {keyForm.key_len} · 总长 {3 + keyForm.key_len}
                    </span>
                  </div>
                  <div
                    role="radiogroup"
                    aria-label="自动生成长度"
                    className="flex flex-wrap gap-1.5"
                  >
                    {API_KEY_LENS.map((n) => {
                      const active = keyForm.key_len === n
                      return (
                        <button
                          key={n}
                          type="button"
                          role="radio"
                          aria-checked={active}
                          title={`sk- + ${n} 字符，总长 ${3 + n}`}
                          onClick={() =>
                            setKeyForm({
                              ...keyForm,
                              key_len: n as APIKeyLen,
                            })
                          }
                          className={cn(
                            "inline-flex h-8 min-w-11 items-center justify-center rounded-md border px-2.5 text-sm font-medium tabular-nums transition-colors",
                            "outline-none focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50",
                            active
                              ? "border-primary bg-primary text-primary-foreground shadow-xs"
                              : "border-border bg-background text-foreground hover:bg-muted",
                          )}
                        >
                          {n}
                        </button>
                      )
                    })}
                  </div>
                </div>
              )}
            </div>
          )}
          <div className="space-y-1">
            <Label>IP 白名单</Label>
            <Textarea
              rows={2}
              value={keyForm.ip_whitelist}
              onChange={(e) => setKeyForm({ ...keyForm, ip_whitelist: e.target.value })}
              placeholder="每行一个 IP 或 CIDR；空=不限制"
            />
          </div>
          <div className="space-y-1">
            <Label>IP 黑名单</Label>
            <Textarea
              rows={2}
              value={keyForm.ip_blacklist}
              onChange={(e) => setKeyForm({ ...keyForm, ip_blacklist: e.target.value })}
              placeholder="每行一个 IP 或 CIDR"
            />
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <Button onClick={() => void onSubmit()} disabled={busy}>
            保存
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

export function DefaultsPriceDialog({
  open,
  onOpenChange,
  defaultsQuery,
  setDefaultsQuery,
  defaultPrices,
  defaultsLoading,
  onSearch,
  onLoadIfEmpty,
  onApplyDefault,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  defaultsQuery: string
  setDefaultsQuery: (v: string) => void
  defaultPrices: ModelDefaultPrice[]
  defaultsLoading: boolean
  onSearch: () => void
  onLoadIfEmpty: () => void
  onApplyDefault: (p: ModelDefaultPrice) => void
}) {
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        onOpenChange(next)
        if (next) onLoadIfEmpty()
      }}
    >
      <DialogContent className="flex max-h-[min(85dvh,720px)] w-full max-w-5xl flex-col gap-3 overflow-hidden sm:max-w-5xl">
        <DialogHeader className="shrink-0">
          <DialogTitle>系统默认价</DialogTitle>
          <DialogDescription>
            内置价目表（LiteLLM + 硬编码回退，只读）。可将一行「用作覆盖」填入表单后保存。
            {!defaultsLoading && defaultPrices.length > 0
              ? ` 当前 ${defaultPrices.length} 条。`
              : null}
          </DialogDescription>
        </DialogHeader>
        <div className="flex shrink-0 gap-2">
          <Input
            placeholder="搜索模型名，如 grok / claude"
            value={defaultsQuery}
            onChange={(e) => setDefaultsQuery(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && void onSearch()}
          />
          <Button
            variant="outline"
            onClick={() => void onSearch()}
            disabled={defaultsLoading}
          >
            <Search className="size-3.5" /> 搜索
          </Button>
        </div>
        <div className="min-h-[240px] max-h-[min(55dvh,480px)] shrink-0 overflow-auto rounded-md border">
          {defaultsLoading ? (
            <div className="flex h-40 items-center justify-center text-sm text-muted-foreground">
              加载中…
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>模型</TableHead>
                  <TableHead>In $/MTok</TableHead>
                  <TableHead>Out $/MTok</TableHead>
                  <TableHead>Cache C/R</TableHead>
                  <TableHead />
                </TableRow>
              </TableHeader>
              <TableBody>
                {defaultPrices.map((p) => (
                  <TableRow key={p.model_name}>
                    <TableCell className="text-xs font-medium">{p.model_name}</TableCell>
                    <TableCell className="text-xs tabular-nums">
                      {perTokenToMTok(p.input_price_per_token)}
                    </TableCell>
                    <TableCell className="text-xs tabular-nums">
                      {perTokenToMTok(p.output_price_per_token)}
                    </TableCell>
                    <TableCell className="text-xs tabular-nums">
                      {perTokenToMTok(p.cache_creation_price_per_token)} /{" "}
                      {perTokenToMTok(p.cache_read_price_per_token)}
                    </TableCell>
                    <TableCell>
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() => onApplyDefault(p)}
                      >
                        用作覆盖
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
                {defaultPrices.length === 0 && (
                  <TableRow>
                    <TableCell colSpan={5} className="text-center text-muted-foreground">
                      无匹配模型（可清空搜索后重试）
                    </TableCell>
                  </TableRow>
                )}
              </TableBody>
            </Table>
          )}
        </div>
        <DialogFooter className="shrink-0">
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            关闭
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

export function CreatedSecretDialog({
  createdSecret,
  onClose,
}: {
  createdSecret: string | null
  onClose: () => void
}) {
  return (
    <Dialog open={!!createdSecret} onOpenChange={() => onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>网关密钥</DialogTitle>
          <DialogDescription>请妥善保存。关闭后仍可通过「查看」再次获取。</DialogDescription>
        </DialogHeader>
        <div className="flex items-center gap-2">
          <code className="flex-1 break-all rounded bg-muted p-2 text-sm select-all">
            {createdSecret}
          </code>
          <Button
            size="icon"
            variant="outline"
            title="复制密钥"
            onClick={() => {
              if (!createdSecret) return
              void copyText(createdSecret)
                .then(() => toast.success("已复制"))
                .catch(() => toast.error("复制失败，请手动选中密钥复制"))
            }}
          >
            <Copy className="size-4" />
          </Button>
        </div>
        <DialogFooter>
          <Button onClick={onClose}>关闭</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
