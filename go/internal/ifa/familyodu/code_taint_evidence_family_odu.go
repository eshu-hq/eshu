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

// The code_taint_evidence family Odù (#6228, under the #6181
// direct-materialization umbrella).
//
// A DIRECT-materialization family: the reducer writes it straight to
// cypher.CodeTaintEvidenceWriter through the WriteCodeTaintEvidence port. The
// relationship type is HAS_TAINT_EVIDENCE, the single static token the
// canonical upsert template MERGEs; the MERGE keys on its two endpoint nodes
// alone (the Function is MATCHed, the CodeTaintEvidence node is MERGEd on its
// uid). Read it off the template and the CodeTaintEvidenceUpsertCypher const,
// never by deriving from the port or family name.
//
// Facts are built as typed codedataflowv1.TaintEvidence values and encoded
// through factschema.EncodeCodeTaintEvidence, never as hand-built maps
// (Contract System v1).
//
// Scope note: taint findings arrive as code_taint_evidence facts the git
// collector emits per resolved finding, so this Odù carries three of them:
// two live findings on distinct functions (the two edges) and one tombstoned
// finding for a deleted flow (the restraint: tombstones pass through the
// extractor to nothing, so a deleted finding never gains an edge).

const (
	// CodeTaintEvidenceFamilyOduName is this Odù's catalog name, the ref a
	// materialized_edges:code_taint_evidence coverage row would name to
	// resolve through it.
	CodeTaintEvidenceFamilyOduName = "odu:ifa-code-taint-evidence-family"

	// codeTaintEvidenceFamilyScopeID is the single repository scope every
	// fact in this Odù belongs to. The reducer handler loads one scope
	// generation's facts, so a fixture spanning scopes would not mirror any
	// real intent.
	//
	// The scope is deliberately NOT any sibling cassette's scope.
	// Scopes carry one ACTIVE generation; driving a second generation into
	// an occupied scope supersedes the first family's generation, its
	// handler never runs, and the sibling exact-set assert fails with zero
	// edges. One live generation per scope per cell is the contract.
	codeTaintEvidenceFamilyScopeID = "repo:eshu-fixture-code-taint-evidence"

	// codeTaintEvidenceFamilyGenerationID is the one scope generation the
	// Odù replays. The reducer's handler loads a single scope generation's
	// facts, so every fact below shares it.
	codeTaintEvidenceFamilyGenerationID = "gen-ifa-code-taint-evidence-family-1"

	// codeTaintEvidenceFamilyCollectorKind mirrors what the git collector
	// stamps on this fact kind, so the Odù describes the same envelopes a
	// live generation would carry rather than agreeing only on the payload.
	codeTaintEvidenceFamilyCollectorKind = "git"

	// codeTaintEvidenceFamilySourceConfidence marks these facts as directly
	// observed, the posture a collector-emitted finding carries.
	codeTaintEvidenceFamilySourceConfidence = "observed"

	// codeTaintEvidenceFamilySchemaVersion is the code_taint_evidence fact
	// schema the payloads encode against.
	codeTaintEvidenceFamilySchemaVersion = "1.0.0"
)

// codeTaintEvidenceFamilyFindingFixture describes one code_taint_evidence
// fact in the Odù: a resolved intraprocedural value-flow finding the
// collector already joined to its Function entity uid.
type codeTaintEvidenceFamilyFindingFixture struct {
	// FunctionUID is the graph Function entity uid the finding attaches
	// to. Required by the payload contract: a finding with no function to
	// attach to is not a projectable fact.
	FunctionUID string
	// FunctionName is the finding's function name, for evidence-row
	// display.
	FunctionName string
	// RelativePath is the repository-relative file path the finding was
	// observed in.
	RelativePath string
	// Kind is the finding's taint-rule kind.
	Kind string
	// SinkKind is the finding's sink classification.
	SinkKind string
	// SourceKind is the finding's source classification.
	SourceKind string
	// Binding is the finding's variable/parameter binding description.
	Binding string
	// SourceLine and SinkLine are the 1-based taint source/sink lines.
	SourceLine int
	SinkLine   int
	// Confidence is the parser's confidence score for the finding.
	Confidence float64
	// Tombstone marks a deleted finding: the extractor skips tombstoned
	// envelopes before decoding, so it contributes no edge.
	Tombstone bool
}

