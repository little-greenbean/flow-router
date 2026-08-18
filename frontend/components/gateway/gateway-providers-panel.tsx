import { useCallback, useEffect, useState } from "react"
import { toast } from "sonner"
import { Loader2, Pencil, Plus, RefreshCw, Search, Trash2 } from "lucide-react"
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
import { Switch } from "@/components/ui/switch"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Textarea } from "@/components/ui/textarea"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { apiFetch } from "@/lib/api"
import type {
  GatewayProvider,
  GatewayProviderPage,
  GatewayUpstreamProtocol,
} from "@/lib/api-types"
import { cn } from "@/lib/utils"

type ProviderForm = {
  name: string
  base_url: string
  api_key: string
  upstream_protocol: GatewayUpstreamProtocol
  default_billing_rate: string
  auth_style: string
  enabled: boolean
  proxy_enabled: boolean
  notes: string
}

const emptyForm = (): ProviderForm => ({
  name: "",
  base_url: "",
  api_key: "",
  upstream_protocol: "auto",
  default_billing_rate: "1",
  auth_style: "both",
  enabled: true,
  proxy_enabled: false,
  notes: "",
})

export function GatewayProvidersPanel() {
  const { confirm, dialog: confirmDialog } = useConfirm()
  const [page, setPage] = useState<GatewayProviderPage | null>(null)
  const [loading, setLoading] = useState(false)
  const [busy, setBusy] = useState(false)
  const [q, setQ] = useState("")
  const [pageNum, setPageNum] = useState(1)
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editing, setEditing] = useState<GatewayProvider | null>(null)
  const [form, setForm] = useState<ProviderForm>(emptyForm())

  const load = useCallback(
    async (p = pageNum, query = q) => {
      setLoading(true)
      try {
        const qs = new URLSearchParams({
          page: String(p),
          page_size: "20",
        })
        if (query.trim()) qs.set("q", query.trim())
        const res = await apiFetch<GatewayProviderPage>(`/gateway/providers?${qs}`)
        setPage(res)
        setPageNum(res.page || p)
      } catch (e) {
        toast.error(e instanceof Error ? e.message : "加载直连渠道失败")
      } finally {
        setLoading(false)
      }
    },
    [pageNum, q],
  )

  useEffect(() => {
    void load(1, "")
  }, [])

  function openCreate() {
    setEditing(null)
    setForm(emptyForm())
    setDialogOpen(true)
  }

  function openEdit(item: GatewayProvider) {
    setEditing(item)
    setForm({
      name: item.name,
      base_url: item.base_url,
      api_key: "",
      upstream_protocol: item.upstream_protocol || "auto",
      default_billing_rate: String(item.default_billing_rate ?? 1),
      auth_style: item.auth_style || "both",
      enabled: item.enabled !== false,
      proxy_enabled: !!item.proxy_enabled,
      notes: item.notes || "",
    })
    setDialogOpen(true)
  }

  async function save() {
    const name = form.name.trim()
    const base = form.base_url.trim().replace(/\/+$/, "")
    if (!name) {
      toast.error("请填写名称")
      return
    }
    if (!base) {
      toast.error("请填写 Base URL")
      return
    }
    if (!editing && !form.api_key.trim()) {
      toast.error("请填写 API Key")
      return
    }
    const rate = Number(form.default_billing_rate)
    if (!Number.isFinite(rate) || rate <= 0) {
      toast.error("计费倍率须为正数")
      return
    }
    setBusy(true)
    try {
      if (editing) {
        const body: Record<string, unknown> = {
          name,
          base_url: base,
          upstream_protocol: form.upstream_protocol,
          default_billing_rate: rate,
          auth_style: form.auth_style,
          enabled: form.enabled,
          proxy_enabled: form.proxy_enabled,
          notes: form.notes.trim(),
        }
        if (form.api_key.trim()) body.api_key = form.api_key.trim()
        await apiFetch(`/gateway/providers/${editing.id}`, {
          method: "PUT",
          body: JSON.stringify(body),
        })
        toast.success("已更新直连渠道")
      } else {
        await apiFetch("/gateway/providers", {
          method: "POST",
          body: JSON.stringify({
            name,
            base_url: base,
            api_key: form.api_key.trim(),
            upstream_protocol: form.upstream_protocol,
            default_billing_rate: rate,
            auth_style: form.auth_style,
            enabled: form.enabled,
            proxy_enabled: form.proxy_enabled,
            notes: form.notes.trim(),
          }),
        })
        toast.success("已创建直连渠道")
      }
      setDialogOpen(false)
      await load(editing ? pageNum : 1, q)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "保存失败")
    } finally {
      setBusy(false)
    }
  }

  async function remove(item: GatewayProvider) {
    const ok = await confirm({
      title: "删除直连渠道",
      description: `确定删除「${item.name}」？已引用该渠道的网关路由将无法转发。`,
      confirmLabel: "删除",
      destructive: true,
    })
    if (!ok) return
    setBusy(true)
    try {
      await apiFetch(`/gateway/providers/${item.id}`, { method: "DELETE" })
      toast.success("已删除")
      await load(pageNum, q)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "删除失败")
    } finally {
      setBusy(false)
    }
  }

  const items = page?.items ?? []

  return (
    <div className="space-y-4">
      {confirmDialog}
      <Card className="overflow-hidden border-border shadow-none">
        <CardContent className="space-y-3 p-4 sm:p-5">
          <div className="flex flex-wrap items-end justify-between gap-2">
            <div className="min-w-0 flex-1 space-y-1">
              <p className="text-sm leading-6 text-muted-foreground">
                配置第三方上游的访问地址与密钥，供网关组路由调用；可与监控渠道混用，无需「确保上游密钥」。
              </p>
            </div>
            <div className="flex flex-wrap items-center gap-2">
              <div className="flex items-center gap-1">
                <Input
                  className="w-44"
                  placeholder="搜索名称 / URL"
                  value={q}
                  onChange={(e) => setQ(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === "Enter") {
                      setPageNum(1)
                      void load(1, q)
                    }
                  }}
                />
                <Button
                  size="sm"
                  variant="outline"
                  disabled={loading}
                  onClick={() => {
                    setPageNum(1)
                    void load(1, q)
                  }}
                >
                  <Search className="size-3.5" />
                </Button>
              </div>
              <Button
                size="sm"
                variant="outline"
                disabled={loading}
                onClick={() => void load(pageNum, q)}
              >
                <RefreshCw className={cn("size-3.5", loading && "animate-spin")} />
              </Button>
              <Button size="sm" onClick={openCreate}>
                <Plus className="size-3.5" /> 新建
              </Button>
            </div>
          </div>

          <div className="overflow-x-auto rounded-md border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>名称</TableHead>
                  <TableHead>Base URL</TableHead>
                  <TableHead>协议</TableHead>
                  <TableHead>默认倍率</TableHead>
                  <TableHead>Key</TableHead>
                  <TableHead>代理</TableHead>
                  <TableHead>状态</TableHead>
                  <TableHead className="text-right">操作</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {loading && items.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={8} className="h-24 text-center text-muted-foreground">
                      <Loader2 className="mx-auto size-4 animate-spin" />
                    </TableCell>
                  </TableRow>
                ) : items.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={8} className="h-24 text-center text-muted-foreground">
                      暂无直连渠道，点击右上角「新建」添加
                    </TableCell>
                  </TableRow>
                ) : (
                  items.map((p) => (
                    <TableRow key={p.id}>
                      <TableCell className="font-medium whitespace-nowrap">{p.name}</TableCell>
                      <TableCell className="max-w-[16rem] truncate font-mono text-xs" title={p.base_url}>
                        {p.base_url}
                      </TableCell>
                      <TableCell className="text-sm">
                        {p.upstream_protocol === "openai_responses"
                          ? "Responses"
                          : p.upstream_protocol === "openai_chat" ||
                              p.upstream_protocol === "openai"
                            ? "Chat"
                            : p.upstream_protocol === "anthropic"
                              ? "Anthropic"
                              : "自动"}
                      </TableCell>
                      <TableCell className="tabular-nums text-sm">
                        {p.default_billing_rate}
                      </TableCell>
                      <TableCell className="font-mono text-xs text-muted-foreground">
                        {p.api_key_hint || "—"}
                      </TableCell>
                      <TableCell>
                        {p.proxy_enabled ? (
                          <Badge variant="outline">代理</Badge>
                        ) : (
                          <span className="text-xs text-muted-foreground">—</span>
                        )}
                      </TableCell>
                      <TableCell>
                        {p.enabled ? (
                          <Badge variant="default">启用</Badge>
                        ) : (
                          <Badge variant="secondary">禁用</Badge>
                        )}
                      </TableCell>
                      <TableCell className="text-right">
                        <div className="flex justify-end gap-1">
                          <Button size="sm" variant="outline" onClick={() => openEdit(p)}>
                            <Pencil className="size-3.5" />
                          </Button>
                          <Button
                            size="sm"
                            variant="outline"
                            disabled={busy}
                            onClick={() => void remove(p)}
                          >
                            <Trash2 className="size-3.5" />
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          </div>

          {page && page.pages > 1 && (
            <div className="flex items-center justify-between text-sm">
              <span className="text-muted-foreground">
                第 {page.page}/{page.pages} 页 · 共 {page.total} 条
              </span>
              <div className="flex gap-2">
                <Button
                  size="sm"
                  variant="outline"
                  disabled={page.page <= 1 || loading}
                  onClick={() => {
                    const p = page.page - 1
                    setPageNum(p)
                    void load(p, q)
                  }}
                >
                  上一页
                </Button>
                <Button
                  size="sm"
                  variant="outline"
                  disabled={page.page >= page.pages || loading}
                  onClick={() => {
                    const p = page.page + 1
                    setPageNum(p)
                    void load(p, q)
                  }}
                >
                  下一页
                </Button>
              </div>
            </div>
          )}
        </CardContent>
      </Card>

      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>{editing ? "编辑直连渠道" : "新建直连渠道"}</DialogTitle>
            <DialogDescription>
              填写上游地址与 API Key 即可转发；在「网关 → 路由」中选择本渠道后生效。
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-3">
            <div className="space-y-1">
              <Label>名称</Label>
              <Input
                value={form.name}
                onChange={(e) => setForm({ ...form, name: e.target.value })}
                placeholder="例如 openrouter"
              />
            </div>
            <div className="space-y-1">
              <Label>Base URL</Label>
              <Input
                value={form.base_url}
                onChange={(e) => setForm({ ...form, base_url: e.target.value })}
                placeholder="https://api.example.com 或 …/v1"
              />
              <p className="text-[11px] leading-5 text-muted-foreground">
                与上游文档中的根地址一致；请求会再拼路径（如
                <code className="mx-0.5 rounded bg-muted px-1">/v1/chat/completions</code>
                ）。若根地址已含
                <code className="mx-0.5 rounded bg-muted px-1">/v1</code>
                ，勿重复填写，以免变成
                <code className="mx-0.5 rounded bg-muted px-1">/v1/v1/…</code>
                。
              </p>
            </div>
            <div className="space-y-1">
              <Label>API Key{editing ? "（留空则不修改）" : ""}</Label>
              <Input
                type="password"
                autoComplete="off"
                value={form.api_key}
                onChange={(e) => setForm({ ...form, api_key: e.target.value })}
                placeholder={editing ? `当前 ${editing.api_key_hint || "••••"}` : "sk-…"}
              />
            </div>
            <div className="grid gap-3 sm:grid-cols-2">
              <div className="space-y-1 min-w-0">
                <Label>上游协议</Label>
                <Select
                  value={form.upstream_protocol}
                  onValueChange={(v) =>
                    setForm({ ...form, upstream_protocol: v as GatewayUpstreamProtocol })
                  }
                >
                  <SelectTrigger className="w-full min-w-0">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent className="min-w-[16rem]">
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
              </div>
              <div className="space-y-1">
                <Label>默认计费倍率</Label>
                <Input
                  type="number"
                  step="0.01"
                  value={form.default_billing_rate}
                  onChange={(e) =>
                    setForm({ ...form, default_billing_rate: e.target.value })
                  }
                />
                <p className="text-[11px] text-muted-foreground">
                  路由未单独设置时使用此倍率
                </p>
              </div>
              <div className="space-y-1">
                <Label>鉴权方式</Label>
                <Select
                  value={form.auth_style}
                  onValueChange={(v) => setForm({ ...form, auth_style: v })}
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="both">Bearer 与 x-api-key</SelectItem>
                    <SelectItem value="bearer">仅 Bearer</SelectItem>
                    <SelectItem value="x-api-key">仅 x-api-key</SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <div className="flex items-end gap-2 pb-1">
                <Switch
                  checked={form.enabled}
                  onCheckedChange={(v) => setForm({ ...form, enabled: v })}
                />
                <Label>启用</Label>
              </div>
            </div>
            <div className="flex items-center justify-between rounded-lg border border-border px-3 py-2">
              <div>
                <p className="text-sm font-medium">启用代理 IP</p>
                <p className="text-xs text-muted-foreground">
                  全局代理启用后，该直连渠道转发走系统代理配置（与监控渠道一致）
                </p>
              </div>
              <Switch
                checked={form.proxy_enabled}
                onCheckedChange={(v) => setForm({ ...form, proxy_enabled: v })}
              />
            </div>
            <p className="text-[11px] leading-5 text-muted-foreground">
              User-Agent 在网关组与路由上配置（透传 / 组 / 自定义），不再按直连渠道单独设置。
            </p>
            <div className="space-y-1">
              <Label>备注</Label>
              <Textarea
                rows={2}
                value={form.notes}
                onChange={(e) => setForm({ ...form, notes: e.target.value })}
                placeholder="可选"
              />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDialogOpen(false)}>
              取消
            </Button>
            <Button disabled={busy} onClick={() => void save()}>
              {busy ? <Loader2 className="size-3.5 animate-spin" /> : null}
              保存
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
