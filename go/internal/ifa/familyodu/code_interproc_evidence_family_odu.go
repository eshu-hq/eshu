// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package familyodu

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/code"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	codedataflowv1 "github.com/eshu-hq/eshu/sdk/go/factschema/codedataflow/v1"
	codegraphv1 "github.com/eshu-hq/eshu/sdk/go/factschema/codegraph/v1"
)

// The code_interproc_evidence family Odù (#6228, under the #6181
// direct-materialization umbrella).
//
// A DIRECT-materialization family: the reducer writes it straight to
// cypher.CodeInterprocEvidenceWriter through the WriteCodeInterprocEvidence
// port. The relationship type is TAINT_FLOWS_TO, the single static token
// the canonical upsert template MERGEs; the MERGE keys on the two endpoint
// Function nodes plus the evidence_uid property (the edge is keyed on its
// evidence uid so a re-projection is idempotent and distinct flows between
// the same pair stay separate). Read it off the template and the
// CodeInterprocEvidenceUpsertCypher const, never by deriving from the port
// or family name.
//
// Facts are built as typed codedataflowv1.InterprocEvidence values and
// encoded through factschema.EncodeCodeInterprocEvidence, never as
// hand-built maps (Contract System v1).
//
// Scope note: cross-function findings arrive as code_interproc_evidence
// facts the git collector emits per resolved finding, so this Odù carries
// three of them: two live findings on distinct endpoint pairs (the two
// edges, one carrying a why-trail so the optional-row-key path is
// exercised) and one tombstoned finding for a deleted flow (the restraint:
// tombstones pass through the extractor to nothing, so a deleted finding
// never gains an edge).
//
// Node-substrate note (#6228 live leaf): the two live findings resolve
// their endpoints to the CanonicalEntityID Function uids the entities
// phase stamps, and the Odù carries the seven substrate facts that
// materialize those nodes -- see codeInterprocEvidenceFamilyNodeSubstrateFacts
// for why each one is load-bearing. The tombstoned finding keeps schematic
// legacy uids on purpose: a tombstone is skipped before decoding, so its
// endpoints never reach the extractor and no canonical form is observable
// through them.

const (
	// CodeInterprocEvidenceFamilyOduName is this Odù's catalog name, the
	// ref a materialized_edges:code_interproc_evidence coverage row would
	// name to resolve through it.
	CodeInterprocEvidenceFamilyOduName = "odu:ifa-code-interproc-evidence-family"

	// codeInterprocEvidenceFamilyScopeID is the single repository scope
	// every fact in this Odù belongs to. The reducer handler loads one
	// scope generation's facts, so a fixture spanning scopes would not
	// mirror any real intent.
	//
	// The scope is deliberately NOT any sibling cassette's scope.
	// Scopes carry one ACTIVE generation; driving a second generation into
	// an occupied scope supersedes the first family's generation, its
	// handler never runs, and the sibling exact-set assert fails with zero
	// edges. One live generation per scope per cell is the contract.
	codeInterprocEvidenceFamilyScopeID = "repo:eshu-fixture-code-interproc-evidence"

	// codeInterprocEvidenceFamilyGenerationID is the one scope generation
	// the Odù replays. The reducer's handler loads a single scope
	// generation's facts, so every fact below shares it.
	codeInterprocEvidenceFamilyGenerationID = "gen-ifa-code-interproc-evidence-family-1"

	// codeInterprocEvidenceFamilyRepoID is the owning repository: the
	// repo the collector's content facts stamp and the CanonicalEntityID
	// derivation hashes into every endpoint uid. The content_entity
	// payloads carry it explicitly, and the scope metadata repeats it (the
	// projector keys content materialization off the metadata; the
	// crossplane_satisfied_by scopes do the same).
	codeInterprocEvidenceFamilyRepoID = "repo-eshu-fixture-code-interproc-evidence"

	// codeInterprocEvidenceFamilyLocalPath is the repository's checkout
	// path the repository fact stamps. File node paths qualify against it,
	// so the entity rows and the file rows agree by construction.
	codeInterprocEvidenceFamilyLocalPath = "/repo-code-interproc-evidence"

	// codeInterprocEvidenceFamilySourceRunID is the collector run the
	// repository fact stamps. The live drain keys generation activation
	// off it (the derived_from live-found gap: a repository fact without
	// a source run leaves the drain waiting).
	codeInterprocEvidenceFamilySourceRunID = "run-ifa-code-interproc-evidence-family-1"

	// codeInterprocEvidenceFamilyCollectorKind mirrors what the git
	// collector stamps on this fact kind, so the Odù describes the same
	// envelopes a live generation would carry rather than agreeing only on
	// the payload.
	codeInterprocEvidenceFamilyCollectorKind = "git"

	// codeInterprocEvidenceFamilySourceConfidence marks these facts as
	// directly observed, the posture a collector-emitted finding carries.
	codeInterprocEvidenceFamilySourceConfidence = "observed"

	// codeInterprocEvidenceFamilySchemaVersion is the code_interproc_evidence
	// fact schema the payloads encode against.
	codeInterprocEvidenceFamilySchemaVersion = "1.0.0"
)

