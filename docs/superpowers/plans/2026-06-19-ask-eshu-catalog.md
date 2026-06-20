# Ask Eshu — `catalog` Package Implementation Plan (Plan 1 of 7)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `go/internal/ask/catalog`, Ask Eshu's self-knowledge: a complete, drift-gated catalog of every implemented API route and MCP tool, each annotated with a backend (NornicDB / Postgres / both / embedded) and a cost class, sourced from the canonical surface inventory.

**Architecture:** The catalog spine is the generated surface inventory (`go/internal/capabilitycatalog/data/surface-inventory.generated.json`) — the authoritative, drift-gated list of routes and tools. The catalog package parses that inventory, filters to implemented `api_route` and `mcp_tool` surfaces, and joins a curated annotation overlay (backend + cost), keyed by surface name. A coverage drift test fails when any implemented surface lacks an annotation, so the catalog can never silently fall behind the inventory.

**Tech Stack:** Go 1.26.0, standard library only (`encoding/json`, `embed`, `sort`, `strings`, `testing`). No third-party dependencies.

## Global Constraints

- Go version floor: `go 1.26.0` (from `go/go.mod`).
- Module import prefix: `github.com/eshu-hq/eshu/go/internal/...`.
- Every source file stays under 500 lines; split before approaching the cap.
- Each new package directory ships `doc.go`, `README.md`, and `AGENTS.md`.
- MUST use `rg` for text search, `rg --files` for file discovery. NEVER `grep`/`find`.
- TDD: failing test first, minimal implementation, frequent commits.
- No AI attribution in commits, code, or docs.
- Tests use `t.Parallel()` and table-driven style, matching repo convention.
- Focused test command (run from the `go/` directory):
  `go test ./internal/ask/catalog -count=1`.
- This package is pure: it does NOT query Postgres or a graph backend, start a
  runtime, or read live state. It reads the embedded inventory artifact only.

---

### Task 1: Package skeleton, core types, and embedded inventory

**Files:**
- Create: `go/internal/ask/catalog/doc.go`
- Create: `go/internal/ask/catalog/catalog.go`
- Create: `go/internal/ask/catalog/catalog_test.go`

**Interfaces:**
- Consumes: nothing (foundation task).
- Produces:
  - `type Backend string` with constants `BackendNornicDB = "nornicdb"`,
    `BackendPostgres = "postgres"`, `BackendBoth = "both"`,
    `BackendEmbedded = "embedded"`, `BackendUnknown = "unknown"`.
  - `type CostClass string` with constants `CostLow = "low"`,
    `CostModerate = "moderate"`, `CostHigh = "high"`.
  - `type SurfaceKind string` with constants `KindAPIRoute = "api_route"`,
    `KindMCPTool = "mcp_tool"`.
  - `type Entry struct { Kind SurfaceKind; Name string; Backend Backend; Cost CostClass }`.
  - `type Catalog struct { ... }` (unexported fields) with no methods yet.

- [ ] **Step 1: Write the failing test**

```go
package catalog

import "testing"

func TestBackendAndCostConstants(t *testing.T) {
	t.Parallel()
	if BackendNornicDB != "nornicdb" || BackendPostgres != "postgres" ||
		BackendBoth != "both" || BackendEmbedded != "embedded" ||
		BackendUnknown != "unknown" {
		t.Fatalf("backend constant drift: %q %q %q %q %q",
			BackendNornicDB, BackendPostgres, BackendBoth, BackendEmbedded, BackendUnknown)
	}
	if CostLow != "low" || CostModerate != "moderate" || CostHigh != "high" {
		t.Fatalf("cost constant drift: %q %q %q", CostLow, CostModerate, CostHigh)
	}
	if KindAPIRoute != "api_route" || KindMCPTool != "mcp_tool" {
		t.Fatalf("kind constant drift: %q %q", KindAPIRoute, KindMCPTool)
	}
}

func TestEntryIsValueType(t *testing.T) {
	t.Parallel()
	e := Entry{Kind: KindMCPTool, Name: "find_symbol", Backend: BackendNornicDB, Cost: CostLow}
	if e.Name != "find_symbol" || e.Backend != BackendNornicDB || e.Cost != CostLow {
		t.Fatalf("unexpected entry: %+v", e)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd go && go test ./internal/ask/catalog -run TestBackend -count=1`
