# Catalog Vendor Filter Design

## Goal

Make the official model catalog easier to browse by adding vendor filter tags
under the search box. Users may select zero, one, or multiple vendors while
choosing catalog models for a gateway group.

## Design

- Derive the vendor list from the currently loaded catalog using `vendor_name`.
- Normalize vendor names by trimming whitespace and de-duplicating them.
- Show an `全部` control plus one control per vendor beneath the search input.
- Vendor controls use multi-select semantics. Selecting vendors keeps models
  from any selected vendor; clicking an active vendor removes it.
- `全部` clears the vendor selection. Typing a query does not clear vendor
  selections, and the two filters are combined with AND semantics.
- Vendor controls are horizontally scrollable/wrapping so long vendor lists do
  not expand the dialog beyond its existing responsive bounds.
- Empty/unknown vendor values remain discoverable through `全部` and keyword
  search but do not create a blank vendor tag.

## Verification

- Run frontend lint and production build.
- Use the local catalog dialog to select multiple vendors and verify the list
  contains models from either vendor, then clear the selection with `全部`.