// codeInterprocEvidenceFamilyFunctionFixture describes one endpoint
// Function the entities phase materializes: a content_entity fact whose uid
// the projector recomputes as content.CanonicalEntityID (Function is in
// canonicalNamePathLineEntityLabels, so the incoming entity_id is ignored
// and the live findings must carry the recomputed id, not a schematic
// name).
type codeInterprocEvidenceFamilyFunctionFixture struct {
	// FunctionName is the endpoint function name, for evidence-row display
	// and the content_entity payload.
	FunctionName string
	// RelativePath is the repository-relative file the function lives in.
	RelativePath string
	// StartLine and EndLine bound the function in its file; the start line
	// feeds the CanonicalEntityID derivation.
	StartLine int
	EndLine   int
}

// codeInterprocEvidenceFamilyFunctions are the four live endpoint
// functions: authenticate and chargeCard in the auth session file,
// parseQuery and runQuery in the search query file. The legacy tombstone
// endpoints resolve to nothing on purpose and carry no function fixture.
var codeInterprocEvidenceFamilyFunctions = []codeInterprocEvidenceFamilyFunctionFixture{
	{
		FunctionName: "authenticate",
		RelativePath: "src/auth/session.go",
		StartLine:    12,
		EndLine:      20,
	},
	{
		FunctionName: "chargeCard",
		RelativePath: "src/auth/session.go",
		StartLine:    40,
		EndLine:      58,
	},
	{
		FunctionName: "parseQuery",
		RelativePath: "src/search/query.go",
		StartLine:    8,
		EndLine:      18,
	},
	{
		FunctionName: "runQuery",
		RelativePath: "src/search/query.go",
		StartLine:    30,
		EndLine:      44,
	},
}

// codeInterprocEvidenceFamilyFunctionUID returns the live graph uid of one
// endpoint function -- the CanonicalEntityID the writer's MATCH binds. It
// fails closed on an unknown name so a finding can never reference a
// function the fixture does not materialize.
func codeInterprocEvidenceFamilyFunctionUID(name string) (string, error) {
	for _, fixture := range codeInterprocEvidenceFamilyFunctions {
		if fixture.FunctionName != name {
			continue
		}
		return content.CanonicalEntityID(
			codeInterprocEvidenceFamilyRepoID,
			fixture.RelativePath,
			"Function",
			fixture.FunctionName,
			fixture.StartLine,
		), nil
	}
	return "", fmt.Errorf("familyodu: code_interproc_evidence fixture names unknown function %q", name)
}

// codeInterprocEvidenceFamilyFindingFixture describes one
// code_interproc_evidence fact in the Odù: a resolved cross-function
// value-flow finding the collector already joined to both Function entity
// uids.
type codeInterprocEvidenceFamilyFindingFixture struct {
	// SourceFunctionName and SinkFunctionName are the endpoint function
	// names. The builder resolves them to graph uids through
	// codeInterprocEvidenceFamilyFunctionUID, so the finding always names
	// nodes the fixture materializes.
	SourceFunctionName string
	SinkFunctionName   string
	// RelativePath is the repository-relative file path the finding was
	// observed in.
	RelativePath string
	// SinkKind is the finding's sink classification.
	SinkKind string
	// SourceKind is the finding's source classification.
	SourceKind string
	// Confidence is the parser's confidence score for the finding.
	Confidence float64
	// Cloud reports whether the flow crosses a cloud/runtime boundary.
	Cloud bool
	// TrailSourceFunctionName and TrailSinkFunctionName resolve the
	// why-trail step endpoints the same way the edge endpoints resolve.
	// Carried only on the finding that supplied a trail, proving the
	// optional why-trail row keys.
	TrailSourceFunctionName string
	TrailSinkFunctionName   string
	HasTrail                bool
	// Tombstone marks a deleted finding: the extractor skips tombstoned
	// envelopes before decoding, so it contributes no edge. Tombstone
	// endpoints stay schematic legacy uids (names included for
	// collector-shape fidelity): they never reach the extractor, so no
	// canonical form is observable through them.
	TombstoneSourceUID  string
	TombstoneSourceName string
	TombstoneSinkUID    string
	TombstoneSinkName   string
	Tombstone           bool
}

