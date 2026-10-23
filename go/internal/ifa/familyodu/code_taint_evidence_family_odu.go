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
//
// Node-substrate note (#6228 live leaf): the two live findings resolve
// their endpoints to the CanonicalEntityID Function uids the entities
// phase stamps, and the Odù carries the five substrate facts that
// materialize those nodes -- see
// codeTaintEvidenceFamilyNodeSubstrateFacts for why each one is
// load-bearing. Canonical projection recomputes the graph uid for Function
// labels (DiscardsIncomingEntityID), ignoring the finding's entity_id, so
// schematic uids like the pre-live fixture's func:payments:* names would
// stamp no node the writer's MATCH can hit and the live drive would
// materialize zero edges. The tombstoned finding keeps schematic legacy
// uids on purpose: a tombstone is skipped before decoding, so its endpoint
// never reaches the extractor and no canonical form is observable through
// it.

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

	// codeTaintEvidenceFamilyRepoID is the owning repository: the
	// repo the collector's content facts stamp and the CanonicalEntityID
	// derivation hashes into every endpoint uid. The content_entity
	// payloads carry it explicitly, and the scope metadata repeats it (the
	// projector keys content materialization off the metadata; the
	// crossplane_satisfied_by scopes do the same).
	codeTaintEvidenceFamilyRepoID = "repo-eshu-fixture-code-taint-evidence"

	// codeTaintEvidenceFamilyLocalPath is the repository's checkout
	// path the repository fact stamps. File node paths qualify against it,
	// so the entity rows and the file rows agree by construction. It is
	// load-bearing, not padding: Repository.path carries a global UNIQUE
	// constraint and the codegraph extractor falls back to local_path for
	// it, so a path-less repository fact collides with every other
	// path-less repository in the shared determinism cell (the
	// derived_from live-found UNIQUE path fix).
	codeTaintEvidenceFamilyLocalPath = "/repo-code-taint-evidence"

	// codeTaintEvidenceFamilySourceRunID is the collector run the
	// repository fact stamps. The live drain keys generation activation
	// off it (the derived_from live-found drain gap: a repository fact
	// without a source run leaves the drain waiting).
	codeTaintEvidenceFamilySourceRunID = "run-ifa-code-taint-evidence-family-1"

	// codeTaintEvidenceFamilyCollectorKind mirrors what the git collector
	// stamps on this fact kind, so the Odù describes the same envelopes a
	// live generation would carry rather than agreeing only on the
	// payload.
	codeTaintEvidenceFamilyCollectorKind = "git"

	// codeTaintEvidenceFamilySourceConfidence marks these facts as directly
	// observed, the posture a collector-emitted finding carries.
	codeTaintEvidenceFamilySourceConfidence = "observed"

	// codeTaintEvidenceFamilySchemaVersion is the code_taint_evidence fact
	// schema the payloads encode against.
	codeTaintEvidenceFamilySchemaVersion = "1.0.0"
)

// codeTaintEvidenceFamilyFunctionFixture describes one endpoint
// Function the entities phase materializes: a content_entity fact whose uid
// the projector recomputes as content.CanonicalEntityID (Function is in
// canonicalNamePathLineEntityLabels, so the incoming entity_id is ignored
// and the live findings must carry the recomputed id, not a schematic
// name).
type codeTaintEvidenceFamilyFunctionFixture struct {
	// FunctionName is the endpoint function name, for evidence-row
	// display and the content_entity payload.
	FunctionName string
	// RelativePath is the repository-relative file the function lives in.
	RelativePath string
	// StartLine and EndLine bound the function in its file; the start line
	// feeds the CanonicalEntityID derivation.
	StartLine int
	EndLine   int
}

// codeTaintEvidenceFamilyFunctions are the two live endpoint functions:
// handlePayment in the payments handler file and refundPayment in the
// refund file. The tombstoned voidPayment flow resolves to nothing on
// purpose and carries no function fixture.
var codeTaintEvidenceFamilyFunctions = []codeTaintEvidenceFamilyFunctionFixture{
	{
		FunctionName: "handlePayment",
		RelativePath: "src/payments/handler.go",
		StartLine:    8,
		EndLine:      24,
	},
	{
		FunctionName: "refundPayment",
		RelativePath: "src/payments/refund.go",
		StartLine:    28,
		EndLine:      46,
	},
}

