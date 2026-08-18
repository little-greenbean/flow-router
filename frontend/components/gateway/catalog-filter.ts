export type CatalogFilterModel = {
  model_name: string
  vendor_name?: string
  description?: string
  tags?: string
}

export type CatalogVendor = {
  key: string
  label: string
  count: number
}

export type CatalogVendorSelection = {
  state: "none" | "partial" | "all"
  selectedCount: number
  totalCount: number
}

function vendorKey(value?: string): string {
  return value?.trim().toLowerCase() ?? ""
}

export function catalogVendors(models: CatalogFilterModel[]): CatalogVendor[] {
  const vendors = new Map<string, CatalogVendor>()

  for (const model of models) {
    const label = model.vendor_name?.trim() ?? ""
    const key = vendorKey(label)
    if (!key) continue

    const existing = vendors.get(key)
    if (existing) existing.count += 1
    else vendors.set(key, { key, label, count: 1 })
  }

  return [...vendors.values()].sort(
    (left, right) => right.count - left.count || left.label.localeCompare(right.label),
  )
}

function catalogVendorModelIDs(
  models: CatalogFilterModel[],
  vendor?: string,
): string[] {
  const key = vendorKey(vendor)
  return models
    .filter((model) => !key || vendorKey(model.vendor_name) === key)
    .map((model) => model.model_name)
}

export function catalogVendorSelection(
  models: CatalogFilterModel[],
  selectedModels: ReadonlySet<string>,
  vendor?: string,
): CatalogVendorSelection {
  const modelIDs = catalogVendorModelIDs(models, vendor)
  const selectedCount = modelIDs.filter((id) => selectedModels.has(id)).length
  const totalCount = modelIDs.length
  const state =
    selectedCount === 0
      ? "none"
      : selectedCount === totalCount
        ? "all"
        : "partial"

  return { state, selectedCount, totalCount }
}

export function toggleCatalogVendor(
  models: CatalogFilterModel[],
  selectedModels: ReadonlySet<string>,
  vendor?: string,
): Set<string> {
  const modelIDs = catalogVendorModelIDs(models, vendor)
  const allSelected =
    modelIDs.length > 0 && modelIDs.every((id) => selectedModels.has(id))
  const next = new Set(selectedModels)

  for (const id of modelIDs) {
    if (allSelected) next.delete(id)
    else next.add(id)
  }
  return next
}

export function filterCatalogModels<T extends CatalogFilterModel>(
  models: T[],
  query: string,
  selectedVendors?: ReadonlySet<string>,
): T[] {
  const q = query.trim().toLowerCase()

  return models.filter((model) => {
    if (
      selectedVendors?.size &&
      !selectedVendors.has(vendorKey(model.vendor_name))
    ) {
      return false
    }
    if (!q) return true

    return [model.model_name, model.vendor_name, model.description, model.tags]
      .filter(Boolean)
      .some((value) => value!.toLowerCase().includes(q))
  })
}
