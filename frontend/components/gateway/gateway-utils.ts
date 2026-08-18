import type {
  GatewayModelListItem,
  GatewayModelTestResult,
  GatewayProviderOption,
  GatewayRoute,
  GatewayRouteSourceKind,
  GatewayServiceTierRule,
  GatewaySystemPromptRule,
  RateSnapshot,
} from "@/lib/api-types"

export type MainTab = "gateway" | "providers" | "usage" | "prices"
export type ConfigTab = "keys" | "routes" | "scheduler" | "models" | "service-tier" | "prompt"

export type ModelSourceLabel = {
  key: string
  label: string
  /** 源分组仅有 ID 时的 tip 文案，如「源 ID: 41」 */
  sourceTip?: string
  route_id?: number
  channel_id: number
}

export type GroupFormState = {
  name: string
  description: string
  status: "active" | "disabled"
  rate_resort_enabled: boolean
  retry_enabled: boolean
  retry_count: string
  failover_enabled: boolean
  failover_max: string
  /** 4xx 状态码也走重试/顺延（默认关） */
  failover_on_4xx: boolean
  cooldown_seconds: string
  first_token_timeout_sec: string
  /** 组级统一 User-Agent；路由选「组」时使用 */
  user_agent: string
}

/** 自动生成时 sk- 之后的字符长度（总长 = 3 + n） */
export const API_KEY_LENS = [16, 24, 32, 48, 64] as const
export type APIKeyLen = (typeof API_KEY_LENS)[number]

export type KeyFormState = {
  name: string
  status: "active" | "disabled"
  quota: string
  ip_whitelist: string
  ip_blacklist: string
  /** 勾选后使用 custom_key；未勾选则按 key_len 自动生成 */
  use_custom_key: boolean
  custom_key: string
  /** 未勾选自定义时生效：sk- 后字符数，默认 48 */
  key_len: APIKeyLen
  reset_quota_used: boolean
}

export type PriceFormState = {
  model_name: string
  billing_mode: "token" | "per_request" | "image" | "video"
  input_mtok: string
  output_mtok: string
  cache_create_mtok: string
  cache_read_mtok: string
  per_request_price: string
  pricing_tiers: PriceTierFormState[]
  image_price_1k: string
  image_price_2k: string
  image_price_4k: string
  video_price_480p: string
  video_price_720p: string
  video_price_1080p: string
  enabled: boolean
}

export type PriceTierFormState = {
  tier_label: string
  min_tokens: string
  max_tokens: string
  per_request_price: string
}

export function channelGroupLabel(
  channelName: string | undefined,
  groupName: string | undefined,
  channelID: number,
) {
  const ch = (channelName || "").trim() || `#${channelID}`
  const g = (groupName || "").trim()
  if (!g) return ch
  // 源分组仅有 id 时不拼进主标签，避免出现「渠道-id:41」
  const idOnly = parseSourceGroupIDRef(g)
  if (idOnly != null) return ch
  return `${ch}-${g}`
}

