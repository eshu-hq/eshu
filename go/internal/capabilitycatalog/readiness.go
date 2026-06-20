package capabilitycatalog

// ReadinessLane is the canonical, static readiness classification of a platform
// surface (command, collector, reducer domain, API route, MCP tool, or console
// page). It is the declared lane an operator or contributor reads to know what a
// surface does today, independent of any single instance's live runtime health.
//
// The lanes are deliberately distinct from the runtime promotion states in
// internal/status (implemented, partial, failed, stale, gated, disabled,
// permission_hidden, unsupported): those describe one configured instance's
// observed health right now, while a ReadinessLane describes the surface's
// development maturity in source. The two agree on the shared vocabulary
// (implemented, partial, gated, unsupported) so docs, status, and this inventory
// never contradict each other.
type ReadinessLane string

const (
	// ReadinessImplemented marks a surface that is built, charted (where a chart
	// applies), and provable end to end. Claiming this lane asserts production
	// readiness, so it is the only lane that requires promotion proof.
	ReadinessImplemented ReadinessLane = "implemented"
	// ReadinessPartial marks a surface whose evidence exists but whose implemented
	// contract is unmet: readback pending, claims inactive, or a runtime-proof gap.
	ReadinessPartial ReadinessLane = "partial"
	// ReadinessGated marks a surface that is built but intentionally withheld from
	// a public lane pending a missing gate (a sanitized live smoke, a public
	// chart, or an operator opt-in).
	ReadinessGated ReadinessLane = "gated"
	// ReadinessFoundationOnly marks a surface with code structure but no hosted
	// runtime, claim-driven path, reducer projection, or chart yet.
	ReadinessFoundationOnly ReadinessLane = "foundation_only"
	// ReadinessFixtureOnly marks a surface proven only against fixtures; it never
	// reaches implemented without live provider proof.
	ReadinessFixtureOnly ReadinessLane = "fixture_only"
	// ReadinessResearchOnly marks a design- or research-only surface with no
	// production code lane.
	ReadinessResearchOnly ReadinessLane = "research_only"
	// ReadinessNotImplemented marks a surface that is declared or referenced but
	// has no implementation.
	ReadinessNotImplemented ReadinessLane = "not_implemented"
	// ReadinessUnsupported marks a known surface family with no configured or
	// shipped instance.
	ReadinessUnsupported ReadinessLane = "unsupported"
)

// readinessLanes is the closed set of valid lanes, ordered from most to least
// production-ready for deterministic enumeration.
var readinessLanes = []ReadinessLane{
	ReadinessImplemented,
	ReadinessPartial,
	ReadinessGated,
	ReadinessFoundationOnly,
	ReadinessFixtureOnly,
	ReadinessResearchOnly,
	ReadinessNotImplemented,
	ReadinessUnsupported,
}

// AllReadinessLanes returns every valid readiness lane in a stable order from
// most to least production-ready. It is the single source of truth for tooling,
// docs generation, and the surface inventory.
func AllReadinessLanes() []ReadinessLane {
	return append([]ReadinessLane(nil), readinessLanes...)
}

// Valid reports whether the lane is one of the closed set of readiness lanes.
func (l ReadinessLane) Valid() bool {
	for _, lane := range readinessLanes {
		if lane == l {
			return true
		}
	}
	return false
}

// RequiresPromotionProof reports whether claiming this lane asserts production
// readiness and therefore must be backed by linked promotion proof. Only the
// implemented lane makes that assertion; every other lane is honest about being
// not-yet-live and needs no proof. The collector promotion-proof CI gate keys on
// this so a doc cannot claim a collector is implemented without proof.
func (l ReadinessLane) RequiresPromotionProof() bool {
	return l == ReadinessImplemented
}
