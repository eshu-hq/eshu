// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

// This file is the reducer root's compatibility surface for the decode and fact-load/write families (schemadecode, factdecode, factwrite, factload, payloadcore, code/function/summary)
// (issue #6061). It merges the per-family *_compat.go files listed below
// with no behavior change: every alias and forwarder is preserved
// byte-identical under its stanza marker. A family move adds a stanza
// to the matching bucket file and NEVER creates a new *_compat.go
// (docs/internal/design/reducer-target-tree.md). Each entry is deleted
// once its last caller has moved; see the importer-migration child issue.
//
// Stanzas merged here:
//   - decode_seam_compat.go
//   - decode_seam_compat2.go
//   - decode_seam_compat3.go
//   - quarantine_compat.go
//   - reducer_fact_write_compat.go
//   - scoped_fact_loader_compat.go
//   - shared_payload_delta_compat.go

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/function/summary"
	"github.com/eshu-hq/eshu/go/internal/reducer/factdecode"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/schemadecode"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Stanza: decode_seam_compat.go (merged; do not recreate this file).
// This file is the transitional compatibility surface for the per-fact-kind
// decoders that moved to [schemadecode] (issue #6061). Every entry binds the
// reducer root's original lowercase spelling to the exported name in that
// package, so the remaining root call sites keep their current spelling; each
// entry is deleted once its last caller has moved into a family subpackage.
// The four incident-routing entries were removed when their only callers
// moved into internal/reducer/incident, which imports schemadecode directly.

var (
	decodeAWSRelationship            = schemadecode.DecodeAWSRelationship
	decodeAWSResource                = schemadecode.DecodeAWSResource
	decodeAzureCloudRelationship     = schemadecode.DecodeAzureCloudRelationship
	decodeAzureCloudResource         = schemadecode.DecodeAzureCloudResource
	decodeCodeFunctionSource         = schemadecode.DecodeCodeFunctionSource
	decodeCodeFunctionSummary        = schemadecode.DecodeCodeFunctionSummary
	decodeCodeInterprocEvidence      = schemadecode.DecodeCodeInterprocEvidence
	decodeCodeTaintEvidence          = schemadecode.DecodeCodeTaintEvidence
	decodeCodegraphFile              = schemadecode.DecodeCodegraphFile
	decodeCodeownersOwnership        = schemadecode.DecodeCodeownersOwnership
	decodeDocumentationDocument      = schemadecode.DecodeDocumentationDocument
	decodeDocumentationEntityMention = schemadecode.DecodeDocumentationEntityMention
	decodeGCPCloudRelationship       = schemadecode.DecodeGCPCloudRelationship
	decodeGCPCloudResource           = schemadecode.DecodeGCPCloudResource
	decodeKubernetesLiveNamespace    = schemadecode.DecodeKubernetesLiveNamespace
	decodeKubernetesLivePodTemplate  = schemadecode.DecodeKubernetesLivePodTemplate
)

// Stanza: quarantine_compat.go (merged; do not recreate this file).
// This file is the transitional compatibility surface for the fact-decode and
// quarantine mechanism that moved to [factdecode] (issue #6061). Reducer-root
// call sites and the external packages that name the exported quarantine types
// keep their current spelling; each entry is deleted once its last caller has
// moved into a family subpackage.

// QuarantinedFactRecord is the durable dead-letter row for one malformed fact.
type QuarantinedFactRecord = factdecode.QuarantinedFactRecord

// QuarantinedFactWriter persists quarantined facts, implemented by the Postgres
// input-invalid fact store.
type QuarantinedFactWriter = factdecode.QuarantinedFactWriter

// quarantinedFact is the in-flight quarantine value produced by a decode
// failure, before it is persisted as a QuarantinedFactRecord.
type quarantinedFact = factdecode.QuarantinedFact

// factDecodeError classifies a malformed payload as a terminal dead letter.
type factDecodeError = factdecode.FactDecodeError

// WithQuarantineWriter forwards to [factdecode.WithQuarantineWriter].
func WithQuarantineWriter(ctx context.Context, writer QuarantinedFactWriter) context.Context {
	return factdecode.WithQuarantineWriter(ctx, writer)
}

