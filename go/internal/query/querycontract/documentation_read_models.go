// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import "time"

// The documentation read models and their filters live here rather than in
// package query because the shared ContentStore double that answers them has
// to be constructible from outside package query (#6060, epic #6053). A Go
// _test.go symbol cannot cross a package boundary, and neither can an
// unexported one, so a fake whose method signatures name a package-query
// unexported read model can never move. Package query keeps an unexported
// alias for each type, so its own call sites read exactly as they did before.
//
// These are read models, not wire types: the handlers project them into
// response maps. Struct tags that appear here are the ones the coverage block
// is serialized with directly.

// DocumentationFindingFilter selects documentation findings for one listing.
//
// AllowedScopeIDs and AllowedRepositoryIDs carry the caller's grants. A scoped
// caller with neither is short-circuited to an empty page by the handler
// before the store is reached, so an empty pair here means "unscoped", never
// "scoped but entitled to nothing".
type DocumentationFindingFilter struct {
	ScopeID              string
	GenerationID         string
	Repository           string
	TargetKind           string
	TargetID             string
	ServiceID            string
	FindingType          string
	SourceID             string
	DocumentID           string
	Status               string
	TruthLevel           string
	FreshnessState       string
	UpdatedSince         *time.Time
	Limit                int
	Cursor               string
	Offset               int
	AllowedScopeIDs      []string
	AllowedRepositoryIDs []string
}

// DocumentationFindingListReadModel is one page of documentation findings plus
// the target readback that explains what the page covers.
//
// Coverage, RelatedFacts, and MissingEvidence are populated only for a
// target-anchored query. An unanchored listing leaves them zero, and the
// response omits the whole readback block rather than emitting empty fields.
type DocumentationFindingListReadModel struct {
	Findings        []map[string]any
	NextCursor      string
	RelatedFacts    []map[string]any
	Coverage        DocumentationTargetCoverage
	MissingEvidence []DocumentationMissingEvidence
}

// DocumentationFactFilter selects documentation facts for one listing.
//
// A listing needs a scope or an anchor; the handler rejects a filter carrying
// neither before it reaches the store.
type DocumentationFactFilter struct {
	FactKind             string
	ScopeID              string
	GenerationID         string
	Repository           string
	TargetKind           string
	TargetID             string
	ServiceID            string
	SourceID             string
	DocumentID           string
	SectionID            string
	Query                string
	UpdatedSince         *time.Time
	Limit                int
	Cursor               string
	Offset               int
	AllowedScopeIDs      []string
	AllowedRepositoryIDs []string
}

// DocumentationFactListReadModel is one page of documentation facts.
type DocumentationFactListReadModel struct {
	Facts      []map[string]any
	NextCursor string
}

// DocumentationEvidencePacketReadModel carries one documentation evidence
// packet.
//
// Available and Denied are distinct states and callers must keep them
// distinct: a missing packet is a 404, a packet the caller's grants exclude is
// a denial carrying DeniedReason. Collapsing the two would tell a scoped
// caller a packet does not exist when it does.
type DocumentationEvidencePacketReadModel struct {
	Available    bool
	Denied       bool
	DeniedReason string
	Packet       map[string]any
}

// DocumentationEvidencePacketFreshnessReadModel reports whether a saved packet
// version is still current.
//
// It carries the same Available/Denied split as
// DocumentationEvidencePacketReadModel, for the same reason.
type DocumentationEvidencePacketFreshnessReadModel struct {
	Available           bool
	Denied              bool
	DeniedReason        string
	PacketID            string `json:"packet_id"`
	PacketVersion       string `json:"packet_version"`
	FreshnessState      string `json:"freshness_state"`
	LatestPacketVersion string `json:"latest_packet_version"`
}

// DocumentationEvidencePacketFilter fetches one packet under the caller's
// grants.
type DocumentationEvidencePacketFilter struct {
	FindingID            string
	AllowedRepositoryIDs []string
	AllowedScopeIDs      []string
}

// DocumentationEvidencePacketFreshnessFilter checks one saved packet version
// under the caller's grants.
type DocumentationEvidencePacketFreshnessFilter struct {
	PacketID             string
	SavedPacketVersion   string
	AllowedRepositoryIDs []string
	AllowedScopeIDs      []string
}

// DocumentationTargetScope names what a documentation readback is anchored to.
type DocumentationTargetScope struct {
	Repository string `json:"repository,omitempty"`
	TargetKind string `json:"target_kind,omitempty"`
	TargetID   string `json:"target_id,omitempty"`
	ServiceID  string `json:"service_id,omitempty"`
}

// DocumentationTargetCoverage reports what the readback found for its target.
//
// Truncated says the fact preview was cut at its limit, so a caller reading
// TargetFactCount against the previewed rows does not mistake the shorter list
// for the whole set.
type DocumentationTargetCoverage struct {
	Target              DocumentationTargetScope `json:"target,omitempty"`
	FindingsReturned    int                      `json:"findings_returned"`
	TargetFactCount     int                      `json:"target_fact_count"`
	TargetFactKinds     map[string]int           `json:"target_fact_kinds,omitempty"`
	SourceOnlyCount     int                      `json:"source_only_count,omitempty"`
	SourceOnlyFactKinds map[string]int           `json:"source_only_fact_kinds,omitempty"`
	Truncated           bool                     `json:"truncated"`
}

// DocumentationMissingEvidence names one reason a target readback came back
// thin, so the response says why instead of just showing fewer rows.
type DocumentationMissingEvidence struct {
	Reason string `json:"reason"`
	Detail string `json:"detail,omitempty"`
}
