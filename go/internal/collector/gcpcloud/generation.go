package gcpcloud

import (
	"errors"
	"sort"
	"sync"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

// ErrStaleGeneration reports that a generation was rejected because a newer
// generation (higher fencing token) already owns the scope. Callers classify it
// as terminal so a stale scan does not replace current facts or readiness.
var ErrStaleGeneration = errors.New("gcp generation rejected by newer fencing token")

// Generation accumulates resource observations and collection warnings for one
// bounded GCP collector scan, then builds a deterministic, deduplicated set of
// source-fact envelopes. It is the fixture-driven seam: callers parse CAI pages
// and feed AddPage/AddWarning, then Build commits a stable generation.
//
// Generation is not safe for concurrent use; one claim worker owns one
// Generation. Cross-scope ownership and fencing live in GenerationTracker.
type Generation struct {
	boundary  Boundary
	key       redact.Key
	resources map[string]ResourceObservation
	warnings  []WarningObservation
	pageCount int
}

// NewGeneration creates a generation accumulator bound to one claim boundary and
// redaction key.
func NewGeneration(boundary Boundary, key redact.Key) *Generation {
	return &Generation{
		boundary:  boundary,
		key:       key,
		resources: make(map[string]ResourceObservation),
	}
}

// AddPage adds one parsed page of resource observations. Duplicate deliveries of
// the same resource within a generation converge: the resource is keyed by its
// full resource name plus asset type, so re-adding the same page does not
// produce duplicate facts. AddPage increments the page count for telemetry.
func (g *Generation) AddPage(resources []ResourceObservation) error {
	g.pageCount++
	for _, obs := range resources {
		key := resourceDedupeKey(obs)
		if key == "" {
			return errors.New("gcp resource observation missing full_resource_name")
		}
		g.resources[key] = obs
	}
	return nil
}

// AddWarning records one explicit collection warning for the generation.
func (g *Generation) AddWarning(obs WarningObservation) {
	g.warnings = append(g.warnings, obs)
}

// Build produces the deterministic, sorted envelope set for the generation.
// Resource facts are sorted by stable fact key so re-emitting the same
// generation yields the same order; warning facts follow in insertion order.
func (g *Generation) Build() ([]facts.Envelope, error) {
	keys := make([]string, 0, len(g.resources))
	for key := range g.resources {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	envelopes := make([]facts.Envelope, 0, len(g.resources)+len(g.warnings))
	for _, key := range keys {
		env, err := NewCloudResourceEnvelope(g.boundary, g.resources[key], g.key)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, env)
	}
	for _, warning := range g.warnings {
		env, err := NewCollectionWarningEnvelope(warning)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, env)
	}
	return envelopes, nil
}

// Boundary returns the claim boundary the generation was created with. The
// runtime uses it to build warning observations (for example page-token
// expiry) that share the generation's scope and fencing identity.
func (g *Generation) Boundary() Boundary { return g.boundary }

// PageCount returns the number of pages added to the generation.
func (g *Generation) PageCount() int { return g.pageCount }

// ResourceCount returns the number of deduplicated resources in the generation.
func (g *Generation) ResourceCount() int { return len(g.resources) }

// WarningCount returns the number of collection warnings in the generation.
func (g *Generation) WarningCount() int { return len(g.warnings) }

func resourceDedupeKey(obs ResourceObservation) string {
	name := obs.Name
	if name == "" {
		return ""
	}
	return name + "\x00" + obs.AssetType
}

// GenerationTracker enforces per-scope fencing so a stale generation cannot
// replace current facts. It records the highest accepted fencing token per
// scope and rejects any lower token with ErrStaleGeneration while allowing
// idempotent re-acceptance of the current token. It is safe for concurrent use.
type GenerationTracker struct {
	mu      sync.Mutex
	current map[string]int64
}

// NewGenerationTracker creates an empty generation tracker.
func NewGenerationTracker() *GenerationTracker {
	return &GenerationTracker{current: make(map[string]int64)}
}

// Accept records an attempt to commit a generation for a scope at a fencing
// token. It returns ErrStaleGeneration when a strictly higher token already owns
// the scope. Re-accepting the current token is idempotent; a higher token
// advances the scope. The generationID is accepted for caller diagnostics and
// future per-generation tracking; fencing is by token.
func (t *GenerationTracker) Accept(scopeID, generationID string, fencingToken int64) error {
	_ = generationID
	t.mu.Lock()
	defer t.mu.Unlock()
	if current, ok := t.current[scopeID]; ok && fencingToken < current {
		return ErrStaleGeneration
	}
	t.current[scopeID] = fencingToken
	return nil
}

// Current returns the highest accepted fencing token for a scope and whether the
// scope has been seen.
func (t *GenerationTracker) Current(scopeID string) (int64, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	token, ok := t.current[scopeID]
	return token, ok
}
