// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package familyodu

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/code"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	codedataflowv1 "github.com/eshu-hq/eshu/sdk/go/factschema/codedataflow/v1"
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

	// codeInterprocEvidenceFamilyCollectorKind mirrors what the git
	// collector stamps on this fact kind, so the Odù describes the same
	// envelopes a live generation would carry rather than agreeing only on
	// the payload.
	codeInterprocEvidenceFamilyCollectorKind = "git"

	// codeInterprocEvidenceFamilySourceConfidence marks these facts as
	// directly observed, the posture a collector-emitted finding carries.
	codeInterprocEvidenceFamilySourceConfidence = "observed"

	// codeInterprocEvidenceFamilySchemaVersion is the
	// code_interproc_evidence fact schema the payloads encode against.
	codeInterprocEvidenceFamilySchemaVersion = "1.0.0"
)

// codeInterprocEvidenceFamilyFindingFixture describes one
// code_interproc_evidence fact in the Odù: a resolved cross-function
// value-flow finding the collector already joined to both Function entity
// uids.
type codeInterprocEvidenceFamilyFindingFixture struct {
	// SourceFunctionUID and SinkFunctionUID are the graph Function entity
	// uids of the finding's taint source and sink. Required by the payload
	// contract: they are the TAINT_FLOWS_TO edge's two endpoints.
	SourceFunctionUID string
	SinkFunctionUID   string
	// SourceFunctionName and SinkFunctionName are the endpoint function
	// names, for evidence-row display.
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
	// WhyTrail is the parser's step-by-step explanation of the flow.
	// Carried only where the parser supplied one, proving the optional
	// why-trail row keys.
	WhyTrail []map[string]any
	// Tombstone marks a deleted finding: the extractor skips tombstoned
	// envelopes before decoding, so it contributes no edge.
	Tombstone bool
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
		SourceFunctionUID:  "func:auth:authenticate",
		SinkFunctionUID:    "func:billing:chargeCard",
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
		SourceFunctionUID:  "func:search:parseQuery",
		SinkFunctionUID:    "func:search:runQuery",
		SourceFunctionName: "parseQuery",
		SinkFunctionName:   "runQuery",
		RelativePath:       "src/search/query.go",
		SinkKind:           "search_exec",
		SourceKind:         "http_request",
		Confidence:         0.83,
		Cloud:              true,
		WhyTrail: []map[string]any{
			{
				"role":          "source",
				"function_uid":  "func:search:parseQuery",
				"function_name": "parseQuery",
				"slot_kind":     "parameter",
				"slot_index":    0,
				"slot_name":     "rawQuery",
			},
			{
				"role":          "sink",
				"function_uid":  "func:search:runQuery",
				"function_name": "runQuery",
				"slot_kind":     "argument",
				"slot_index":    1,
				"slot_name":     "query",
			},
		},
	},
	{
		// NO EDGE: tombstoned finding for a deleted session-token flow
		// from oldLogin to oldCharge. The extractor skips tombstones
		// before decoding, so this fact reaches neither the rows nor
		// the quarantine count.
		SourceFunctionUID:  "func:legacy:oldLogin",
		SinkFunctionUID:    "func:legacy:oldCharge",
		SourceFunctionName: "oldLogin",
		SinkFunctionName:   "oldCharge",
		RelativePath:       "src/legacy/auth.go",
		SinkKind:           "session_issue",
		SourceKind:         "credential",
		Confidence:         0.66,
		Tombstone:          true,
	},
}

// codeInterprocEvidenceFamilyStableFactKey derives one fact's durable dedup
// key from the collector's own identity inputs (endpoint uids plus kinds),
// in a readable fixture-local format rather than a hex digest, instead of
// hand-typing a string the expected-edge fixture cannot independently
// check.
func codeInterprocEvidenceFamilyStableFactKey(fixture codeInterprocEvidenceFamilyFindingFixture) string {
	return fmt.Sprintf(
		"git:code_interproc_evidence:%s:%s:%s:%s",
		fixture.SourceFunctionUID,
		fixture.SinkFunctionUID,
		fixture.SinkKind,
		fixture.SourceKind,
	)
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
	factsForOdu := make([]facts.Envelope, 0, len(codeInterprocEvidenceFamilyFindings))
	for _, fixture := range codeInterprocEvidenceFamilyFindings {
		relativePath := fixture.RelativePath
		sourceFunctionName := fixture.SourceFunctionName
		sinkFunctionName := fixture.SinkFunctionName
		language := "go"
		sinkKind := fixture.SinkKind
		sourceKind := fixture.SourceKind
		confidence := fixture.Confidence
		evidence := codedataflowv1.InterprocEvidence{
			SourceFunctionUID:  fixture.SourceFunctionUID,
			SinkFunctionUID:    fixture.SinkFunctionUID,
			RelativePath:       &relativePath,
			SourceFunctionName: &sourceFunctionName,
			SinkFunctionName:   &sinkFunctionName,
			Language:           &language,
			SinkKind:           &sinkKind,
			SourceKind:         &sourceKind,
			Confidence:         &confidence,
			WhyTrail:           fixture.WhyTrail,
		}
		if fixture.Cloud {
			cloud := true
			evidence.Cloud = &cloud
		}
		payload, err := factschema.EncodeCodeInterprocEvidence(evidence)
		if err != nil {
			panic(fmt.Sprintf(
				"familyodu: catalog_seed %s: encode code_interproc_evidence payload for %q -> %q: %v",
				CodeInterprocEvidenceFamilyOduName, fixture.SourceFunctionUID, fixture.SinkFunctionUID, err,
			))
		}
		factsForOdu = append(factsForOdu, facts.Envelope{
			ScopeID:          codeInterprocEvidenceFamilyScopeID,
			GenerationID:     codeInterprocEvidenceFamilyGenerationID,
			FactKind:         code.InterprocEvidenceFactKind,
			StableFactKey:    codeInterprocEvidenceFamilyStableFactKey(fixture),
			SchemaVersion:    codeInterprocEvidenceFamilySchemaVersion,
			CollectorKind:    codeInterprocEvidenceFamilyCollectorKind,
			SourceConfidence: codeInterprocEvidenceFamilySourceConfidence,
			IsTombstone:      fixture.Tombstone,
			Payload:          payload,
		})
	}

	return CatalogOdu{
		Odu:    Odu{Name: CodeInterprocEvidenceFamilyOduName, Facts: factsForOdu},
		Detail: "three facts for the direct-materialization code_interproc_evidence family: two live code_interproc_evidence findings on distinct endpoint pairs (the two TAINT_FLOWS_TO edges, one carrying a why-trail) and one tombstoned finding for a deleted flow (the restraint proving tombstones contribute no edge and no quarantine)",
	}
}
