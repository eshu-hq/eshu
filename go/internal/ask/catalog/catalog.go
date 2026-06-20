package catalog

import (
	"encoding/json"
	"fmt"
	"sort"
)

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

// inventoryEnvelope mirrors the generated surface-inventory artifact. Only the
// fields the catalog needs are decoded; the artifact may carry more.
type inventoryEnvelope struct {
	Version  string          `json:"version"`
	Surfaces []surfaceRecord `json:"surfaces"`
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
	return missing
}

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
