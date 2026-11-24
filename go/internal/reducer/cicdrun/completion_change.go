// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cicdrun

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
)

// The ci_cd completion change gate (#10972 PR-4). A ci_cd_run_correlation
// success emits a cross-scope completion event, and the completion fan-out
// re-pends every settled supply_chain_impact item, the only consumer of this
// domain's completion (reducer/crossscope/dependencies.go). A success that
// wrote nothing a consumer reads gives those passes no new input, so the
// writer computes a changed verdict inside its own transaction and the ack
// emits only for a changed pass, modelled on the container_image_identity gate
// (#8284).

// CompletionChangedSignal is the result sub-signal that carries a ci_cd pass's
// changed verdict to the queue acknowledgement: 1 when the pass inserted or
// changed a row supply_chain_impact reads, 0 when it wrote nothing a consumer
// could see. A result without it emits the completion event, so an unwired
// verdict never starves the consumer.
const CompletionChangedSignal = "cicd_completion_changed"

// PayloadKeyCompletionOwed is the work-item payload key of the durable
// completion-owed marker. The writer sets it inside the transaction that
// commits changed rows, fenced on the pass's lease; the acknowledgement that
// emits clears it. A pass that commits changes and dies before its ack leaves
// the marker, so the retry emits although its own rewrite shows no change.
const PayloadKeyCompletionOwed = "cicd_completion_owed"

// CompletionOwedRecorder marks the calling pass's own work item as owing a
// completion event, inside the writer's transaction. It must fail when the
// pass no longer holds its lease, so a zombie pass cannot commit.
type CompletionOwedRecorder interface {
	RecordCompletionOwed(ctx context.Context, exec factwrite.Execer) error
}

// ShouldEmitCompletion reports whether an acknowledged ci_cd result earns a
// completion event. A result without the verdict emits (fail open: a bounded
// spurious refresh beats a silent missed one).
func ShouldEmitCompletion(result reducercontract.Result) bool {
	changed, ok := result.SubSignals[CompletionChangedSignal]
	if !ok {
		return true
	}
	return changed > 0
}

// completionDigestFields are the payload fields of a reducer_ci_cd_run_correlation
// row that supply_chain_impact reads: the deployment context
// (core/deployment_context.go supplyChainDeploymentContextFromEnvelope:
// artifact_digest, image_ref, repository_id, environment, environment_evidence,
// outcome, provenance_only) and the evidence-filter expansion
// (core/active_filter.go and core/criteria_bound.go: artifact_digest,
// repository_id, image_ref). The fact id itself is the row identity. Per-pass
// fields (intent_id, cause, generation_id, evidence_fact_ids, source layers)
// change on every pass and are left out, so a rewrite of the same truth is
// unchanged.
var completionDigestFields = []string{
	"artifact_digest", "environment", "environment_evidence", "image_ref",
	"outcome", "provenance_only", "repository_id",
}

// completionDigest hashes the consumer-read fields of one row payload. Values
// are compared through their JSON encoding, so a value read back from the
// store and the value the writer is about to write hash alike.
func completionDigest(payload map[string]any) string {
	view := make(map[string]any, len(completionDigestFields))
	for _, key := range completionDigestFields {
		view[key] = payload[key]
	}
	encoded, err := json.Marshal(view)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

// storedCompletionRow is one existing row the writer is about to overwrite.
type storedCompletionRow struct {
	tombstone bool
	digest    string
}

// completionChanged is the changed verdict of one write. A pass with no rows
// has no baseline and counts as changed (the first success of an item emits).
// A row whose fact id has no live stored row is new (a new generation has new
// fact ids), and a row whose consumer digest differs is changed. The writer
// never tombstones rows, but a tombstoned stored row revived by this write is
// a change too.
func completionChanged(newDigests map[string]string, stored map[string]storedCompletionRow) bool {
	if len(newDigests) == 0 {
		return true
	}
	for factID, digest := range newDigests {
		previous, ok := stored[factID]
		if !ok || previous.tombstone || previous.digest == "" || previous.digest != digest {
			return true
		}
	}
	return false
}