/** 识别「id:41」这类仅有源分组 ID 的占位文案 */
export function parseSourceGroupIDRef(raw?: string | null): number | null {
  const s = (raw || "").trim()
  if (!s) return null
  const m = /^(?:id|源\s*id)\s*[:：#]?\s*(\d+)$/i.exec(s)
  if (!m) return null
  const n = Number(m[1])
  return Number.isFinite(n) && n > 0 ? n : null
}

/**
 * 源分组展示：
 * - 有名称 → 主文案用名称，tip 可附 ID
 * - 仅 id:N → 主文案用「源分组」，tip 显示「源 ID: N」
 */
export function formatSourceGroupDisplay(
  sourceGroupName?: string | null,
  sourceGroupID?: number | null,
): { label: string; tip?: string } {
  const name = (sourceGroupName || "").trim()
  const idFromName = parseSourceGroupIDRef(name)
  const id =
    sourceGroupID != null && Number(sourceGroupID) > 0
      ? Number(sourceGroupID)
      : idFromName

  if (name && idFromName == null) {
    return {
      label: name,
      tip: id != null ? `源 ID: ${id}` : undefined,
    }
  }
  if (id != null) {
    return { label: "源分组", tip: `源 ID: ${id}` }
  }
  if (name) return { label: name }
  return { label: "" }
}

/** 按探测结果给渠道-分组 tag 着色：失败红 / 未测灰 / ≤5s 绿 / ≤10s 黄 / >20s 橙 */
export function sourceTagTone(tr?: GatewayModelTestResult) {
  if (!tr) {
    return {
      className:
        "border-border bg-muted/30 text-muted-foreground hover:bg-muted/50",
      summary: "未测试 · 点击可探测",
    }
  }
  if (!tr.ok) {
    return {
      className:
        "border-red-200 bg-red-50 text-red-700 dark:border-red-900/50 dark:bg-red-950/40 dark:text-red-300",
      summary: tr.error || `失败 · HTTP ${tr.status_code || "—"}`,
    }
  }
  const ms = tr.latency_ms
  const sec = (ms / 1000).toFixed(ms >= 1000 ? 1 : 2)
  if (ms <= 5_000) {
    return {
      className:
        "border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-900/50 dark:bg-emerald-950/40 dark:text-emerald-300",
      summary: `可用 · ${sec}s（≤5s）`,
    }
  }
  if (ms <= 10_000) {
    return {
      className:
        "border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-900/50 dark:bg-amber-950/40 dark:text-amber-300",
      summary: `可用 · ${sec}s（≤10s）`,
    }
  }
  // >10s 用橙；>20s 仍为橙但 tip 标明更慢
  return {
    className:
      "border-orange-200 bg-orange-50 text-orange-800 dark:border-orange-900/50 dark:bg-orange-950/40 dark:text-orange-300",
    summary: ms > 20_000 ? `可用 · ${sec}s（>20s）` : `可用 · ${sec}s（>10s）`,
  }
}

export function findSourceTest(
  s: ModelSourceLabel,
  tests: GatewayModelTestResult[],
): GatewayModelTestResult | undefined {
  return tests.find(
    (t) =>
      (s.route_id != null && t.route_id === s.route_id) ||
      (s.route_id == null && t.channel_id === s.channel_id),
  )
}

/** 把组内路由展开为可测来源标签（自定义模型 / 无来源时用） */
function pushAllRoutesAsSources(
  routes: Partial<GatewayRoute>[],
  channelNameByID: Map<number, string>,
  providerNameByID: Map<number, string> | undefined,
  push: (src: {
    route_id?: number
    channel_id: number
    channel_name?: string
    source_group_name?: string
    source_group_id?: number | null
  }) => void,
) {
  for (const r of routes) {
    if (!r.id) continue
    // 禁用路由不可调度，不展示为可测来源
    if (r.enabled === false) continue
    const kind = routeSourceKind(r)
    if (kind === "provider") {
      const pid = Number(r.gateway_provider_id) || 0
      const name =
        providerNameByID?.get(pid) ||
        (pid > 0 ? `直连 #${pid}` : "直连渠道")
      push({
        route_id: r.id,
        channel_id: 0,
        channel_name: name,
      })
      continue
    }
    const cid = Number(r.source_channel_id) || 0
    push({
      route_id: r.id,
      channel_id: cid,
      channel_name: channelNameByID.get(cid),
      source_group_name: r.source_group_name,
      source_group_id: r.source_group_id,
    })
  }
}

/**
 * 解析模型可测来源：
 * - 同步模型：优先用 sources / channel_ids
 * - 自定义模型：关联组内全部启用路由，便于逐渠道路测
 * - 无来源时：同样回退到全部启用路由
 */
export function resolveModelSources(
  m: GatewayModelListItem,
  routes: Partial<GatewayRoute>[],
  channelNameByID: Map<number, string>,
  providerNameByID?: Map<number, string>,
): ModelSourceLabel[] {
  const out: ModelSourceLabel[] = []
  const seen = new Set<string>()

  const push = (src: {
    route_id?: number
    channel_id: number
    channel_name?: string
    source_group_name?: string
    source_group_id?: number | null
  }) => {
    if (!src.channel_id && !src.route_id) return
    const channelName =
      src.channel_name || channelNameByID.get(src.channel_id) || undefined
    const label = channelGroupLabel(
      channelName,
      src.source_group_name,
      src.channel_id || 0,
    )
    const sg = formatSourceGroupDisplay(
      src.source_group_name,
      src.source_group_id,
    )
    const key = `${src.route_id ?? 0}:${src.channel_id}:${src.source_group_name ?? ""}:${src.source_group_id ?? ""}`
    if (seen.has(key)) return
    seen.add(key)
    out.push({
      key,
      label,
      sourceTip: sg.tip,
      route_id: src.route_id,
      channel_id: src.channel_id,
    })
  }

  // 自定义模型始终关联当前组全部启用路由（可测试）
  if (m.source === "custom") {
    pushAllRoutesAsSources(routes, channelNameByID, providerNameByID, push)
    return out
  }

  if (m.sources?.length) {
    for (const s of m.sources) push(s)
    return out
  }

  const channelIDs = m.channel_ids ?? []
  if (channelIDs.length === 0) {
    // 旧数据或空来源：回退为全部启用路由，避免无法测试
    pushAllRoutesAsSources(routes, channelNameByID, providerNameByID, push)
    return out
  }

  for (const cid of channelIDs) {
    const matched = routes.filter((r) => r.source_channel_id === cid)
    if (matched.length === 0) {
      push({ channel_id: cid, channel_name: channelNameByID.get(cid) })
      continue
    }
    for (const r of matched) {
      if (r.enabled === false) continue
      push({
        route_id: r.id,
        channel_id: cid,
        channel_name: channelNameByID.get(cid),
        source_group_name: r.source_group_name,
        source_group_id: r.source_group_id,
      })
    }
  }
  return out
}

export const emptyRoute = (): Partial<GatewayRoute> => ({
  source_kind: "monitor",
  source_channel_id: 0,
  gateway_provider_id: 0,
  source_group_id: null,
  source_group_name: "",
  weight: 1,
  rate_convert_mode: "raw",
  rate_convert_value: 1,
  billing_rate_multiplier: 1,
  enabled: true,
  model_mapping: "",
  upstream_protocol: "auto",
  concurrency: 10,
  user_agent_mode: "passthrough",
  user_agent_custom: "",
})

export function routeSourceKind(r: Partial<GatewayRoute>): GatewayRouteSourceKind {
  if (r.source_kind === "provider") return "provider"
  if (r.source_kind === "monitor") return "monitor"
  if ((r.gateway_provider_id ?? 0) > 0 && !(r.source_channel_id ?? 0)) return "provider"
  return "monitor"
}

/** 临时暂停是否仍在生效（过期后调度已恢复） */
export function isRouteTempPaused(
  until: string | null | undefined,
  now = Date.now(),
): boolean {
  if (!until) return false
  const t = new Date(until).getTime()
  return !Number.isNaN(t) && t > now
}

/** 是否有可展示的上次/当前暂停错误 */
export function hasRoutePauseError(route: Partial<GatewayRoute>): boolean {
  return !!(route.temp_unschedulable_reason?.trim() || route.temp_unschedulable_until)
}

/** 对齐网关路由 accountRateMultiplier：原值=源分组 ratio，非强制 1 */
export function routeAccountRate(
  route: Partial<GatewayRoute>,
  groups: RateSnapshot[],
): number {
  if (route.rate_convert_mode === "custom") {
    return Number(route.rate_convert_value) || 0
  }
  const sourceGroupName = (route.source_group_name ?? "").trim()
  const sourceGroupID = Number(route.source_group_id || 0)
  const matchedRatio = sourceGroupName
    ? groups.find((g) => g.model_name === sourceGroupName)?.ratio
    : groups.find((g) => g.remote_group_id === sourceGroupID)?.ratio
  // Runtime fallback: when the live source-group snapshot is unavailable,
  // preserve the persisted billing multiplier so preview order stays stable.
  const savedRate = Number(route.billing_rate_multiplier)
  const sourceRatio = matchedRatio ?? (savedRate > 0 ? savedRate : 1)
  switch (route.rate_convert_mode) {
    case "multiply_100":
      return sourceRatio * 100
    case "divide_100":
      return sourceRatio / 100
    default:
      return sourceRatio
  }
}

/** 路由有效倍率（调度 / 列表排序用，含直连渠道） */
export function routeEffectiveRate(
  route: Partial<GatewayRoute>,
  groups: RateSnapshot[],
  providers: GatewayProviderOption[],
): number {
  if (routeSourceKind(route) === "provider") {
    if ((route.rate_convert_mode as string) === "custom") {
      const v = Number(route.rate_convert_value)
      if (Number.isFinite(v) && v > 0) return v
    }
    const pid = Number(route.gateway_provider_id) || 0
    const p = providers.find((x) => x.id === pid)
    return p?.default_billing_rate && p.default_billing_rate > 0
      ? p.default_billing_rate
      : 1
  }
  return routeAccountRate(route, groups)
}

/**
 * 对齐同步账号 sortSyncAccountRows：按倍率方向 + 权重重排。
 * 返回 { route, index }，index 为原数组下标便于原地编辑。
 */
export function sortGatewayRouteRows(
  routes: Partial<GatewayRoute>[],
  sourceGroupsByChannel: Record<number, RateSnapshot[]>,
  providers: GatewayProviderOption[],
  direction: string,
): { route: Partial<GatewayRoute>; index: number; rate: number }[] {
  const dir = direction === "desc" ? -1 : 1
  return routes
    .map((route, index) => {
      const chId = Number(route.source_channel_id) || 0
      const groups = sourceGroupsByChannel[chId] ?? []
      return {
        route,
        index,
        rate: routeEffectiveRate(route, groups, providers),
      }
    })
    .sort((a, b) => {
      const rateDiff = (a.rate - b.rate) * dir
      if (rateDiff !== 0) return rateDiff
      const wa = Number(a.route.weight) || 1
      const wb = Number(b.route.weight) || 1
      if (wa !== wb) return wb - wa
      return a.index - b.index
    })
}

export function formatRate(value: number) {
  if (!Number.isFinite(value)) return "0"
  return Number(value.toFixed(8)).toString()
}

export function sourceGroupOptionValue(g: RateSnapshot) {
  if (g.remote_group_id != null) return `id:${g.remote_group_id}`
  return `name:${g.model_name}`
}

export function sourceGroupSelectValue(r: Partial<GatewayRoute>) {
  // 优先按 ID 匹配（sub2api 选项值是 id:N）；同时可有名称用于展示
  if (r.source_group_id != null && Number(r.source_group_id) > 0) {
    return `id:${r.source_group_id}`
  }
  const name = (r.source_group_name || "").trim()
  if (name) {
    const idOnly = parseSourceGroupIDRef(name)
    if (idOnly != null) return `id:${idOnly}`
    return `name:${name}`
  }
  return "none"
}

export function parseModelsJSON(raw?: string): GatewayModelListItem[] {
  if (!raw?.trim()) return []
  try {
    return JSON.parse(raw) as GatewayModelListItem[]
  } catch {
    return []
  }
}

/** 后端存 JSON 数组字符串；UI 用每行一个 IP/CIDR */
export function ipListToText(raw?: string): string {
  if (!raw?.trim()) return ""
  try {
    const list = JSON.parse(raw) as unknown
    if (Array.isArray(list)) return list.map(String).join("\n")
  } catch {
    /* fallthrough */
  }
  return raw
}

export function textToIPListJSON(text: string): string {
  const lines = text
    .split(/[\n,]+/)
    .map((s) => s.trim())
    .filter(Boolean)
  if (lines.length === 0) return ""
  return JSON.stringify(lines)
}

export function parseSystemPromptRules(raw?: string): GatewaySystemPromptRule[] {
  if (!raw?.trim()) return []
  try {
    const parsed: unknown = JSON.parse(raw)
    if (!Array.isArray(parsed)) return []
    return parsed.flatMap((item) => {
      if (!item || typeof item !== "object") return []
      const value = item as Partial<GatewaySystemPromptRule>
      const keyIDs = [...new Set((Array.isArray(value.key_ids) ? value.key_ids : []).flatMap((keyID) => {
        const id = Number(keyID)
        return Number.isInteger(id) && id > 0 ? [id] : []
      }))]
      const keyScope = value.key_scope === "selected" ? "selected" : "all"
      return [{
        enabled: value.enabled !== false,
        text: typeof value.text === "string" ? value.text : "",
        override: value.override === true,
        key_scope: keyScope,
        key_ids: keyScope === "selected" ? keyIDs : [],
      }]
    })
  } catch {
    return []
  }
}

export const MTOK = 1_000_000

export function perTokenToMTok(v: number): string {
  if (!v) return "0"
  const m = v * MTOK
  // 去掉多余尾零
  return String(Number(m.toPrecision(12)))
}

export function mTokToPerToken(s: string): number {
  const n = Number(s)
  if (!Number.isFinite(n)) return 0
  return n / MTOK
}

export function parsePriceTiers(raw?: string): PriceTierFormState[] {
  if (!raw?.trim()) return []
  try {
    const parsed: unknown = JSON.parse(raw)
    if (!Array.isArray(parsed)) return []
    return parsed.flatMap((item) => {
      if (!item || typeof item !== "object") return []
      const tier = item as Partial<{
        tier_label: unknown
        min_tokens: unknown
        max_tokens: unknown
        per_request_price: unknown
      }>
      const label = typeof tier.tier_label === "string" ? tier.tier_label : ""
      const min = Number(tier.min_tokens)
      const max = tier.max_tokens == null ? "" : String(Number(tier.max_tokens) || 0)
      const price = Number(tier.per_request_price)
      return [{
        tier_label: label,
        min_tokens: String(Number.isFinite(min) && min >= 0 ? min : 0),
        max_tokens: max,
        per_request_price: String(Number.isFinite(price) && price >= 0 ? price : 0),
      }]
    })
  } catch {
    return []
  }
}

export function serializePriceTiers(tiers: PriceTierFormState[]): string {
  if (tiers.length === 0) return ""
  return JSON.stringify(
    tiers.map((tier) => {
      const min = Number(tier.min_tokens)
      const max = Number(tier.max_tokens)
      return {
        tier_label: tier.tier_label.trim(),
        min_tokens: Number.isFinite(min) && min >= 0 ? min : 0,
        ...(tier.max_tokens.trim() && Number.isFinite(max)
          ? { max_tokens: max }
          : {}),
        per_request_price: Number(tier.per_request_price) || 0,
      }
    }),
  )
}
export const emptyGroupForm = (): GroupFormState => ({
  name: "",
  description: "",
  status: "active",
  rate_resort_enabled: false,
  retry_enabled: true,
  retry_count: "0",
  failover_enabled: true,
  failover_max: "8",
  failover_on_4xx: false,
  cooldown_seconds: "30",
  first_token_timeout_sec: "0",
  user_agent: "",
})

export const emptyKeyForm = (): KeyFormState => ({
  name: "",
  status: "active",
  quota: "0",
  ip_whitelist: "",
  ip_blacklist: "",
  use_custom_key: false,
  custom_key: "",
  key_len: 48,
  reset_quota_used: false,
})

export const defaultServiceTierRules = (): GatewayServiceTierRule[] => [
  { tier: "all", action: "filter", models: [] },
  { tier: "priority", action: "filter", models: [] },
  { tier: "flex", action: "filter", models: [] },
]

export function parseServiceTierRules(raw?: string): GatewayServiceTierRule[] {
  if (!raw?.trim()) return defaultServiceTierRules()
  try {
    const parsed: unknown = JSON.parse(raw)
    if (!Array.isArray(parsed)) return defaultServiceTierRules()
    return parsed.flatMap((item) => {
      if (!item || typeof item !== "object") return []
      const value = item as Partial<GatewayServiceTierRule>
      const tier = value.tier === "priority" || value.tier === "flex" ? value.tier : "all"
      const action = value.action === "passthrough" || value.action === "block" || value.action === "force_priority"
        ? value.action
        : "filter"
      const models = Array.isArray(value.models)
        ? value.models.filter((model): model is string => typeof model === "string" && !!model.trim())
        : []
      const keyIDs = [
        ...(Array.isArray(value.key_ids) ? value.key_ids : []),
        value.key_id,
      ].flatMap((keyID) => {
        const id = Number(keyID)
        return Number.isInteger(id) && id > 0 ? [id] : []
      })
      const userEmail = typeof value.user_email === "string" ? value.user_email.trim() : ""
      const keyScope = value.key_scope === "selected" || keyIDs.length > 0 || userEmail
        ? "selected"
        : "all"
      return [{
        tier,
        action,
        key_scope: keyScope,
        key_ids: [...new Set(keyIDs)],
        user_email: userEmail || undefined,
        models,
      }]
    })
  } catch {
    return defaultServiceTierRules()
  }
}

export const emptyPriceForm = (): PriceFormState => ({
  model_name: "",
  billing_mode: "token",
  input_mtok: "0",
  output_mtok: "0",
  cache_create_mtok: "0",
  cache_read_mtok: "0",
  per_request_price: "0",
  pricing_tiers: [],
  image_price_1k: "0",
  image_price_2k: "0",
  image_price_4k: "0",
  video_price_480p: "0",
  video_price_720p: "0",
  video_price_1080p: "0",
  enabled: true,
})


/** busy key: modelId 批量 / modelId#routeId 单条 */
export function testBusyKey(modelID: string, routeID?: number) {
  return routeID != null ? `${modelID}#${routeID}` : modelID
}