// codeTaintEvidenceFamilyFunctionUID returns the live graph uid of one
// endpoint function -- the CanonicalEntityID the writer's MATCH binds. It
// fails closed on an unknown name so a finding can never reference a
// function the fixture does not materialize.
func codeTaintEvidenceFamilyFunctionUID(name string) (string, error) {
	for _, fixture := range codeTaintEvidenceFamilyFunctions {
		if fixture.FunctionName != name {
			continue
		}
		return content.CanonicalEntityID(
			codeTaintEvidenceFamilyRepoID,
			fixture.RelativePath,
			"Function",
			fixture.FunctionName,
			fixture.StartLine,
		), nil
	}
	return "", fmt.Errorf("familyodu: code_taint_evidence fixture names unknown function %q", name)
}

// codeTaintEvidenceFamilyFindingFixture describes one code_taint_evidence
// fact in the Odù: a resolved intraprocedural value-flow finding the
// collector already joined to its Function entity uid.
type codeTaintEvidenceFamilyFindingFixture struct {
	// FunctionName is the finding's function. The builder resolves it to
	// the graph uid through codeTaintEvidenceFamilyFunctionUID, so the
	// finding always names a node the fixture materializes.
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
	// TombstoneFunctionUID and TombstoneFunctionName are the schematic
	// legacy identity a deleted finding carried. A tombstone is skipped
	// before decoding, so these never reach the extractor and no canonical
	// form is observable through them.
	TombstoneFunctionUID  string
	TombstoneFunctionName string
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
		RelativePath:          "src/payments/void.go",
		Kind:                  "cross_site_scripting",
		SinkKind:              "html_sink",
		SourceKind:            "http_request",
		Binding:               "comment",
		SourceLine:            7,
		SinkLine:              9,
		Confidence:            0.71,
		TombstoneFunctionUID:  "func:payments:voidPayment",
		TombstoneFunctionName: "voidPayment",
		Tombstone:             true,
	},
}

// codeTaintEvidenceFamilyStableFactKey derives one fact's durable dedup key
// from the collector's own identity inputs (function uid plus source/sink
// lines plus kinds plus binding), in a readable fixture-local format rather
// than a hex digest, instead of hand-typing a string the expected-edge
// fixture cannot independently check. It takes the resolved uid rather than
// the fixture name so the tombstone key keeps the legacy endpoint uid --
// the only identity that finding ever carried.
func codeTaintEvidenceFamilyStableFactKey(functionUID string, sourceLine, sinkLine int, sinkKind, sourceKind, binding string) string {
	return fmt.Sprintf(
		"git:code_taint_evidence:%s:%d:%d:%s:%s:%s",
		functionUID,
		sourceLine,
		sinkLine,
		sinkKind,
		sourceKind,
		binding,
	)
}

