package scope

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// NewTerraformStateSnapshotScope returns a durable state_snapshot scope for a
// Terraform state locator.
func NewTerraformStateSnapshotScope(
	parentScopeID string,
	backendKind string,
	locator string,
	metadata map[string]string,
) (IngestionScope, error) {
	backendKind = strings.ToLower(strings.TrimSpace(backendKind))
	locator = strings.TrimSpace(locator)
	if backendKind == "" {
		return IngestionScope{}, fmt.Errorf("backend_kind must not be blank")
	}
	if locator == "" {
		return IngestionScope{}, fmt.Errorf("locator must not be blank")
	}

	locatorHash := hashStateLocator(backendKind, locator)
	scopeMetadata := cloneStringMap(metadata)
	scopeMetadata["backend_kind"] = backendKind
	scopeMetadata["locator_hash"] = locatorHash

	return IngestionScope{
		ScopeID:       fmt.Sprintf("state_snapshot:%s:%s", backendKind, locatorHash),
		SourceSystem:  string(CollectorTerraformState),
		ScopeKind:     KindStateSnapshot,
		ParentScopeID: parentScopeID,
		CollectorKind: CollectorTerraformState,
		PartitionKey:  fmt.Sprintf("terraform_state:%s:%s", backendKind, locatorHash),
		Metadata:      scopeMetadata,
	}, nil
}

// NewTerraformStateSnapshotGeneration returns the pending generation identity
// for one observed Terraform state serial and lineage.
func NewTerraformStateSnapshotGeneration(
	scopeID string,
	serial int64,
	lineageUUID string,
	observedAt time.Time,
) (ScopeGeneration, error) {
	scopeID = strings.TrimSpace(scopeID)
	lineageUUID = strings.TrimSpace(lineageUUID)
	if scopeID == "" {
		return ScopeGeneration{}, fmt.Errorf("scope_id must not be blank")
	}
	if serial < 0 {
		return ScopeGeneration{}, fmt.Errorf("serial must not be negative")
	}
	if lineageUUID == "" {
		return ScopeGeneration{}, fmt.Errorf("lineage_uuid must not be blank")
	}

	observedAt = observedAt.UTC()
	return ScopeGeneration{
		GenerationID:  fmt.Sprintf("terraform_state:%s:%s:serial:%d", scopeID, lineageUUID, serial),
		ScopeID:       scopeID,
		ObservedAt:    observedAt,
		IngestedAt:    observedAt,
		Status:        GenerationStatusPending,
		TriggerKind:   TriggerKindSnapshot,
		FreshnessHint: fmt.Sprintf("lineage=%s serial=%d", lineageUUID, serial),
	}, nil
}

func hashStateLocator(backendKind, locator string) string {
	sum := sha256.Sum256([]byte(backendKind + "\x00" + locator))
	return hex.EncodeToString(sum[:])
}

func cloneStringMap(input map[string]string) map[string]string {
	cloned := make(map[string]string, len(input)+2)
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}
