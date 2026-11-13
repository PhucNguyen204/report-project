# Q01/Q02 scoped search implementation plan

> Root approved execution with TDD. Root owns Git, API artifacts and progress reports.

**Goal:** remove truncated search totals, implement typed food filters and cursor pagination while preserving current result variants.

**Architecture:** planning aggregates catalog foods, published recipes and inventory lot results. Each source returns at most the global limit plus one row, after the same `(name,type,id)` cursor. All counts and filter facets cover the entire filtered source before cursor/limit. Planning accesses pantry only through the inventory query port.

**Tech Stack:** Go1.26.11, pgx/PostgreSQL16, unaccent, existing platform pagination.

**Spec:** `backmatter/pl/f-dac-ta-api.tex:34–36`, `backmatter/pl/b-dac-ta-bo-sung.tex:264–278`, `chapters/c3/api.tex:58`; ADR06 in the09/11 backend plan.

## ADR09 — mixed search filter semantics (approved by root)

The specification defines category/location/expiry filters for food results but does not define recipe categories or ingredient-to-stock matching for recipe filtering. Preserve the existing `data.items` union of food/recipe/lot, rather than inventing three response arrays or ingredient semantics.

- Q01 categoryId filters foods and lots through their food category. Recipes are excluded whenever a category/location/expiry filter is present.
- Location and expiryState describe actual stock lots. They exclude catalog foods and recipes; a catalog defaultLocation is not actual pantry placement.
- Repeated/comma-separated values are OR within one field, AND across fields. Invalid UUID/location/state is400; a valid nonexistent category yields no matches.
- `q` is literal, case/accent/đ-insensitive across all sources. `%` and `_` are characters, not SQL wildcards. No new fuzzy ranking or Elasticsearch dependency is added under ADR06.
- Sort name/type/id ASC with COLLATE C to preserve the prior Go string ordering. Q01 defaults20/max100 and has an opaque cursor bound to user/path/query filters through ReadPage.
- Preserve `data.facets` type counts and add `data.filterFacets.{categoryId,location,expiryState}`. Counts describe hits after all active filters, before cursor/limit; they do not exclude their own facet filter. A food hit and its lot hits are distinct search results.
- Q02 remains food autocomplete only, q2–100 runes/default10/max10, without cursor or physical-stock filters. Its food count covers all matches; result rows remain bounded10 with unit/defaultLocation/shelfLives intact.
- All SQL retains explicit tenant scope, published/template visibility and member checks. Inventory derives current Vietnam date and reminder N from the viewer.
- Pagination does not promise a snapshot across separate HTTP calls or sort-key edits. Within each source, its page and aggregate are read in one SQL statement.

## Interfaces and files

Modify `backend/internal/platform/contracts.go`:

```go
type SearchKey struct { Name, Type, ID string }
type LotSearchQuery struct {
    Query string
    CategoryIDs, Locations, ExpiryStates []string
    After SearchKey
    Limit int
}
type LotSearchPage struct {
    Items []map[string]any
    Total int
    CategoryCounts, LocationCounts, ExpiryCounts map[string]int
}
type LotSearchPagePort interface {
    SearchLotsPage(context.Context, pgx.Tx, string, string, LotSearchQuery) (LotSearchPage, error)
}
```

The two string arguments are sid and user. Replace the internal old LotSearchPort and update its caller/tests; do not retain a dead100-row path. New inventory port accepts limit up to101 for global lookahead and returns rows in name/id C order.

Modify `backend/internal/modules/planning/search.go`: validated SearchInput, SearchPage aggregation, preserve existing Search wrapper, explicit FilterFacets and an internal next-key result. Modify only the Search registration block in `http_extra.go` to parse Q01 page/filters and return WritePage; Q02 retains its bounded response contract.

Modify `backend/internal/modules/inventory/search.go`: one full-filter CTE for scoped active lots, viewer-specific expiry classification, page rows and uncapped aggregates. Add migration048 only if query/index evidence warrants it; do not change committed migrations.

## Tasks

- [x] Write RED real PostgreSQL/HTTP cases: more than100 rows per source, global mixed cursor and name/type/id ties, inserted-before boundary, category/location/expiry OR+AND, viewer-specific N, full-filter facets constant across pages, published/scope isolation and literal wildcard queries.
- [x] Write RED validation cases: bad/duplicate limit/cursor, changed filter/user/space, invalid filter values; Q02 q bounds/limit10/cursor rejection and unchanged suggestion fields.
- [x] Implement input/port and minimal SQL/page aggregation; rerun the same RED tests to GREEN.
- [x] Không thêm migration048 khi chưa có bằng chứng cần index mới; fresh DB migrations vẫn được full integration kiểm. Tải lớn và EXPLAIN sâu thuộc F37, chưa tự suy ra đã tối ưu từ test nghiệp vụ.
- [x] Review transaction boundaries, independent source ordering and counted-set/page predicates; send root exact query/schema/metadata changes.
- [x] Run planning/inventory integration with race and coverage, bare project tests, vet and diff checks. Report every observed failure, skip or external-service limitation. Root commits/pushes after review.

Root verification13/11: scoped/published/facet-across-pages thêm đủ và full integration407 pass/0fail/0skip. Root phát hiện uppercase category UUID bị text comparison làm0hits, testRED→castuuid[]→GREEN. ContractREDfilterFacets/meta chưa được nhận→GREEN; OpenAPI202schemas/101operations. Bộ performance4routes không đo Q01 nên không dùng để khẳng định latency Q01.