// codeInterprocEvidenceFamilyFindings are the fixture findings: two live
// findings on distinct endpoint pairs (the two TAINT_FLOWS_TO edges the
// expected set names) and one tombstoned finding (the restraint proving a
// deleted flow contributes nothing).
//
// The live findings differ in every uid input (endpoint uids plus kinds) so
// their edge uids cannot collide: two findings sharing the full uid input
// would converge on one stored edge while the extractor still emitted two
// rows, and the exact-set comparison counts by multiplicity.
var codeInterprocEvidenceFamilyFindings = []codeInterprocEvidenceFamilyFindingFixture{
	{
		// EDGE: a credential to payment-capture flow from authenticate
		// to chargeCard, with no trail.
		SourceFunctionName: "authenticate",
		SinkFunctionName:   "chargeCard",
		RelativePath:       "src/auth/session.go",
		SinkKind:           "payment_capture",
		SourceKind:         "credential",
		Confidence:         0.9,
	},
	{
		// EDGE: an http-request to search-exec flow from parseQuery to
		// runQuery, crossing a cloud boundary with a two-step trail,
		// proving the optional why-trail and cloud row keys.
		SourceFunctionName:      "parseQuery",
		SinkFunctionName:        "runQuery",
		RelativePath:            "src/search/query.go",
		SinkKind:                "search_exec",
		SourceKind:              "http_request",
		Confidence:              0.83,
		Cloud:                   true,
		TrailSourceFunctionName: "parseQuery",
		TrailSinkFunctionName:   "runQuery",
		HasTrail:                true,
	},
	{
		// NO EDGE: tombstoned finding for a deleted session-token flow
		// from oldLogin to oldCharge. The extractor skips tombstones
		// before decoding, so this fact reaches neither the rows nor the
		// quarantine count.
		RelativePath:        "src/legacy/auth.go",
		SinkKind:            "session_issue",
		SourceKind:          "credential",
		Confidence:          0.66,
		TombstoneSourceUID:  "func:legacy:oldLogin",
		TombstoneSourceName: "oldLogin",
		TombstoneSinkUID:    "func:legacy:oldCharge",
		TombstoneSinkName:   "oldCharge",
		Tombstone:           true,
	},
}

// codeInterprocEvidenceFamilyStableFactKey derives one fact's durable dedup
// key from the collector's own identity inputs (resolved endpoint uids plus
// kinds), in a readable fixture-local format rather than a hex digest,
// instead of hand-typing a string the expected-edge fixture cannot
// independently check. It takes resolved uids rather than fixture names so
// the tombstone key keeps the legacy endpoint uids -- the only identity
// that finding ever carried.
func codeInterprocEvidenceFamilyStableFactKey(sourceUID, sinkUID, sinkKind, sourceKind string) string {
	return fmt.Sprintf(
		"git:code_interproc_evidence:%s:%s:%s:%s",
		sourceUID,
		sinkUID,
		sinkKind,
		sourceKind,
	)
}