// codeTaintEvidenceFamilyNodeSubstrateFacts builds the five
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
// source run leaves the drain waiting). The local path is load-bearing for
// the same reason derived_from's is: Repository.path carries a global
// UNIQUE constraint and the codegraph extractor falls back to local_path
// for it.
func codeTaintEvidenceFamilyNodeSubstrateFacts(scopeID, generationID string) []facts.Envelope {
	substrate := make([]facts.Envelope, 0, 5)
	substrate = append(substrate, facts.Envelope{
		ScopeID:          scopeID,
		GenerationID:     generationID,
		FactKind:         RepositoryFactKind,
		StableFactKey:    "repository:" + codeTaintEvidenceFamilyRepoID,
		SchemaVersion:    codeTaintEvidenceFamilySchemaVersion,
		CollectorKind:    codeTaintEvidenceFamilyCollectorKind,
		SourceConfidence: codeTaintEvidenceFamilySourceConfidence,
		Payload: map[string]any{
			"repo_id":       codeTaintEvidenceFamilyRepoID,
			"source_run_id": codeTaintEvidenceFamilySourceRunID,
			"local_path":    codeTaintEvidenceFamilyLocalPath,
		},
	})
	seenFiles := map[string]bool{}
	for _, fixture := range codeTaintEvidenceFamilyFunctions {
		if seenFiles[fixture.RelativePath] {
			continue
		}
		seenFiles[fixture.RelativePath] = true
		graphID := codeTaintEvidenceFamilyRepoID + ":" + fixture.RelativePath
		graphKind := "file"
		language := "go"
		isDependency := false
		payload, err := factschema.EncodeCodegraphFile(codegraphv1.File{
			RepoID:         codeTaintEvidenceFamilyRepoID,
			RelativePath:   fixture.RelativePath,
			ParsedFileData: map[string]any{},
			GraphID:        &graphID,
			GraphKind:      &graphKind,
			Language:       &language,
			IsDependency:   &isDependency,
		})
		if err != nil {
			panic(fmt.Sprintf("familyodu: encode code_taint_evidence file %q: %v", fixture.RelativePath, err))
		}
		substrate = append(substrate, facts.Envelope{
			ScopeID:          scopeID,
			GenerationID:     generationID,
			FactKind:         FileFactKind,
			StableFactKey:    "file:" + codeTaintEvidenceFamilyRepoID + ":" + fixture.RelativePath,
			SchemaVersion:    codeTaintEvidenceFamilySchemaVersion,
			CollectorKind:    codeTaintEvidenceFamilyCollectorKind,
			SourceConfidence: codeTaintEvidenceFamilySourceConfidence,
			Payload:          payload,
		})
	}
	for _, fixture := range codeTaintEvidenceFamilyFunctions {
		entityID := content.CanonicalEntityID(
			codeTaintEvidenceFamilyRepoID,
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
			SchemaVersion:    codeTaintEvidenceFamilySchemaVersion,
			CollectorKind:    codeTaintEvidenceFamilyCollectorKind,
			SourceConfidence: codeTaintEvidenceFamilySourceConfidence,
			Payload: map[string]any{
				"graph_id":      entityID,
				"graph_kind":    "content_entity",
				"entity_id":     entityID,
				"repo_id":       codeTaintEvidenceFamilyRepoID,
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

// codeTaintEvidenceFamilyFindingFact builds one code_taint_evidence fact
// envelope, resolving the fixture's function name to the graph uid the
// fixture materializes. It panics on an encode failure for the same reason
// S3LogsToFamilyOdu does: a failure means the payload contract moved under
// a committed fixture, and every coverage claim built on it is already
// void.
func codeTaintEvidenceFamilyFindingFact(scopeID, generationID string, fixture codeTaintEvidenceFamilyFindingFixture) facts.Envelope {
	functionUID := fixture.TombstoneFunctionUID
	functionName := fixture.TombstoneFunctionName
	if !fixture.Tombstone {
		var err error
		functionUID, err = codeTaintEvidenceFamilyFunctionUID(fixture.FunctionName)
		if err != nil {
			panic(err.Error())
		}
		functionName = fixture.FunctionName
	}
	relativePath := fixture.RelativePath
	language := "go"
	kind := fixture.Kind
	sinkKind := fixture.SinkKind
	sourceKind := fixture.SourceKind
	binding := fixture.Binding
	sourceLine := fixture.SourceLine
	sinkLine := fixture.SinkLine
	confidence := fixture.Confidence
	payload, err := factschema.EncodeCodeTaintEvidence(codedataflowv1.TaintEvidence{
		FunctionUID:  functionUID,
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
			CodeTaintEvidenceFamilyOduName, functionUID, err,
		))
	}
	return facts.Envelope{
		ScopeID:          scopeID,
		GenerationID:     generationID,
		FactKind:         code.TaintEvidenceFactKind,
		StableFactKey:    codeTaintEvidenceFamilyStableFactKey(functionUID, fixture.SourceLine, fixture.SinkLine, fixture.SinkKind, fixture.SourceKind, fixture.Binding),
		SchemaVersion:    codeTaintEvidenceFamilySchemaVersion,
		CollectorKind:    codeTaintEvidenceFamilyCollectorKind,
		SourceConfidence: codeTaintEvidenceFamilySourceConfidence,
		IsTombstone:      fixture.Tombstone,
		Payload:          payload,
	}
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
	factsForOdu := codeTaintEvidenceFamilyNodeSubstrateFacts(
		codeTaintEvidenceFamilyScopeID, codeTaintEvidenceFamilyGenerationID,
	)
	for _, fixture := range codeTaintEvidenceFamilyFindings {
		factsForOdu = append(factsForOdu, codeTaintEvidenceFamilyFindingFact(
			codeTaintEvidenceFamilyScopeID, codeTaintEvidenceFamilyGenerationID, fixture,
		))
	}

	return CatalogOdu{
		Odu:    Odu{Name: CodeTaintEvidenceFamilyOduName, Facts: factsForOdu},
		Detail: "eight facts for the direct-materialization code_taint_evidence family: one repository fact, two typed file facts, two content_entity facts materializing the endpoint Function nodes (the writer MATCHes Function nodes, so without them the statements complete having written nothing), two live code_taint_evidence findings on distinct functions (the two HAS_TAINT_EVIDENCE edges) and one tombstoned finding for a deleted flow (the restraint proving tombstones contribute no edge and no quarantine)",
	}
}
