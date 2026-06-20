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