// codeInterprocEvidenceFamilyNodeSubstrateFacts builds the seven
// node-substrate envelopes the two live findings resolve against: the
// owning repository fact, one typed file fact per referenced path, and one
// content_entity fact per endpoint Function carrying the CanonicalEntityID
// the projector recomputes for Function labels (so the entities phase
// stamps exactly the uids the findings name).
//
// Repository nodes are what the entities phase hangs File nodes off (MATCH
// Repository first), and File nodes are what it hangs Function nodes off
// (MATCH File first) -- without them every statement completes having
// written nothing. ParsedFileData stays empty: the File node write needs
// path and repo identity, not parsed content. The source run id is
// load-bearing, not padding: the live drain keys generation activation off
// it (the derived_from live-found drain gap: a repository fact without a
// source run leaves the drain waiting).
func codeInterprocEvidenceFamilyNodeSubstrateFacts(scopeID, generationID string) []facts.Envelope {
	substrate := make([]facts.Envelope, 0, 7)
	substrate = append(substrate, facts.Envelope{
		ScopeID:          scopeID,
		GenerationID:     generationID,
		FactKind:         RepositoryFactKind,
		StableFactKey:    "repository:" + codeInterprocEvidenceFamilyRepoID,
		SchemaVersion:    codeInterprocEvidenceFamilySchemaVersion,
		CollectorKind:    codeInterprocEvidenceFamilyCollectorKind,
		SourceConfidence: codeInterprocEvidenceFamilySourceConfidence,
		Payload: map[string]any{
			"repo_id":       codeInterprocEvidenceFamilyRepoID,
			"source_run_id": codeInterprocEvidenceFamilySourceRunID,
			"local_path":    codeInterprocEvidenceFamilyLocalPath,
		},
	})
	seenFiles := map[string]bool{}
	for _, fixture := range codeInterprocEvidenceFamilyFunctions {
		if seenFiles[fixture.RelativePath] {
			continue
		}
		seenFiles[fixture.RelativePath] = true
		graphID := codeInterprocEvidenceFamilyRepoID + ":" + fixture.RelativePath
		graphKind := "file"
		language := "go"
		isDependency := false
		payload, err := factschema.EncodeCodegraphFile(codegraphv1.File{
			RepoID:         codeInterprocEvidenceFamilyRepoID,
			RelativePath:   fixture.RelativePath,
			ParsedFileData: map[string]any{},
			GraphID:        &graphID,
			GraphKind:      &graphKind,
			Language:       &language,
			IsDependency:   &isDependency,
		})
		if err != nil {
			panic(fmt.Sprintf("familyodu: encode code_interproc_evidence file %q: %v", fixture.RelativePath, err))
		}
		substrate = append(substrate, facts.Envelope{
			ScopeID:          scopeID,
			GenerationID:     generationID,
			FactKind:         FileFactKind,
			SchemaVersion:    codeInterprocEvidenceFamilySchemaVersion,
			StableFactKey:    "file:" + codeInterprocEvidenceFamilyRepoID + ":" + fixture.RelativePath,
			CollectorKind:    codeInterprocEvidenceFamilyCollectorKind,
			SourceConfidence: codeInterprocEvidenceFamilySourceConfidence,
			Payload:          payload,
		})
	}
	for _, fixture := range codeInterprocEvidenceFamilyFunctions {
		entityID := content.CanonicalEntityID(
			codeInterprocEvidenceFamilyRepoID,
			fixture.RelativePath,
			"Function",
			fixture.FunctionName,
			fixture.StartLine,
		)
		substrate = append(substrate, facts.Envelope{
			ScopeID:          scopeID,
			GenerationID:     generationID,
			FactKind:         ContentEntityFactKind,
			StableFactKey:    "content_entity:" + entityID,
			SchemaVersion:    codeInterprocEvidenceFamilySchemaVersion,
			CollectorKind:    codeInterprocEvidenceFamilyCollectorKind,
			SourceConfidence: codeInterprocEvidenceFamilySourceConfidence,
			Payload: map[string]any{
				"graph_id":      entityID,
				"graph_kind":    "content_entity",
				"entity_id":     entityID,
				"repo_id":       codeInterprocEvidenceFamilyRepoID,
				"relative_path": fixture.RelativePath,
				"entity_type":   "Function",
				"entity_name":   fixture.FunctionName,
				"start_line":    fixture.StartLine,
				"end_line":      fixture.EndLine,
				"language":      "go",
			},
		})
	}
	return substrate
}