Expected: build failure — `undefined: BackendNornicDB` (package does not exist yet).

- [ ] **Step 3: Write `doc.go`**

```go
// Package catalog is Ask Eshu's self-knowledge of every implemented API route
// and MCP tool it can call to answer a question.
//
// The catalog spine is the canonical surface inventory artifact
// (go/internal/capabilitycatalog/data/surface-inventory.generated.json), which
// is generated and drift-gated on every surface change. The catalog parses that
// inventory, keeps only implemented api_route and mcp_tool surfaces, and joins a
// curated annotation overlay that records each surface's backend (NornicDB,
// Postgres, both, or the embedded inventory) and a coarse cost class. The
// backend and cost signals let the Ask Eshu planner prefer the cheapest correct
// retrieval path.
//
// Backend and cost are NOT carried by the surface inventory; they are a curated
// overlay in this package. A coverage check (Catalog.Unannotated) reports any
// implemented surface that lacks an annotation so the overlay can never silently
// fall behind the inventory. The package is pure: it reads the embedded artifact
// only and never queries Postgres, a graph backend, or live runtime state.
package catalog
```

- [ ] **Step 4: Write `catalog.go` with the types**

```go
package catalog

// Backend names the data store a surface reads from. It drives Ask Eshu's
// fastest-path planning between the graph backend and Postgres.
type Backend string

const (
	// BackendNornicDB marks a surface served from the canonical graph backend.
	BackendNornicDB Backend = "nornicdb"
	// BackendPostgres marks a surface served from the Postgres read model.
	BackendPostgres Backend = "postgres"
	// BackendBoth marks a surface that reads from both stores.
	BackendBoth Backend = "both"
	// BackendEmbedded marks a surface served from embedded artifact data with no
	// live backend read.
	BackendEmbedded Backend = "embedded"
	// BackendUnknown marks a surface with no annotation yet. It is the conservative
	// default and is reported by Catalog.Unannotated.
	BackendUnknown Backend = "unknown"
)

// CostClass is a coarse retrieval-cost bucket used to bias path selection toward
// cheaper surfaces.
type CostClass string

const (
	// CostLow marks a bounded, indexed, or embedded read.
	CostLow CostClass = "low"
	// CostModerate marks a scoped read with moderate fan-out.
	CostModerate CostClass = "moderate"
	// CostHigh marks a broad or denormalized read.
	CostHigh CostClass = "high"
)

// SurfaceKind is the surface family a catalog entry describes.
type SurfaceKind string

const (
	// KindAPIRoute is an HTTP API route surface.
	KindAPIRoute SurfaceKind = "api_route"
	// KindMCPTool is an MCP tool surface.
	KindMCPTool SurfaceKind = "mcp_tool"
)

// Entry is one callable surface Ask Eshu knows about, with the backend and cost
// annotations that drive planning.
type Entry struct {
	// Kind is the surface family.
	Kind SurfaceKind
	// Name is the surface identifier: an API route name like
	// "GET /api/v0/code/symbols" or an MCP tool name like "find_symbol".
	Name string
	// Backend is the annotated data store, or BackendUnknown when unannotated.
	Backend Backend
	// Cost is the annotated cost class, or CostHigh when unannotated (conservative).
	Cost CostClass
}

// Catalog holds the parsed, annotated set of callable surfaces.
type Catalog struct {
	entries []Entry
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd go && go test ./internal/ask/catalog -count=1`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add go/internal/ask/catalog/doc.go go/internal/ask/catalog/catalog.go go/internal/ask/catalog/catalog_test.go
git commit -m "feat(ask/catalog): core types for the Ask Eshu surface catalog (#3250)"
```

---

### Task 2: Parse the surface inventory into entries

**Files:**
- Modify: `go/internal/ask/catalog/catalog.go`
- Test: `go/internal/ask/catalog/parse_test.go`

**Interfaces:**
- Consumes: `Entry`, `Catalog`, `SurfaceKind` constants from Task 1.
- Produces:
  - `func Parse(inventoryJSON []byte) (*Catalog, error)` — parses the inventory
    envelope, keeps only `implemented` surfaces of kind `api_route`/`mcp_tool`,
    and returns a `Catalog` whose entries have `Backend = BackendUnknown` and
    `Cost = CostHigh` until annotation (Task 3) runs.
  - `func (c *Catalog) Entries() []Entry` — returns a copy of the entries, sorted
    by `(Kind, Name)`.

- [ ] **Step 1: Write the failing test**

```go
package catalog

