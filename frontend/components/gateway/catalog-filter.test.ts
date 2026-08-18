import assert from "node:assert/strict"
import test from "node:test"
import {
  catalogVendorSelection,
  catalogVendors,
  filterCatalogModels,
  toggleCatalogVendor,
} from "./catalog-filter.ts"

const models = [
  {
    model_name: "gpt-5.6",
    vendor_name: " OpenAI ",
    description: "Latest reasoning model",
    tags: "reasoning",
  },
  {
    model_name: "gpt-4o",
    vendor_name: "openai",
    description: "Vision model",
    tags: "vision",
  },
  {
    model_name: "claude-opus-5",
    vendor_name: "Anthropic",
    description: "Latest reasoning model",
    tags: "reasoning",
  },
  {
    model_name: "community-model",
    vendor_name: "",
    description: "Latest open model",
    tags: "open",
  },
]

test("catalogVendors normalizes duplicate vendor names and counts models", () => {
  assert.deepEqual(catalogVendors(models), [
    { key: "openai", label: "OpenAI", count: 2 },
    { key: "anthropic", label: "Anthropic", count: 1 },
  ])
})

test("filterCatalogModels combines multi-vendor OR with keyword AND", () => {
  const filtered = filterCatalogModels(
    models,
    "latest",
    new Set(["openai", "anthropic"]),
  )

  assert.deepEqual(
    filtered.map((model) => model.model_name),
    ["gpt-5.6", "claude-opus-5"],
  )
})

test("catalogVendorSelection follows selected models instead of filter state", () => {
  assert.deepEqual(catalogVendorSelection(models, new Set(), "openai"), {
    state: "none",
    selectedCount: 0,
    totalCount: 2,
  })
  assert.deepEqual(catalogVendorSelection(models, new Set(["gpt-5.6"]), "openai"), {
    state: "partial",
    selectedCount: 1,
    totalCount: 2,
  })
  assert.deepEqual(
    catalogVendorSelection(models, new Set(["gpt-5.6", "gpt-4o"]), "openai"),
    { state: "all", selectedCount: 2, totalCount: 2 },
  )
})

test("toggleCatalogVendor adds or removes one vendor without changing others", () => {
  const withOpenAI = toggleCatalogVendor(models, new Set(["claude-opus-5"]), "openai")
  assert.deepEqual([...withOpenAI].sort(), ["claude-opus-5", "gpt-4o", "gpt-5.6"])

  const withoutOpenAI = toggleCatalogVendor(models, withOpenAI, "openai")
  assert.deepEqual([...withoutOpenAI], ["claude-opus-5"])
})