// codeInterprocEvidenceFamilyFindingFact builds one code_interproc_evidence
// fact envelope, resolving endpoint names to the graph uids the fixture
// materializes. It panics on an encode failure for the same reason
// S3LogsToFamilyOdu does: a failure means the payload contract moved under
// a committed fixture, and every coverage claim built on it is already
// void.
func codeInterprocEvidenceFamilyFindingFact(scopeID, generationID string, fixture codeInterprocEvidenceFamilyFindingFixture) facts.Envelope {
	sourceUID := fixture.TombstoneSourceUID
	sinkUID := fixture.TombstoneSinkUID
	sourceName := fixture.TombstoneSourceName
	sinkName := fixture.TombstoneSinkName
	if !fixture.Tombstone {
		var err error
		sourceUID, err = codeInterprocEvidenceFamilyFunctionUID(fixture.SourceFunctionName)
		if err != nil {
			panic(err.Error())
		}
		sinkUID, err = codeInterprocEvidenceFamilyFunctionUID(fixture.SinkFunctionName)
		if err != nil {
			panic(err.Error())
		}
		sourceName = fixture.SourceFunctionName
		sinkName = fixture.SinkFunctionName
	}
	relativePath := fixture.RelativePath
	language := "go"
	sinkKind := fixture.SinkKind
	sourceKind := fixture.SourceKind
	confidence := fixture.Confidence
	evidence := codedataflowv1.InterprocEvidence{
		SourceFunctionUID:  sourceUID,
		SinkFunctionUID:    sinkUID,
		RelativePath:       &relativePath,
		SourceFunctionName: &sourceName,
		SinkFunctionName:   &sinkName,
		Language:           &language,
		SinkKind:           &sinkKind,
		SourceKind:         &sourceKind,
		Confidence:         &confidence,
	}
	if fixture.Cloud {
		cloud := true
		evidence.Cloud = &cloud
	}
	if fixture.HasTrail {
		trailSourceUID, err := codeInterprocEvidenceFamilyFunctionUID(fixture.TrailSourceFunctionName)
		if err != nil {
			panic(err.Error())
		}
		trailSinkUID, err := codeInterprocEvidenceFamilyFunctionUID(fixture.TrailSinkFunctionName)
		if err != nil {
			panic(err.Error())
		}
		evidence.WhyTrail = []map[string]any{
			{
				"role":          "source",
				"function_uid":  trailSourceUID,
				"function_name": fixture.TrailSourceFunctionName,
				"slot_kind":     "parameter",
				"slot_index":    0,
				"slot_name":     "rawQuery",
			},
			{
				"role":          "sink",
				"function_uid":  trailSinkUID,
				"function_name": fixture.TrailSinkFunctionName,
				"slot_kind":     "argument",
				"slot_index":    1,
				"slot_name":     "query",
			},
		}
	}
	payload, err := factschema.EncodeCodeInterprocEvidence(evidence)
	if err != nil {
		panic(fmt.Sprintf(
			"familyodu: catalog_seed %s: encode code_interproc_evidence payload for %q -> %q: %v",
			CodeInterprocEvidenceFamilyOduName, sourceUID, sinkUID, err,
		))
	}
	return facts.Envelope{
		ScopeID:          scopeID,
		GenerationID:     generationID,
		FactKind:         code.InterprocEvidenceFactKind,
		StableFactKey:    codeInterprocEvidenceFamilyStableFactKey(sourceUID, sinkUID, fixture.SinkKind, fixture.SourceKind),
		SchemaVersion:    codeInterprocEvidenceFamilySchemaVersion,
		CollectorKind:    codeInterprocEvidenceFamilyCollectorKind,
		SourceConfidence: codeInterprocEvidenceFamilySourceConfidence,
		IsTombstone:      fixture.Tombstone,
		Payload:          payload,
	}
}

// CodeInterprocEvidenceFamilyOdu builds the cataloged Odù for the
// code_interproc_evidence direct-materialization family.
//
// Exported because catalog_seed.go registers it at package-init time and
// materializededges' guard test resolves it by name. It panics on an encode
// failure for the same reason S3LogsToFamilyOdu does: a failure means the
// payload contract moved under a committed fixture, and every coverage claim
// built on it is already void.
func CodeInterprocEvidenceFamilyOdu() CatalogOdu {
	factsForOdu := codeInterprocEvidenceFamilyNodeSubstrateFacts(
		codeInterprocEvidenceFamilyScopeID, codeInterprocEvidenceFamilyGenerationID,
	)
	for _, fixture := range codeInterprocEvidenceFamilyFindings {
		factsForOdu = append(factsForOdu, codeInterprocEvidenceFamilyFindingFact(
			codeInterprocEvidenceFamilyScopeID, codeInterprocEvidenceFamilyGenerationID, fixture,
		))
	}

	return CatalogOdu{
		Odu:    Odu{Name: CodeInterprocEvidenceFamilyOduName, Facts: factsForOdu},
		Detail: "ten facts for the direct-materialization code_interproc_evidence family: one repository fact, two typed file facts, four content_entity facts materializing the endpoint Function nodes (the writer MATCHes Function nodes, so without them the statements complete having written nothing), two live code_interproc_evidence findings on distinct endpoint pairs (the two TAINT_FLOWS_TO edges, one carrying a why-trail) and one tombstoned finding for a deleted flow (the restraint proving tombstones contribute no edge and no quarantine)",
	}
}