// newFactDecodeError forwards to [factdecode.NewFactDecodeError].
func newFactDecodeError(factKind string, err error) *factDecodeError {
	return factdecode.NewFactDecodeError(factKind, err)
}

// partitionDecodeFailures forwards to [factdecode.PartitionDecodeFailures].
func partitionDecodeFailures(env facts.Envelope, err error) (quarantinedFact, bool, error) {
	return factdecode.PartitionDecodeFailures(env, err)
}

// quarantinedAttributeShapeFact forwards to
// [factdecode.QuarantinedAttributeShapeFact].
func quarantinedAttributeShapeFact(env facts.Envelope, err error) quarantinedFact {
	return factdecode.QuarantinedAttributeShapeFact(env, err)
}

// attributeShapeAsFactDecodeError forwards to
// [factdecode.AttributeShapeAsFactDecodeError].
func attributeShapeAsFactDecodeError(factKind string, err error) error {
	return factdecode.AttributeShapeAsFactDecodeError(factKind, err)
}

// inputInvalidSubSignals forwards to [factdecode.InputInvalidSubSignals].
func inputInvalidSubSignals(count int) map[string]float64 {
	return factdecode.InputInvalidSubSignals(count)
}

// recordQuarantinedFacts forwards to [factdecode.RecordQuarantinedFacts].
func recordQuarantinedFacts(
	ctx context.Context,
	instruments *telemetry.Instruments,
	domain Domain,
	scopeID, generationID string,
	quarantined []quarantinedFact,
) int {
	return factdecode.RecordQuarantinedFacts(ctx, instruments, domain, scopeID, generationID, quarantined)
}

// Stanza: reducer_fact_write_compat.go (merged; do not recreate this file).
// This file is the transitional compatibility surface for the reducer fact
// batch writers that moved to [factwrite] (issue #6061). Each entry is deleted
// once its last reducer-root caller has moved into a family subpackage.

// workloadIdentityExecer is the minimal ExecContext surface the batch writers
// need.
type workloadIdentityExecer = factwrite.Execer

// reducerFactRow is one row of a reducer-owned fact batch insert.
type reducerFactRow = factwrite.Row

// reducerFactVersionedRow is one row of a versioned reducer fact batch insert.
type reducerFactVersionedRow = factwrite.VersionedRow

// Batch-insert statement fragments and the chunk size the writers use.
const (
	reducerFactBatchInsertPrefix   = factwrite.BatchInsertPrefix
	reducerFactBatchInsertSource   = factwrite.BatchInsertSource
	reducerFactBatchInsertConflict = factwrite.BatchInsertConflict
	reducerFactBatchInsertQuery    = factwrite.BatchInsertQuery
	reducerFactBatchSize           = factwrite.BatchSize

	reducerFactBatchInsertVersionedQuery = factwrite.BatchInsertVersionedQuery

	// canonicalReducerFactInsertQuery is the canonical single-row upsert every
	// reducer-owned fact writer uses. See [factwrite.SingleInsertQuery].
	canonicalReducerFactInsertQuery = factwrite.SingleInsertQuery
)

// reducerBatchInsertFacts forwards to [factwrite.BatchInsertFacts].
func reducerBatchInsertFacts(ctx context.Context, db workloadIdentityExecer, rows []reducerFactRow) error {
	return factwrite.BatchInsertFacts(ctx, db, rows)
}

// reducerBatchInsertVersionedFacts forwards to
// [factwrite.BatchInsertVersionedFacts].
func reducerBatchInsertVersionedFacts(
	ctx context.Context,
	db workloadIdentityExecer,
	rows []reducerFactVersionedRow,
) error {
	return factwrite.BatchInsertVersionedFacts(ctx, db, rows)
}

// dedupeReducerFactRowsByFactID forwards to [factwrite.DedupeRowsByFactID]. It
// stays generic: a function-valued variable cannot carry a type parameter, and
// a func statement keeps the call inlinable.
func dedupeReducerFactRowsByFactID[T any](rows []T, factID func(T) string) []T {
	return factwrite.DedupeRowsByFactID(rows, factID)
}

