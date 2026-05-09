package scope

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
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
