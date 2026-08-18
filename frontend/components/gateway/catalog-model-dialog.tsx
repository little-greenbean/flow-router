import { useEffect, useMemo, useState } from "react"
import { Check, DatabaseZap, Loader2, Minus, Search } from "lucide-react"
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
import type { GatewayCatalogModel, GatewayRoute } from "@/lib/api-types"
import {
  catalogVendorSelection,
  catalogVendors,
  filterCatalogModels,
  toggleCatalogVendor,
} from "./catalog-filter"
import { channelGroupLabel, routeSourceKind } from "./gateway-utils"

type CatalogModelDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  models: GatewayCatalogModel[]
  routes: Partial<GatewayRoute>[]
  channelNameByID: Map<number, string>
  providerNameByID: Map<number, string>
  loading: boolean
  applying: boolean
  onApply: (modelIDs: string[], routeIDs: number[]) => void
}

export function CatalogModelDialog({
  open,
  onOpenChange,
  models,
  routes,
  channelNameByID,
  providerNameByID,
  loading,
  applying,
  onApply,
}: CatalogModelDialogProps) {
  const [query, setQuery] = useState("")
  const [selectedModels, setSelectedModels] = useState<Set<string>>(new Set())
  const [selectedRoutes, setSelectedRoutes] = useState<Set<number>>(new Set())

  useEffect(() => {
    if (!open) return
    setQuery("")
    setSelectedModels(new Set())
    setSelectedRoutes(new Set())
  }, [open])

  const vendors = useMemo(() => catalogVendors(models), [models])

  const filteredModels = useMemo(() => {
    return filterCatalogModels(models, query)
  }, [models, query])

  const enabledRoutes = useMemo(
    () => routes.filter((route) => route.id && route.enabled !== false),
    [routes],
  )

  function toggleModel(id: string, checked: boolean) {
    setSelectedModels((current) => {
      const next = new Set(current)
      if (checked) next.add(id)
      else next.delete(id)
      return next
    })
  }

  function toggleRoute(id: number, checked: boolean) {
    setSelectedRoutes((current) => {
      const next = new Set(current)
      if (checked) next.add(id)
      else next.delete(id)
      return next
    })
  }

  function routeLabel(route: Partial<GatewayRoute>) {
    if (routeSourceKind(route) === "provider") {
      const providerID = Number(route.gateway_provider_id) || 0
      return providerNameByID.get(providerID) || `直连 #${providerID}`
    }
    const channelID = Number(route.source_channel_id) || 0
    return channelGroupLabel(
      channelNameByID.get(channelID),
      route.source_group_name,
      channelID,
    )
  }

  const canApply = selectedModels.size > 0 && selectedRoutes.size > 0 && !applying

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex max-h-[min(88dvh,820px)] flex-col gap-3 overflow-hidden sm:max-w-5xl">
        <DialogHeader className="shrink-0">
          <DialogTitle className="flex items-center gap-2">
            <DatabaseZap className="size-4" />
            同步官方模型目录
          </DialogTitle>
          <DialogDescription>
            目录模型仅写入当前网关组；可用路由以本次绑定为准。
          </DialogDescription>
        </DialogHeader>

        <div className="grid min-h-0 flex-1 gap-3 md:grid-cols-[minmax(0,1.35fr)_minmax(16rem,0.65fr)]">
          <section className="flex min-h-[18rem] min-w-0 flex-col overflow-hidden rounded-md border">
            <div className="shrink-0 space-y-2 border-b p-2.5">
              <div className="flex items-center gap-2">
                <div className="relative min-w-0 flex-1">
                  <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
                  <Input
                    className="h-8 pl-8"
                    placeholder="搜索模型、厂商或标签"
                    value={query}
                    onChange={(event) => setQuery(event.target.value)}
                  />
                </div>
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={() => {
                    setSelectedModels((current) => {
                      const next = new Set(current)
                      for (const model of filteredModels) next.add(model.model_name)
                      return next
                    })
                  }}
                  disabled={filteredModels.length === 0}
                >
                  全选当前
                </Button>
                <Button size="sm" variant="ghost" onClick={() => setSelectedModels(new Set())}>
                  清空
                </Button>
              </div>
              {vendors.length > 0 ? (
                <div className="flex max-h-20 flex-wrap gap-1.5 overflow-y-auto" aria-label="厂商筛选">
                  {(() => {
                    const selection = catalogVendorSelection(models, selectedModels)
                    return (
                      <Button
                        type="button"
                        size="sm"
                        variant={selection.state === "all" ? "default" : "outline"}
                        className={`h-7 px-2.5 text-xs ${
                          selection.state === "partial"
                            ? "border-primary bg-primary/10 text-primary hover:bg-primary/15"
                            : ""
                        }`}
                        aria-pressed={selection.state === "all"}
                        data-selection-state={selection.state}
                        onClick={() =>
                          setSelectedModels((current) =>
                            toggleCatalogVendor(models, current),
                          )
                        }
                      >
                        {selection.state === "all" ? (
                          <Check className="size-3.5" />
                        ) : selection.state === "partial" ? (
                          <Minus className="size-3.5" />
                        ) : null}
                        全部
                        <span className="text-[10px] opacity-70">
                          {selection.selectedCount > 0
                            ? `${selection.selectedCount}/${selection.totalCount}`
                            : selection.totalCount}
                        </span>
                      </Button>
                    )
                  })()}
                  {vendors.map((vendor) => {
                    const selection = catalogVendorSelection(
                      models,
                      selectedModels,
                      vendor.key,
                    )
                    return (
                      <Button
                        key={vendor.key}
                        type="button"
                        size="sm"
                        variant={selection.state === "all" ? "default" : "outline"}
                        className={`h-7 px-2.5 text-xs ${
                          selection.state === "partial"
                            ? "border-primary bg-primary/10 text-primary hover:bg-primary/15"
                            : ""
                        }`}
                        aria-pressed={selection.state === "all"}
                        data-selection-state={selection.state}
                        onClick={() =>
                          setSelectedModels((current) =>
                            toggleCatalogVendor(models, current, vendor.key),
                          )
                        }
                      >
                        {selection.state === "all" ? (
                          <Check className="size-3.5" />
                        ) : selection.state === "partial" ? (
                          <Minus className="size-3.5" />
                        ) : null}
                        {vendor.label}
                        <span className="text-[10px] opacity-70">
                          {selection.selectedCount > 0
                            ? `${selection.selectedCount}/${selection.totalCount}`
                            : vendor.count}
                        </span>
                      </Button>
                    )
                  })}
                </div>
              ) : null}
            </div>
            <div className="min-h-0 flex-1 overflow-y-auto">
              {loading ? (
                <div className="flex h-full min-h-48 items-center justify-center text-sm text-muted-foreground">
                  <Loader2 className="mr-2 size-4 animate-spin" /> 加载目录
                </div>
              ) : filteredModels.length === 0 ? (
                <div className="flex h-full min-h-48 items-center justify-center text-sm text-muted-foreground">
                  没有匹配模型
                </div>
              ) : (
                <ul className="divide-y">
                  {filteredModels.map((model) => {
                    const checked = selectedModels.has(model.model_name)
                    return (
                      <li key={model.model_name}>
                        <label className="flex cursor-pointer items-start gap-3 px-3 py-2.5 hover:bg-muted/40">
                          <Checkbox
                            className="mt-0.5"
                            checked={checked}
                            onCheckedChange={(value) => toggleModel(model.model_name, value === true)}
                          />
                          <span className="min-w-0 flex-1">
                            <span className="flex min-w-0 flex-wrap items-center gap-1.5">
                              <code className="break-all text-sm font-medium">{model.model_name}</code>
                              {model.vendor_name ? (
                                <Badge variant="outline" className="h-5 px-1.5 text-[10px] font-normal">
                                  {model.vendor_name}
                                </Badge>
                              ) : null}
                              {model.tags ? (
                                <span className="text-[11px] text-muted-foreground">{model.tags}</span>
                              ) : null}
                            </span>
                            {model.description ? (
                              <span className="mt-0.5 block line-clamp-2 text-xs leading-5 text-muted-foreground">
                                {model.description}
                              </span>
                            ) : null}
                          </span>
                        </label>
                      </li>
                    )
                  })}
                </ul>
              )}
            </div>
          </section>

          <section className="flex min-h-[14rem] min-w-0 flex-col overflow-hidden rounded-md border">
            <div className="flex shrink-0 items-center justify-between gap-2 border-b px-3 py-2.5">
              <div className="text-sm font-medium">绑定路由</div>
              <div className="flex items-center gap-1">
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={() =>
                    setSelectedRoutes(new Set(enabledRoutes.map((route) => Number(route.id))))
                  }
                  disabled={enabledRoutes.length === 0}
                >
                  全选
                </Button>
                <Button size="sm" variant="ghost" onClick={() => setSelectedRoutes(new Set())}>
                  清空
                </Button>
              </div>
            </div>
            <div className="min-h-0 flex-1 overflow-y-auto">
              {enabledRoutes.length === 0 ? (
                <div className="flex h-full min-h-36 items-center justify-center px-4 text-center text-sm text-muted-foreground">
                  当前组没有启用路由
                </div>
              ) : (
                <ul className="divide-y">
                  {enabledRoutes.map((route) => {
                    const routeID = Number(route.id)
                    const checked = selectedRoutes.has(routeID)
                    return (
                      <li key={routeID}>
                        <label className="flex cursor-pointer items-start gap-2.5 px-3 py-2.5 hover:bg-muted/40">
                          <Checkbox
                            className="mt-0.5"
                            checked={checked}
                            onCheckedChange={(value) => toggleRoute(routeID, value === true)}
                          />
                          <span className="min-w-0 flex-1">
                            <span className="block break-words text-sm font-medium">{routeLabel(route)}</span>
                            <span className="mt-0.5 block text-[11px] text-muted-foreground">
                              route {routeID} · {routeSourceKind(route) === "provider" ? "直连" : "监控渠道"}
                            </span>
                          </span>
                          {checked ? <Check className="mt-0.5 size-3.5 text-primary" /> : null}
                        </label>
                      </li>
                    )
                  })}
                </ul>
              )}
            </div>
          </section>
        </div>

        <DialogFooter className="shrink-0 items-center sm:justify-between">
          <div className="text-xs text-muted-foreground">
            已选 {selectedModels.size} 个模型 · {selectedRoutes.size} 条路由
          </div>
          <div className="flex gap-2">
            <Button variant="outline" onClick={() => onOpenChange(false)} disabled={applying}>
              取消
            </Button>
            <Button
              disabled={!canApply}
              onClick={() => onApply([...selectedModels], [...selectedRoutes])}
            >
              {applying ? <Loader2 className="size-4 animate-spin" /> : <DatabaseZap className="size-4" />}
              应用到当前组
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