// reducerWriterNow forwards to [factwrite.Now].
func reducerWriterNow(now func() time.Time) time.Time {
	return factwrite.Now(now)
}

// reducerFactCollectorKind forwards to [factwrite.CollectorKind].
func reducerFactCollectorKind(sourceSystem string) string {
	return factwrite.CollectorKind(sourceSystem)
}

// Stanza: scoped_fact_loader_compat.go (merged; do not recreate this file).
// This file is the transitional compatibility surface for the scoped fact
// loader that moved to [factload] (issue #6061). Reducer-root call sites and the
// external packages naming FactLoader keep their current spelling; each entry is
// deleted once its last caller has moved into a family subpackage.

// FactLoader loads fact envelopes for one scope generation.
type FactLoader = factload.FactLoader

// Fact-kind names the scoped loader filters on.
const (
	factKindContentEntity       = factload.FactKindContentEntity
	factKindFile                = factload.FactKindFile
	factKindParsedFile          = factload.FactKindParsedFile
	factKindRepository          = factload.FactKindRepository
	factKindCodeownersOwnership = factload.FactKindCodeownersOwnership
	factKindSubmodulePin        = factload.FactKindSubmodulePin
)

// loadFactsForKinds forwards to [factload.LoadFactsForKinds].
func loadFactsForKinds(
	ctx context.Context,
	loader FactLoader,
	scopeID string,
	generationID string,
	factKinds []string,
) ([]facts.Envelope, error) {
	return factload.LoadFactsForKinds(ctx, loader, scopeID, generationID, factKinds)
}

// classifyFactLoadError forwards to [factload.ClassifyFactLoadError].
func classifyFactLoadError(err error) error {
	return factload.ClassifyFactLoadError(err)
}

// cleanFactFilterValues forwards to [payloadcore.CleanFactFilterValues].
func cleanFactFilterValues(values []string) []string {
	return payloadcore.CleanFactFilterValues(values)
}

// Stanza: shared_payload_delta_compat.go (merged; do not recreate this file).
// This file holds the payload/delta forwarders that used to live in the
// semantic_entity_*.go files before the semantic_entity family moved to
// [code/semantic] (issue #6061). Each one already forwarded to a
// shared-tier package; they stay in root because other root families that
// have not moved out yet still call them by their unqualified root spelling.
// code/semantic calls the shared-tier functions directly instead of
// reaching back into root for these.

// payloadMap forwards to [payloadcore.PayloadMap].
func payloadMap(payload map[string]any, key string) map[string]any {
	return payloadcore.PayloadMap(payload, key)
}

// semanticPayloadString forwards to [payloadcore.SemanticPayloadString].
func semanticPayloadString(payload map[string]any, key string) string {
	return payloadcore.SemanticPayloadString(payload, key)
}

// semanticPayloadStringSlice forwards to [payloadcore.SemanticPayloadStringSlice].
func semanticPayloadStringSlice(payload map[string]any, key string) []string {
	return payloadcore.SemanticPayloadStringSlice(payload, key)
}

// semanticQualifyDeltaPath forwards to [payloadcore.QualifyDeltaPath].
func semanticQualifyDeltaPath(repoPath string, relativePath string) string {
	return payloadcore.QualifyDeltaPath(repoPath, relativePath)
}

// semanticDeltaPayloadBool forwards to [payloadcore.DeltaPayloadBool].
func semanticDeltaPayloadBool(payload map[string]any, key string) bool {
	return payloadcore.DeltaPayloadBool(payload, key)
}

// deltaScopeRepositorySet forwards to [sharedintent.DeltaScopeRepositorySet].
func deltaScopeRepositorySet(repositoryIDs []string) map[string]struct{} {
	return sharedintent.DeltaScopeRepositorySet(repositoryIDs)
}

// applyRepoRefreshDeltaScope forwards to
// [sharedintent.ApplyRepoRefreshDeltaScope], which carries the full rule and
// why the two obvious alternatives lose edges (#6216).
func applyRepoRefreshDeltaScope(
	payload map[string]any,
	repoID string,
	deltaRepositoryIDs map[string]struct{},
	filePathsByRepoID map[string][]string,
) {
	sharedintent.ApplyRepoRefreshDeltaScope(payload, repoID, deltaRepositoryIDs, filePathsByRepoID)
}

