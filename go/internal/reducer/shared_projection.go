package reducer

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// SharedProjectionDomain constants for the shared projection domains.
const (
	DomainPlatformInfra      = "platform_infra"
	DomainRepoDependency     = "repo_dependency"
	DomainWorkloadDependency = "workload_dependency"
	DomainCodeCalls          = "code_calls"
	DomainSQLRelationships   = "sql_relationships"
	DomainInheritanceEdges   = "inheritance_edges"
)

// SharedProjectionIntentRow is one durable shared-domain projection intent.
type SharedProjectionIntentRow struct {
	IntentID         string
	ProjectionDomain string
	PartitionKey     string
	ScopeID          string
	AcceptanceUnitID string
	RepositoryID     string
	SourceRunID      string
	GenerationID     string
	Payload          map[string]any
	CreatedAt        time.Time
	CompletedAt      *time.Time
}

// SharedProjectionIntentInput holds the parameters for building one
// deterministic shared projection intent row.
type SharedProjectionIntentInput struct {
	ProjectionDomain string
	PartitionKey     string
	ScopeID          string
	AcceptanceUnitID string
	RepositoryID     string
	SourceRunID      string
	GenerationID     string
	Payload          map[string]any
	CreatedAt        time.Time
}

// BuildSharedProjectionIntent builds one deterministic shared projection intent
// row. The intent ID is a SHA256 of the identity fields, matching the Python
// implementation exactly.
func BuildSharedProjectionIntent(input SharedProjectionIntentInput) SharedProjectionIntentRow {
	acceptanceUnitID := strings.TrimSpace(input.AcceptanceUnitID)
	if acceptanceUnitID == "" {
		acceptanceUnitID = strings.TrimSpace(input.RepositoryID)
	}

	intentID := stableIntentID(map[string]string{
		"acceptance_unit_id": acceptanceUnitID,
		"generation_id":      input.GenerationID,
		"partition_key":      input.PartitionKey,
		"projection_domain":  input.ProjectionDomain,
		"repository_id":      input.RepositoryID,
		"scope_id":           strings.TrimSpace(input.ScopeID),
		"source_run_id":      input.SourceRunID,
	})

	return SharedProjectionIntentRow{
		IntentID:         intentID,
		ProjectionDomain: input.ProjectionDomain,
		PartitionKey:     input.PartitionKey,
		ScopeID:          strings.TrimSpace(input.ScopeID),
		AcceptanceUnitID: acceptanceUnitID,
		RepositoryID:     input.RepositoryID,
		SourceRunID:      input.SourceRunID,
		GenerationID:     input.GenerationID,
		Payload:          input.Payload,
		CreatedAt:        input.CreatedAt,
		CompletedAt:      nil,
	}
}

// SharedProjectionAcceptanceKey identifies one authoritative freshness slice.
type SharedProjectionAcceptanceKey struct {
	ScopeID          string
	AcceptanceUnitID string
	SourceRunID      string
}

func sharedProjectionReadinessPhase(domain string) (GraphProjectionPhase, bool) {
	switch domain {
	case DomainCodeCalls:
		return GraphProjectionPhaseCanonicalNodesCommitted, true
	case DomainSQLRelationships, DomainInheritanceEdges:
		return GraphProjectionPhaseSemanticNodesCommitted, true
	default:
		return "", false
	}
}

func graphProjectionPhaseKeyForAcceptance(
	key SharedProjectionAcceptanceKey,
	generationID string,
	keyspace GraphProjectionKeyspace,
) (GraphProjectionPhaseKey, bool) {
	phaseKey := GraphProjectionPhaseKey{
		ScopeID:          strings.TrimSpace(key.ScopeID),
		AcceptanceUnitID: strings.TrimSpace(key.AcceptanceUnitID),
		SourceRunID:      strings.TrimSpace(key.SourceRunID),
		GenerationID:     strings.TrimSpace(generationID),
		Keyspace:         keyspace,
	}
	if err := phaseKey.Validate(); err != nil {
		return GraphProjectionPhaseKey{}, false
	}
	return phaseKey, true
}

func graphProjectionPhaseKeyForIntent(
	row SharedProjectionIntentRow,
	generationID string,
	keyspace GraphProjectionKeyspace,
) (GraphProjectionPhaseKey, bool) {
	acceptanceKey, ok := row.AcceptanceKey()
	if !ok {
		return GraphProjectionPhaseKey{}, false
	}
	return graphProjectionPhaseKeyForAcceptance(acceptanceKey, generationID, keyspace)
}

// AcceptanceKey returns the bounded-unit freshness key for the row.
func (row SharedProjectionIntentRow) AcceptanceKey() (SharedProjectionAcceptanceKey, bool) {
	scopeID := strings.TrimSpace(row.ScopeID)
	if scopeID == "" && row.Payload != nil {
		scopeID = strings.TrimSpace(anyToString(row.Payload["scope_id"]))
	}

	acceptanceUnitID := strings.TrimSpace(row.AcceptanceUnitID)
	if acceptanceUnitID == "" && row.Payload != nil {
		acceptanceUnitID = strings.TrimSpace(anyToString(row.Payload["acceptance_unit_id"]))
	}
	if acceptanceUnitID == "" {
		acceptanceUnitID = strings.TrimSpace(row.RepositoryID)
	}

	sourceRunID := strings.TrimSpace(row.SourceRunID)
	if scopeID == "" || acceptanceUnitID == "" || sourceRunID == "" {
		return SharedProjectionAcceptanceKey{}, false
	}

	return SharedProjectionAcceptanceKey{
		ScopeID:          scopeID,
		AcceptanceUnitID: acceptanceUnitID,
		SourceRunID:      sourceRunID,
	}, true
}

// RowsForPartition returns intent rows whose partition key belongs to one
// worker partition.
func RowsForPartition(rows []SharedProjectionIntentRow, partitionID, partitionCount int) []SharedProjectionIntentRow {
	var result []SharedProjectionIntentRow
	for _, row := range rows {
		p, err := PartitionForKey(row.PartitionKey, partitionCount)
		if err != nil {
			continue
		}
		if p == partitionID {
			result = append(result, row)
		}
	}
	return result
}

// stableIntentID computes a deterministic intent identifier matching the Python
// _stable_intent_id function. It serializes the identity dict as compact
// JSON with sorted keys: {"identity":{...sorted fields...}}
func stableIntentID(identity map[string]string) string {
	// Build the identity object with sorted keys. Since json.Marshal sorts
	// map keys by default in Go, this produces the same output as Python's
	// json.dumps(sort_keys=True, separators=(",", ":")).
	wrapper := map[string]any{
		"identity": identity,
	}

	payload, err := json.Marshal(wrapper)
	if err != nil {
		// Identity fields are plain strings; marshal cannot fail.
		panic(fmt.Sprintf("marshal identity: %v", err))
	}

	digest := sha256.Sum256(payload)
	return fmt.Sprintf("%x", digest)
}
