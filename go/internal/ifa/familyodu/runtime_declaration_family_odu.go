// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package familyodu

import (
	"fmt"

	"github.com/eshu-hq/eshu-sdk/factschema"
	runtimedeclarationv1 "github.com/eshu-hq/eshu-sdk/factschema/runtimedeclaration/v1"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
)

const (
	// RuntimeDeclarationFamilyOduName is this Odù's catalog name, the ref a
	// materialized_edges:runtime_declaration coverage row would name to
	// resolve through it (#8816).
	RuntimeDeclarationFamilyOduName = "odu:ifa-runtime-declaration-family"

	// runtimeDeclarationFamilyScopeID and runtimeDeclarationFamilyGenerationID
	// are the single scope generation every fact belongs to: one full
	// generation of one repository, the shape the projection handler reads.
	runtimeDeclarationFamilyScopeID      = "git:repository:eshu-fixture-runtime-pins"
	runtimeDeclarationFamilyGenerationID = "gen-ifa-runtime-declaration-family-1"

	// runtimeDeclarationFamilyRepositoryID owns every declaration: the
	// DECLARES_RUNTIME edge source.
	runtimeDeclarationFamilyRepositoryID = "repository:eshu-fixture-runtime-pins"

	// runtimeDeclarationFamilySourceRunID is the collector run the
	// repository fact attributes itself to. Canonical projection requires
	// it on every repository fact (identity without it drops the phase
	// row, and the quiescence gate then holds every shared lane shut):
	// it mirrors the run-ifa-<family>-family-1 shape the git collector
	// emits, not a stable content hash.
	runtimeDeclarationFamilySourceRunID = "run-ifa-runtime-declaration-family-1"

	// runtimeDeclarationFamilyLocalPath is the fixture-unique checkout
	// path the projector falls back to for the Repository.path node
	// identity: every fixture repository must derive a distinct path or
	// the backend UNIQUE(path) constraint keeps exactly one of the
	// colliding nodes (#6228).
	runtimeDeclarationFamilyLocalPath = "/repo-runtime-pins"

	runtimeDeclarationFamilySchemaVersion = "1.0.0"
	runtimeDeclarationFamilyCollectorKind = "git"
)

// runtimeDeclarationFamilyFact describes one runtime_declaration.version fact
// in the Odù.
type runtimeDeclarationFamilyFact struct {
	Path       string
	Line       int
	Order      int
	SourceKind string
	Runtime    string
	RawSpec    string
	SpecKind   string
	Major      *int
	Tombstone  bool
}

func runtimeDeclarationFamilyMajor(v int) *int { return &v }

// runtimeDeclarationFamilyFacts holds four edge-producing declarations and
// three deliberate non-producers:
//
//   - positive: two tokens on one .tool-versions line (order 0 and 1, one node
//     each) and a .python-version pin;
//   - ambiguous: an .nvmrc alias (lts/iron), which is still a declaration and
//     projects a node, but with no version_major;
//   - negative: a tombstoned .ruby-version fact (no node), a fact whose
//     runtime is blank after trimming (it cannot name a node, so it is
//     dropped), and a replay of the .python-version fact under the same
//     five-part key (it collapses onto the existing node instead of adding
//     one).
//
// The projector trigger is a ninth fact outside this table (appended by
// RuntimeDeclarationFamilyOdu below): the collector's per-repository
// shared_followup marker, which the projector's generic per-fact intent path
// converts into the runtime_declaration_projection reducer intent. No
// scope-generation fanout builder names this family, so without the marker a
// live drive of this fixture enqueues no intent and materializes zero edges.
// The marker is transport for the drive, not truth for the guard:
// declaration.ExtractRows skips every fact that is not a
// runtime_declaration.version, so the expected-edge set is unaffected.
var runtimeDeclarationFamilyFacts = []runtimeDeclarationFamilyFact{
	{Path: ".tool-versions", Line: 1, Order: 0, SourceKind: "tool_versions", Runtime: "node", RawSpec: "20.11.1", SpecKind: runtimedeclarationv1.SpecKindExact, Major: runtimeDeclarationFamilyMajor(20)},
	{Path: ".tool-versions", Line: 1, Order: 1, SourceKind: "tool_versions", Runtime: "node", RawSpec: "18.19.0", SpecKind: runtimedeclarationv1.SpecKindExact, Major: runtimeDeclarationFamilyMajor(18)},
	{Path: ".python-version", Line: 1, Order: 0, SourceKind: "python_version", Runtime: "python", RawSpec: "3.12.1", SpecKind: runtimedeclarationv1.SpecKindExact, Major: runtimeDeclarationFamilyMajor(3)},
	{Path: "web/.nvmrc", Line: 1, Order: 0, SourceKind: "nvmrc", Runtime: "node", RawSpec: "lts/iron", SpecKind: runtimedeclarationv1.SpecKindAlias},
	{Path: ".ruby-version", Line: 1, Order: 0, SourceKind: "ruby_version", Runtime: "ruby", RawSpec: "3.3.0", SpecKind: runtimedeclarationv1.SpecKindExact, Major: runtimeDeclarationFamilyMajor(3), Tombstone: true},
	{Path: ".go-version", Line: 1, Order: 0, SourceKind: "go_version", Runtime: "   ", RawSpec: "1.22.5", SpecKind: runtimedeclarationv1.SpecKindExact, Major: runtimeDeclarationFamilyMajor(1)},
	{Path: ".python-version", Line: 1, Order: 0, SourceKind: "python_version", Runtime: "python", RawSpec: "3.12.1", SpecKind: runtimedeclarationv1.SpecKindExact, Major: runtimeDeclarationFamilyMajor(3)},
}

