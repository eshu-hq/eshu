package reducer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const containerImageIdentityFactKind = "reducer_container_image_identity"

// PostgresContainerImageIdentityWriter persists digest-keyed image identity
// decisions into the shared fact store.
type PostgresContainerImageIdentityWriter struct {
	DB  workloadIdentityExecer
	Now func() time.Time
}

// WriteContainerImageIdentityDecisions stores only canonical image identity
// decisions. Weak, missing, ambiguous, or stale tag outcomes stay diagnostic
// reducer output until a stronger source can prove digest identity.
func (w PostgresContainerImageIdentityWriter) WriteContainerImageIdentityDecisions(
	ctx context.Context,
	write ContainerImageIdentityWrite,
) (ContainerImageIdentityWriteResult, error) {
	if w.DB == nil {
		return ContainerImageIdentityWriteResult{}, fmt.Errorf("container image identity database is required")
	}

	now := reducerWriterNow(w.Now)
	decisions := containerImageIdentityCanonicalDecisions(write.Decisions)
	for _, decision := range decisions {
		canonicalID := canonicalContainerImageIdentityID(write, decision)
		payloadJSON, err := json.Marshal(containerImageIdentityPayload(write, decision, canonicalID))
		if err != nil {
			return ContainerImageIdentityWriteResult{}, fmt.Errorf("marshal container image identity payload: %w", err)
		}

		if _, err := w.DB.ExecContext(
			ctx,
			canonicalReducerFactInsertQuery,
			containerImageIdentityFactID(write, decision),
			write.ScopeID,
			write.GenerationID,
			containerImageIdentityFactKind,
			containerImageIdentityStableFactKey(write, decision),
			reducerFactCollectorKind(write.SourceSystem),
			facts.SourceConfidenceInferred,
			write.SourceSystem,
			write.IntentID,
			nil,
			nil,
			now,
			now,
			false,
			payloadJSON,
		); err != nil {
			return ContainerImageIdentityWriteResult{}, fmt.Errorf("write container image identity fact: %w", err)
		}
	}

	return ContainerImageIdentityWriteResult{
		CanonicalWrites: len(decisions),
		EvidenceSummary: fmt.Sprintf("wrote container image identity decisions %d", len(decisions)),
	}, nil
}

func containerImageIdentityFactID(
	write ContainerImageIdentityWrite,
	decision ContainerImageIdentityDecision,
) string {
	return containerImageIdentityFactKind + ":" + facts.StableID(
		containerImageIdentityFactKind,
		containerImageIdentityIdentity(write, decision),
	)
}

func containerImageIdentityStableFactKey(
	write ContainerImageIdentityWrite,
	decision ContainerImageIdentityDecision,
) string {
	identity := containerImageIdentityIdentity(write, decision)
	return strings.Join([]string{
		"container_image_identity",
		strings.TrimSpace(fmt.Sprint(identity["scope_id"])),
		strings.TrimSpace(fmt.Sprint(identity["generation_id"])),
		strings.TrimSpace(fmt.Sprint(identity["digest"])),
		strings.TrimSpace(fmt.Sprint(identity["image_ref"])),
	}, ":")
}

func canonicalContainerImageIdentityID(
	write ContainerImageIdentityWrite,
	decision ContainerImageIdentityDecision,
) string {
	return "canonical:" + containerImageIdentityStableFactKey(write, decision)
}

func containerImageIdentityIdentity(
	write ContainerImageIdentityWrite,
	decision ContainerImageIdentityDecision,
) map[string]any {
	return map[string]any{
		"scope_id":      strings.TrimSpace(write.ScopeID),
		"generation_id": strings.TrimSpace(write.GenerationID),
		"image_ref":     strings.TrimSpace(decision.ImageRef),
		"digest":        strings.TrimSpace(decision.Digest),
		"outcome":       string(decision.Outcome),
	}
}

func containerImageIdentityPayload(
	write ContainerImageIdentityWrite,
	decision ContainerImageIdentityDecision,
	canonicalID string,
) map[string]any {
	return map[string]any{
		"reducer_domain":    string(DomainContainerImageIdentity),
		"intent_id":         write.IntentID,
		"scope_id":          write.ScopeID,
		"generation_id":     write.GenerationID,
		"source_system":     write.SourceSystem,
		"cause":             write.Cause,
		"image_ref":         decision.ImageRef,
		"digest":            decision.Digest,
		"repository_id":     decision.RepositoryID,
		"outcome":           string(decision.Outcome),
		"reason":            decision.Reason,
		"canonical_id":      canonicalID,
		"canonical_writes":  decision.CanonicalWrites,
		"evidence_fact_ids": uniqueSortedStrings(decision.EvidenceFactIDs),
		"identity_strength": decision.IdentityStrength,
		"publication_kind":  containerImageIdentityFactKind,
		"source_layers":     []string{"source_declaration", "registry_observation", "observed_resource"},
	}
}