// codeTaintEvidenceFamilyFindings are the fixture findings: two live
// findings on distinct functions (the two HAS_TAINT_EVIDENCE edges the
// expected set names) and one tombstoned finding (the restraint proving a
// deleted flow contributes nothing).
//
// The live findings differ in every uid input (function uid, lines, kinds,
// binding) so their edge uids cannot collide: two findings sharing the full
// uid input would converge on one stored edge while the extractor still
// emitted two rows, and the exact-set comparison counts by multiplicity.
var codeTaintEvidenceFamilyFindings = []codeTaintEvidenceFamilyFindingFixture{
	{
		// EDGE: an http-request to sql-exec flow in handlePayment.
		FunctionUID:  "func:payments:handlePayment",
		FunctionName: "handlePayment",
		RelativePath: "src/payments/handler.go",
		Kind:         "sql_injection",
		SinkKind:     "sql_exec",
		SourceKind:   "http_request",
		Binding:      "orderID",
		SourceLine:   12,
		SinkLine:     18,
		Confidence:   0.92,
	},
	{
		// EDGE: a queue-message to command-exec flow in refundPayment.
		FunctionUID:  "func:payments:refundPayment",
		FunctionName: "refundPayment",
		RelativePath: "src/payments/refund.go",
		Kind:         "command_injection",
		SinkKind:     "command_exec",
		SourceKind:   "queue_message",
		Binding:      "payload",
		SourceLine:   31,
		SinkLine:     40,
		Confidence:   0.87,
	},
	{
		// NO EDGE: tombstoned finding for a deleted cross-site-scripting
		// flow in voidPayment. The extractor skips tombstones before
		// decoding, so this fact reaches neither the rows nor the
		// quarantine count.
		FunctionUID:  "func:payments:voidPayment",
		FunctionName: "voidPayment",
		RelativePath: "src/payments/void.go",
		Kind:         "cross_site_scripting",
		SinkKind:     "html_sink",
		SourceKind:   "http_request",
		Binding:      "comment",
		SourceLine:   7,
		SinkLine:     9,
		Confidence:   0.71,
		Tombstone:    true,
	},
}

// codeTaintEvidenceFamilyStableFactKey derives one fact's durable dedup key
// from the collector's own identity inputs (function uid plus source/sink
// lines plus kinds plus binding), in a readable fixture-local format rather
// than a hex digest, instead of hand-typing a string the expected-edge
// fixture cannot independently check.
func codeTaintEvidenceFamilyStableFactKey(fixture codeTaintEvidenceFamilyFindingFixture) string {
	return fmt.Sprintf(
		"git:code_taint_evidence:%s:%d:%d:%s:%s:%s",
		fixture.FunctionUID,
		fixture.SourceLine,
		fixture.SinkLine,
		fixture.SinkKind,
		fixture.SourceKind,
		fixture.Binding,
	)
}

// CodeTaintEvidenceFamilyOdu builds the cataloged Odù for the
// code_taint_evidence direct-materialization family.
//
// Exported because catalog_seed.go registers it at package-init time and
// materializededges' guard test resolves it by name. It panics on an encode
// failure for the same reason S3LogsToFamilyOdu does: a failure means the
// payload contract moved under a committed fixture, and every coverage claim
// built on it is already void.
func CodeTaintEvidenceFamilyOdu() CatalogOdu {
	factsForOdu := make([]facts.Envelope, 0, len(codeTaintEvidenceFamilyFindings))
	for _, fixture := range codeTaintEvidenceFamilyFindings {
		relativePath := fixture.RelativePath
		functionName := fixture.FunctionName
		language := "go"
		kind := fixture.Kind
		sinkKind := fixture.SinkKind
		sourceKind := fixture.SourceKind
		binding := fixture.Binding
		sourceLine := fixture.SourceLine
		sinkLine := fixture.SinkLine
		confidence := fixture.Confidence
		payload, err := factschema.EncodeCodeTaintEvidence(codedataflowv1.TaintEvidence{
			FunctionUID:  fixture.FunctionUID,
			RelativePath: &relativePath,
			FunctionName: &functionName,
			Language:     &language,
			Kind:         &kind,
			SinkKind:     &sinkKind,
			SourceKind:   &sourceKind,
			Binding:      &binding,
			SourceLine:   &sourceLine,
			SinkLine:     &sinkLine,
			Confidence:   &confidence,
		})
		if err != nil {
			panic(fmt.Sprintf(
				"familyodu: catalog_seed %s: encode code_taint_evidence payload for %q: %v",
				CodeTaintEvidenceFamilyOduName, fixture.FunctionUID, err,
			))
		}
		factsForOdu = append(factsForOdu, facts.Envelope{
			ScopeID:          codeTaintEvidenceFamilyScopeID,
			GenerationID:     codeTaintEvidenceFamilyGenerationID,
			FactKind:         code.TaintEvidenceFactKind,
			StableFactKey:    codeTaintEvidenceFamilyStableFactKey(fixture),
			SchemaVersion:    codeTaintEvidenceFamilySchemaVersion,
			CollectorKind:    codeTaintEvidenceFamilyCollectorKind,
			SourceConfidence: codeTaintEvidenceFamilySourceConfidence,
			IsTombstone:      fixture.Tombstone,
			Payload:          payload,
		})
	}

	return CatalogOdu{
		Odu:    Odu{Name: CodeTaintEvidenceFamilyOduName, Facts: factsForOdu},
		Detail: "three facts for the direct-materialization code_taint_evidence family: two live code_taint_evidence findings on distinct functions (the two HAS_TAINT_EVIDENCE edges) and one tombstoned finding for a deleted flow (the restraint proving tombstones contribute no edge and no quarantine)",
	}
}