import "testing"

const sampleInventory = `{
  "version": "v1",
  "surfaces": [
    {"category": "api_route", "name": "GET /api/v0/code/symbols", "readiness": "implemented"},
    {"category": "mcp_tool", "name": "find_symbol", "readiness": "implemented"},
    {"category": "mcp_tool", "name": "draft_tool", "readiness": "draft"},
    {"category": "command", "name": "eshu", "readiness": "implemented"},
    {"category": "reducer_domain", "name": "code_calls", "readiness": "implemented"}
  ]
}`

func TestParseKeepsOnlyImplementedRoutesAndTools(t *testing.T) {
	t.Parallel()
	cat, err := Parse([]byte(sampleInventory))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	got := cat.Entries()
	if len(got) != 2 {
		t.Fatalf("expected 2 entries, got %d: %+v", len(got), got)
	}
	// Sorted by (Kind, Name): api_route before mcp_tool.
	if got[0].Kind != KindAPIRoute || got[0].Name != "GET /api/v0/code/symbols" {
		t.Fatalf("entry[0] = %+v", got[0])
	}
	if got[1].Kind != KindMCPTool || got[1].Name != "find_symbol" {
		t.Fatalf("entry[1] = %+v", got[1])
	}
	// Unannotated defaults are conservative.
	if got[0].Backend != BackendUnknown || got[0].Cost != CostHigh {
		t.Fatalf("expected conservative defaults, got %+v", got[0])
	}
}