// Stanza: decode_seam_compat2.go (merged; do not recreate this file).

// This file is the transitional compatibility surface for the per-fact-kind
// decoders that moved to [schemadecode] (issue #6061). Every entry binds the
// reducer root's original lowercase spelling to the exported name in that
// package, so the 3 remaining root call sites keep their current spelling;
// each entry is deleted once its last caller has moved into a family
// subpackage. The 17 decodeObservability* entries were removed when their only
// callers moved into internal/reducer/obscoverage, and the three Vault entries
// when theirs moved into internal/reducer/secretsiam; both subpackages import
// schemadecode directly. The 9 vulnerability/scanner/package-consumption
// entries were removed with the supplychain/core move: their only callers
// were supply-chain files that now import schemadecode directly (#6061).

var (
	decodeReducerPackageOwnershipCorrelation   = schemadecode.DecodeReducerPackageOwnershipCorrelation
	decodeReducerPackagePublicationCorrelation = schemadecode.DecodeReducerPackagePublicationCorrelation
	decodeSubmodulePin                         = schemadecode.DecodeSubmodulePin
)

// Stanza: decode_seam_compat3.go (merged; do not recreate this file).

// This file is the transitional compatibility surface for the per-fact-kind
// decoders that moved to [schemadecode] (issue #6061). Every entry binds the
// reducer root's original lowercase spelling to the exported name in that
// package, so the 6 remaining root call sites (in codedataflow_input_invalid_test.go)
// keep their current spelling; each entry is deleted once its last caller has
// moved into a family subpackage. The factschemaEnvelope forwarder was removed
// with the supplychain/core move: its only callers were supply-chain files
// that now import schemadecode directly (#6061).

var (
	decodeCodeDataflowFunction = schemadecode.DecodeCodeDataflowFunction
	decodeCodeDataflowScanned  = schemadecode.DecodeCodeDataflowScanned
)

// Stanza: code-function-summary family move (#6061; no prior compat file).
// The durable value-flow function-summary persistence family moved to
// [summary] (go/internal/reducer/code/function/summary). Every entry keeps
// the reducer.X spelling for cmd/reducer's wiring, defaults_handlers.go's
// DefaultHandlers/CodeEvidenceHandlers field types, and the postgres store
// implementers named only in comments (structural typing needs no source
// change there). Each entry is deleted once its last caller names [summary]
// directly.

// CodeFunctionSummaryLoader is the root spelling of [summary.Loader].
type CodeFunctionSummaryLoader = summary.Loader

// CodeFunctionSummaryWriter is the root spelling of [summary.Writer]. It is
// satisfied by postgres.FunctionSummaryStore.
type CodeFunctionSummaryWriter = summary.Writer

// CodeFunctionSourceLoader is the root spelling of [summary.SourceLoader].
type CodeFunctionSourceLoader = summary.SourceLoader

// CodeFunctionSourceWriter is the root spelling of [summary.SourceWriter].
// It is satisfied by postgres.FunctionSourceStore.
type CodeFunctionSourceWriter = summary.SourceWriter

// CodeFunctionGraphIDLoader is the root spelling of [summary.GraphIDLoader].
type CodeFunctionGraphIDLoader = summary.GraphIDLoader

// CodeFunctionGraphIDWriter is the root spelling of [summary.GraphIDWriter].
// It is satisfied by postgres.FunctionGraphIDStore.
type CodeFunctionGraphIDWriter = summary.GraphIDWriter

// ValueFlowFixpointProjector is the root spelling of
// [summary.ValueFlowFixpointProjector]. cmd/reducer's value_flow_wiring.go
// constructs the concrete projector this interface is satisfied by.
type ValueFlowFixpointProjector = summary.ValueFlowFixpointProjector

// CodeFunctionSummaryMaterializationHandler is the root spelling of
// [summary.Handler].
type CodeFunctionSummaryMaterializationHandler = summary.Handler

// codeFunctionSummaryDomainDefinition forwards to [summary.Definition].
func codeFunctionSummaryDomainDefinition() DomainDefinition {
	return summary.Definition()
}