// RuntimeDeclarationFamilyOdu builds the cataloged Odù for the
// runtime_declaration direct-materialization family: one repository fact,
// seven runtime_declaration.version facts encoded through
// factschema.EncodeRuntimeDeclarationVersion (never hand-built maps), and the
// collector's shared_followup projector-trigger marker. It panics on an
// encode failure: that means the payload contract moved under a committed
// fixture, and every coverage claim built on it is already void.
func RuntimeDeclarationFamilyOdu() CatalogOdu {
	out := make([]facts.Envelope, 0, len(runtimeDeclarationFamilyFacts)+2)
	out = append(out, facts.Envelope{
		ScopeID:          runtimeDeclarationFamilyScopeID,
		GenerationID:     runtimeDeclarationFamilyGenerationID,
		FactKind:         factload.FactKindRepository,
		StableFactKey:    "repository:" + runtimeDeclarationFamilyRepositoryID,
		SchemaVersion:    runtimeDeclarationFamilySchemaVersion,
		CollectorKind:    runtimeDeclarationFamilyCollectorKind,
		SourceConfidence: facts.SourceConfidenceObserved,
		Payload: map[string]any{
			"graph_id":      runtimeDeclarationFamilyRepositoryID,
			"graph_kind":    "repository",
			"repo_id":       runtimeDeclarationFamilyRepositoryID,
			"name":          "runtime-pins",
			"local_path":    runtimeDeclarationFamilyLocalPath,
			"source_run_id": runtimeDeclarationFamilySourceRunID,
		},
	})
	for i, fact := range runtimeDeclarationFamilyFacts {
		payload, err := factschema.EncodeRuntimeDeclarationVersion(runtimedeclarationv1.Version{
			RepoID:       runtimeDeclarationFamilyRepositoryID,
			RelativePath: fact.Path,
			ScopeDir:     ".",
			LineNumber:   fact.Line,
			OrderIndex:   fact.Order,
			SourceKind:   fact.SourceKind,
			Runtime:      fact.Runtime,
			RawSpec:      fact.RawSpec,
			SpecKind:     fact.SpecKind,
			VersionMajor: fact.Major,
		})
		if err != nil {
			panic(fmt.Sprintf("familyodu: catalog_seed %s: encode runtime_declaration.version payload %d: %v",
				RuntimeDeclarationFamilyOduName, i, err))
		}
		out = append(out, facts.Envelope{
			ScopeID:      runtimeDeclarationFamilyScopeID,
			GenerationID: runtimeDeclarationFamilyGenerationID,
			FactKind:     facts.RuntimeDeclarationVersionFactKind,
			// The fact index keeps every stable key distinct, including the
			// deliberate same-identity replay.
			StableFactKey:    fmt.Sprintf("runtime_declaration:%s:%s:%d:%d:%d", runtimeDeclarationFamilyRepositoryID, fact.Path, fact.Line, fact.Order, i),
			SchemaVersion:    facts.RuntimeDeclarationSchemaVersionV1,
			CollectorKind:    runtimeDeclarationFamilyCollectorKind,
			SourceConfidence: facts.SourceConfidenceObserved,
			IsTombstone:      fact.Tombstone,
			Payload:          payload,
		})
	}
	// The collector emits this shared_followup marker unconditionally with
	// every repository snapshot, full or delta (see
	// collector/repo/git runtimeDeclarationProjectionFactEnvelope): it is what
	// a recorded generation of this scope really contains, and the projector's
	// generic per-fact path turns its reducer_domain payload into the
	// runtime_declaration_projection intent. Hand-built with literals, never
	// derived from the collector's builder: internal/ifa must not import
	// collector internals, and the lockstep test holds these bytes to the
	// committed cassette. The guard skips it (not a version fact).
	// Production envelopes carry no schema_version, but the replay cassette
	// format requires one on every fact, so the Odù pins 1.0.0 like every
	// sibling family cassette; a versionless marker fails cassette load.
	out = append(out, facts.Envelope{
		ScopeID:          runtimeDeclarationFamilyScopeID,
		GenerationID:     runtimeDeclarationFamilyGenerationID,
		FactKind:         "shared_followup",
		StableFactKey:    "shared_followup:" + runtimeDeclarationFamilyRepositoryID + ":runtime_declaration_projection",
		SchemaVersion:    runtimeDeclarationFamilySchemaVersion,
		CollectorKind:    runtimeDeclarationFamilyCollectorKind,
		SourceConfidence: facts.SourceConfidenceObserved,
		Payload: map[string]any{
			"reducer_domain": "runtime_declaration_projection",
			"entity_key":     "runtime_declaration:" + runtimeDeclarationFamilyRepositoryID,
			"reason":         "repository snapshot emitted runtime declaration follow-up",
			"repo_id":        runtimeDeclarationFamilyRepositoryID,
		},
	})
	return CatalogOdu{
		Odu: Odu{Name: RuntimeDeclarationFamilyOduName, Facts: out},
		Detail: "nine facts for the direct-materialization runtime_declaration family: one repository, seven runtime_declaration.version facts and the collector's shared_followup projector trigger — " +
			"two tokens on one .tool-versions line, a .python-version pin and an lts/iron alias project four DECLARES_RUNTIME edges, " +
			"while a tombstoned fact, a blank-runtime fact and a same-key replay project nothing extra",
	}
}