func TestParseRejectsInvalidJSON(t *testing.T) {
	t.Parallel()
	if _, err := Parse([]byte("{not json")); err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestParseEmptyInventoryYieldsNoEntries(t *testing.T) {
	t.Parallel()
	cat, err := Parse([]byte(`{"version":"v1","surfaces":[]}`))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if len(cat.Entries()) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(cat.Entries()))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd go && go test ./internal/ask/catalog -run TestParse -count=1`
Expected: build failure — `undefined: Parse`.

- [ ] **Step 3: Add `Parse` and `Entries` to `catalog.go`**

```go
import (
	"encoding/json"
	"fmt"
	"sort"
)

// inventoryEnvelope mirrors the generated surface-inventory artifact. Only the
// fields the catalog needs are decoded; the artifact may carry more.
type inventoryEnvelope struct {
	Version  string           `json:"version"`
	Surfaces []surfaceRecord  `json:"surfaces"`
}

type surfaceRecord struct {
	Category  string `json:"category"`
	Name      string `json:"name"`
	Readiness string `json:"readiness"`
}

// Parse reads the canonical surface-inventory JSON and returns a Catalog of the
// implemented api_route and mcp_tool surfaces. Entries are unannotated
// (BackendUnknown, CostHigh) until Annotate runs. A nil or malformed document is
// an error; an empty surface list is valid and yields an empty catalog.
func Parse(inventoryJSON []byte) (*Catalog, error) {
	var env inventoryEnvelope
	if err := json.Unmarshal(inventoryJSON, &env); err != nil {
		return nil, fmt.Errorf("parse surface inventory: %w", err)
	}
	entries := make([]Entry, 0, len(env.Surfaces))
	for _, rec := range env.Surfaces {
		if rec.Readiness != "implemented" {
			continue
		}
		kind := SurfaceKind(rec.Category)
		if kind != KindAPIRoute && kind != KindMCPTool {
			continue
		}
		entries = append(entries, Entry{
			Kind:    kind,
			Name:    rec.Name,
			Backend: BackendUnknown,
			Cost:    CostHigh,
		})
	}
	c := &Catalog{entries: entries}
	c.sort()
	return c, nil
}

func (c *Catalog) sort() {
	sort.Slice(c.entries, func(i, j int) bool {
		if c.entries[i].Kind != c.entries[j].Kind {
			return c.entries[i].Kind < c.entries[j].Kind
		}
		return c.entries[i].Name < c.entries[j].Name
	})
}

// Entries returns a sorted copy of the catalog entries.
func (c *Catalog) Entries() []Entry {
	out := make([]Entry, len(c.entries))
	copy(out, c.entries)
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd go && go test ./internal/ask/catalog -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add go/internal/ask/catalog/catalog.go go/internal/ask/catalog/parse_test.go
git commit -m "feat(ask/catalog): parse the surface inventory into callable entries (#3250)"
```

---

### Task 3: Curated annotation overlay and join

**Files:**
- Create: `go/internal/ask/catalog/annotations.go`
- Modify: `go/internal/ask/catalog/catalog.go`
- Test: `go/internal/ask/catalog/annotations_test.go`

**Interfaces:**
- Consumes: `Catalog`, `Entry`, `Backend`, `CostClass` from Tasks 1–2.
- Produces:
  - `type Annotation struct { Backend Backend; Cost CostClass }`.
  - `func annotations() map[string]Annotation` — the curated overlay keyed by
    surface name (package-private, defined in `annotations.go`).
  - `func (c *Catalog) Annotate()` — applies the overlay onto entries in place.
  - `func (c *Catalog) Unannotated() []string` — sorted names of implemented
    surfaces with no overlay entry (still `BackendUnknown` after `Annotate`).

- [ ] **Step 1: Write the failing test**

```go
package catalog

import "testing"

func TestAnnotateAppliesOverlay(t *testing.T) {
	t.Parallel()
	cat, err := Parse([]byte(sampleInventory))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	cat.Annotate()
	for _, e := range cat.Entries() {
		if e.Name == "find_symbol" {
			if e.Backend != BackendNornicDB {
				t.Fatalf("find_symbol backend = %q, want nornicdb", e.Backend)
			}
			if e.Cost != CostLow {
				t.Fatalf("find_symbol cost = %q, want low", e.Cost)
			}
			return
		}
	}
	t.Fatal("find_symbol entry not found")
}

func TestUnannotatedReportsMissingOverlay(t *testing.T) {
	t.Parallel()
	// An implemented surface with no overlay entry must be reported.
	inv := `{"version":"v1","surfaces":[
		{"category":"mcp_tool","name":"surface_without_annotation","readiness":"implemented"}
	]}`
	cat, err := Parse([]byte(inv))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	cat.Annotate()
	missing := cat.Unannotated()
	if len(missing) != 1 || missing[0] != "surface_without_annotation" {
		t.Fatalf("Unannotated() = %v, want [surface_without_annotation]", missing)
	}
}

func TestAnnotationOverlayHasNoUnknownBackends(t *testing.T) {
	t.Parallel()
	for name, a := range annotations() {
		if a.Backend == BackendUnknown || a.Backend == "" {
			t.Fatalf("overlay entry %q has invalid backend %q", name, a.Backend)
		}
		switch a.Cost {
		case CostLow, CostModerate, CostHigh:
		default:
			t.Fatalf("overlay entry %q has invalid cost %q", name, a.Cost)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd go && go test ./internal/ask/catalog -run TestAnnotate -count=1`
Expected: build failure — `undefined: annotations` / `Annotate` / `Unannotated`.

- [ ] **Step 3: Write `annotations.go` (seed overlay)**

```go
package catalog

// Annotation records the backend and cost class for one callable surface. It is
// the curated half of the catalog: the surface inventory says a surface exists;
// this overlay says how it is served.
type Annotation struct {
	Backend Backend
	Cost    CostClass
}

// annotations is the curated overlay keyed by surface name. Every implemented
// api_route and mcp_tool surface in the inventory MUST have an entry here; the
// coverage drift test (TestOverlayCoversInventory) fails otherwise. Seed entries
// below are illustrative; the drift test in Task 5 enumerates the real set to
// fill in.
func annotations() map[string]Annotation {
	return map[string]Annotation{
		// MCP tools.
		"find_symbol": {Backend: BackendNornicDB, Cost: CostLow},
		// API routes.
		"GET /api/v0/code/symbols": {Backend: BackendNornicDB, Cost: CostLow},
	}
}
```

- [ ] **Step 4: Add `Annotate` and `Unannotated` to `catalog.go`**

```go
import "strings" // add alongside existing imports

// Annotate applies the curated overlay onto the catalog entries in place. An
// entry with no overlay match keeps the conservative BackendUnknown/CostHigh
// defaults and is reported by Unannotated.
func (c *Catalog) Annotate() {
	overlay := annotations()
	for i := range c.entries {
		if a, ok := overlay[c.entries[i].Name]; ok {
			c.entries[i].Backend = a.Backend
			c.entries[i].Cost = a.Cost
		}
	}
}

// Unannotated returns the sorted names of implemented surfaces that still have
// no overlay annotation after Annotate. A non-empty result means the overlay has
// fallen behind the surface inventory.
func (c *Catalog) Unannotated() []string {
	var missing []string
	for _, e := range c.entries {
		if e.Backend == BackendUnknown {
			missing = append(missing, e.Name)
		}
	}
	sort.Strings(missing)
	_ = strings.TrimSpace // retained for future name normalization
	return missing
}
```

Note: if the `strings` import is not otherwise used, drop the import and the
`_ = strings.TrimSpace` line. Keep the file compiling with `gofmt`.

- [ ] **Step 5: Run test to verify it passes**

Run: `cd go && go test ./internal/ask/catalog -count=1`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add go/internal/ask/catalog/annotations.go go/internal/ask/catalog/catalog.go go/internal/ask/catalog/annotations_test.go
git commit -m "feat(ask/catalog): curated backend/cost annotation overlay (#3250)"
```

---

### Task 4: Planning queries (lookup, filter, cheapest-first ordering)

**Files:**
- Modify: `go/internal/ask/catalog/catalog.go`
- Test: `go/internal/ask/catalog/query_test.go`

**Interfaces:**
- Consumes: `Catalog`, `Entry`, `Backend`, `CostClass`, `SurfaceKind` from Tasks 1–3.
- Produces:
  - `func (c *Catalog) Lookup(name string) (Entry, bool)`.
  - `func (c *Catalog) ByBackend(b Backend) []Entry` — sorted entries for a backend.
  - `func (c *Catalog) CheapestFirst() []Entry` — entries ordered low→moderate→high
    cost, ties broken by `(Kind, Name)`. This is the planner's preference order.

- [ ] **Step 1: Write the failing test**

```go
package catalog

import "testing"

func TestLookup(t *testing.T) {
	t.Parallel()
	cat, _ := Parse([]byte(sampleInventory))
	cat.Annotate()
	e, ok := cat.Lookup("find_symbol")
	if !ok {
		t.Fatal("expected find_symbol present")
	}
	if e.Backend != BackendNornicDB {
		t.Fatalf("find_symbol backend = %q", e.Backend)
	}
	if _, ok := cat.Lookup("does_not_exist"); ok {
		t.Fatal("expected absent tool to return ok=false")
	}
}

func TestCheapestFirstOrdersByCost(t *testing.T) {
	t.Parallel()
	c := &Catalog{entries: []Entry{
		{Kind: KindMCPTool, Name: "b_high", Backend: BackendPostgres, Cost: CostHigh},
		{Kind: KindMCPTool, Name: "a_low", Backend: BackendNornicDB, Cost: CostLow},
		{Kind: KindMCPTool, Name: "c_moderate", Backend: BackendBoth, Cost: CostModerate},
	}}
	order := c.CheapestFirst()
	got := []string{order[0].Name, order[1].Name, order[2].Name}
	want := []string{"a_low", "c_moderate", "b_high"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("CheapestFirst order = %v, want %v", got, want)
		}
	}
}

func TestByBackendFilters(t *testing.T) {
	t.Parallel()
	c := &Catalog{entries: []Entry{
		{Kind: KindMCPTool, Name: "g1", Backend: BackendNornicDB, Cost: CostLow},
		{Kind: KindMCPTool, Name: "p1", Backend: BackendPostgres, Cost: CostLow},
		{Kind: KindMCPTool, Name: "g2", Backend: BackendNornicDB, Cost: CostHigh},
	}}
	graph := c.ByBackend(BackendNornicDB)
	if len(graph) != 2 || graph[0].Name != "g1" || graph[1].Name != "g2" {
		t.Fatalf("ByBackend(nornicdb) = %+v", graph)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd go && go test ./internal/ask/catalog -run 'TestLookup|TestCheapestFirst|TestByBackend' -count=1`
Expected: build failure — `undefined: Lookup` / `CheapestFirst` / `ByBackend`.

- [ ] **Step 3: Add the query methods to `catalog.go`**

```go
// Lookup returns the entry for an exact surface name.
func (c *Catalog) Lookup(name string) (Entry, bool) {
	for _, e := range c.entries {
		if e.Name == name {
			return e, true
		}
	}
	return Entry{}, false
}

// ByBackend returns the sorted entries served by a backend.
func (c *Catalog) ByBackend(b Backend) []Entry {
	var out []Entry
	for _, e := range c.entries {
		if e.Backend == b {
			out = append(out, e)
		}
	}
	return out
}

// costRank orders cost classes from cheapest to most expensive. Unknown costs
// sort last so unclassified surfaces are never preferred.
func costRank(c CostClass) int {
	switch c {
	case CostLow:
		return 0
	case CostModerate:
		return 1
	case CostHigh:
		return 2
	default:
		return 3
	}
}

// CheapestFirst returns the entries ordered cheapest cost first, ties broken by
// (Kind, Name). This is the planner's default preference order.
func (c *Catalog) CheapestFirst() []Entry {
	out := c.Entries() // already (Kind, Name) sorted
	sort.SliceStable(out, func(i, j int) bool {
		return costRank(out[i].Cost) < costRank(out[j].Cost)
	})
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd go && go test ./internal/ask/catalog -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add go/internal/ask/catalog/catalog.go go/internal/ask/catalog/query_test.go
git commit -m "feat(ask/catalog): planner queries (lookup, by-backend, cheapest-first) (#3250)"
```

---

### Task 5: Coverage drift gate against the real inventory + package docs

**Files:**
- Create: `go/internal/ask/catalog/coverage_test.go`
- Create: `go/internal/ask/catalog/README.md`
- Create: `go/internal/ask/catalog/AGENTS.md`
- Modify: `go/internal/ask/catalog/annotations.go` (fill the overlay to full coverage)

**Interfaces:**
- Consumes: `Parse`, `Annotate`, `Unannotated` from earlier tasks.
- Produces: a drift test asserting the overlay covers every implemented surface
  in the real, committed inventory artifact; no new exported API.

- [ ] **Step 1: Write the failing drift test**

This reads the real generated inventory by relative path, the same way
`cmd/capability-inventory` tests reach real specs.

```go
package catalog

import (
	"os"
	"path/filepath"
	"testing"
)

// realInventoryPath returns the path to the committed, generated surface
// inventory artifact relative to this test file.
func realInventoryPath(t *testing.T) string {
	t.Helper()
	// catalog_test.go lives in go/internal/ask/catalog; the artifact lives in
	// go/internal/capabilitycatalog/data.
	p := filepath.Join("..", "..", "capabilitycatalog", "data", "surface-inventory.generated.json")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("surface inventory artifact not found at %s: %v", p, err)
	}
	return p
}

func TestOverlayCoversInventory(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(realInventoryPath(t))
	if err != nil {
		t.Fatalf("read inventory: %v", err)
	}
	cat, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse real inventory: %v", err)
	}
	cat.Annotate()
	if missing := cat.Unannotated(); len(missing) != 0 {
		t.Fatalf("%d implemented surfaces lack a backend/cost annotation in annotations(); add them:\n%v",
			len(missing), missing)
	}
}
```

- [ ] **Step 2: Run the drift test to verify it fails**

Run: `cd go && go test ./internal/ask/catalog -run TestOverlayCoversInventory -count=1`
Expected: FAIL listing every implemented `api_route`/`mcp_tool` surface name not
yet present in `annotations()`. This list is the work item for Step 3.

- [ ] **Step 3: Fill `annotations()` to full coverage**

Use the failing-test output as the authoritative checklist. For each listed
surface name, add an `Annotation` to the map in `annotations.go`. Classify each:

- **Backend** — determine from the owning query handler / MCP tool:
  - serves the embedded surface inventory or other embedded artifact, "no live
    backend read" / "graph sidecar unused" in the capability-matrix profile
    notes → `BackendEmbedded`.
  - reducer read-model / `reducer_*` rows / "requires the Postgres ... read
    model" in the handler error string → `BackendPostgres`.
  - graph topology / Cypher / relationship traversal → `BackendNornicDB`.
  - composes a graph read and a Postgres read → `BackendBoth`.
  - To confirm, read the handler: `rg -n "<surface or capability id>" go/internal/query go/internal/mcp`.
- **Cost** — map the capability-matrix `p95_latency_ms` for the surface's
  capability (search `specs/` capability YAML): `<= 200` → `CostLow`,
  `1000`–`5000` → `CostModerate`, `> 5000` → `CostHigh`. When no matrix latency
  exists, default `CostModerate` and add a `// unverified latency` comment.

Keep `annotations.go` under 500 lines; if the map grows past the cap, split into
`annotations_routes.go` and `annotations_tools.go`, each returning a partial map
merged by `annotations()`.

- [ ] **Step 4: Run the full package tests to verify they pass**

Run: `cd go && go test ./internal/ask/catalog -count=1`
Expected: PASS (including `TestOverlayCoversInventory`).

- [ ] **Step 5: Write `README.md`**

```markdown
# catalog

`catalog` is Ask Eshu's self-knowledge: the set of API routes and MCP tools it
can call, each annotated with a backend and a cost class so the planner can
prefer the cheapest correct retrieval path.

## What it does

- Parses the canonical surface inventory artifact
  (`go/internal/capabilitycatalog/data/surface-inventory.generated.json`).
- Keeps only `implemented` `api_route` and `mcp_tool` surfaces.
- Joins a curated overlay (`annotations.go`) that records each surface's
  `Backend` (nornicdb / postgres / both / embedded) and `CostClass`.
- Exposes planner queries: `Lookup`, `ByBackend`, `CheapestFirst`.

## Why the overlay is curated

The surface inventory does not record which store a surface reads or how
expensive it is. Those signals live in the capability-matrix profiles
(`p95_latency_ms`) and query-handler errors. The overlay encodes them
explicitly, and `TestOverlayCoversInventory` fails if any implemented surface is
missing an annotation, so the overlay cannot silently fall behind the inventory.

## What it is not

Pure and read-only. It never queries Postgres or a graph backend, starts a
runtime, or reads live state.
```

- [ ] **Step 6: Write `AGENTS.md`**

```markdown
# AGENTS — catalog

## Read first

1. `doc.go` — package contract.
2. `catalog.go` — types, inventory parsing, planner queries.
3. `annotations.go` — the curated backend/cost overlay.
4. `../../../capabilitycatalog/data/surface-inventory.generated.json` — the spine.

## Invariants

- The inventory is the spine; the overlay only annotates. Never add a surface to
  the overlay that is not in the inventory.
- Every implemented `api_route`/`mcp_tool` surface MUST have an overlay entry.
  `TestOverlayCoversInventory` enforces this — do not weaken it.
- An unannotated entry keeps `BackendUnknown`/`CostHigh`; never default an
  unknown surface to a cheap class.
- Pure package: no Postgres, graph, runtime, or live-state reads here.

## Common changes

- A new route/tool was added: regenerate the inventory
  (`go run ./cmd/capability-inventory -mode generate` from `go/`), then add its
  overlay annotation. The drift test names what is missing.
- Re-classify a backend/cost: read the owning handler to confirm before editing.

## Anti-patterns

- Do not hand-maintain a second list of surfaces; the inventory is authoritative.
- Do not infer a backend from the route name alone; confirm in the handler.
```

- [ ] **Step 7: Run the repo doc-state and lint gates**

Run: `cd go && gofmt -l ./internal/ask/catalog && go vet ./internal/ask/catalog && go test ./internal/ask/catalog -count=1`
Expected: `gofmt -l` prints nothing, `go vet` clean, tests PASS.

- [ ] **Step 8: Commit**

```bash
git add go/internal/ask/catalog/
git commit -m "feat(ask/catalog): coverage drift gate and package docs (#3250)"
```

---

## Self-Review

**Spec coverage (this plan's slice):** Implements spec §Architecture "`catalog`"
and §"Self-knowledge from the surface inventory" — generated spine, backend +
cost annotation, no hand-maintained list, drift-gated. The remaining spec
sections (`provider`, `engine`, `sandbox`, `render`, API/MCP surface,
observability/governance) are out of scope for plan 1 and covered by plans 2–7.

**Placeholder scan:** No "TBD"/"implement later". Task 5 Step 3 deliberately
defers the *enumeration* of overlay entries to the failing drift test (the test
prints the exact, authoritative list); the classification rule is fully
specified, which is the correct DRY approach rather than guessing the surface
set at plan-writing time.

**Type consistency:** `Backend`, `CostClass`, `SurfaceKind`, `Entry`, `Catalog`,
`Annotation` names and the method set (`Parse`, `Entries`, `Annotate`,
`Unannotated`, `Lookup`, `ByBackend`, `CheapestFirst`, `costRank`) are
consistent across all five tasks.

## Plans 2–7 (forthcoming, same grounded approach)

2. `provider` — tool-calling adapters over `semanticprofile` (+ `minimax` kind,
   `agent_reasoning` source class).
3. `engine` — bounded loop, Tier 1 canonical-route orchestration, narration gate.
4. `sandbox` — Tier 2 Cypher + SQL read-only validators, scope injection, cost gate.
5. `render` — output-format production + validation (mermaid/json/yaml/csv).
6. API routes (sync + SSE) + OpenAPI + `ask` MCP tool + `answer-narration` wiring.
7. Observability, governance policy wiring, Tier 2 security-review package.
