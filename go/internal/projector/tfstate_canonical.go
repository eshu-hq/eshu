package projector

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TerraformStateResourceRow carries one Terraform state resource instance for
// canonical graph projection.
type TerraformStateResourceRow struct {
	UID                string
	Address            string
	Mode               string
	ResourceType       string
	Name               string
	ModuleAddress      string
	ProviderAddress    string
	Lineage            string
	Serial             int64
	BackendKind        string
	LocatorHash        string
	StatePath          string
	SourceFactID       string
	StableFactKey      string
	SourceSystem       string
	SourceRecordID     string
	SourceConfidence   string
	CollectorKind      string
	CorrelationAnchors []string
	TagKeyHashes       []string
	ObservedAt         time.Time
}

// TerraformStateModuleRow carries one Terraform module observed in state.
type TerraformStateModuleRow struct {
	UID              string
	ModuleAddress    string
	ResourceCount    int64
	Lineage          string
	Serial           int64
	BackendKind      string
	LocatorHash      string
	StatePath        string
	SourceFactID     string
	StableFactKey    string
	SourceSystem     string
	SourceRecordID   string
	SourceConfidence string
	CollectorKind    string
	ObservedAt       time.Time
}

// TerraformStateOutputRow carries one Terraform output observed in state.
type TerraformStateOutputRow struct {
	UID              string
	Name             string
	Sensitive        bool
	ValueShape       string
	Lineage          string
	Serial           int64
	BackendKind      string
	LocatorHash      string
	StatePath        string
	SourceFactID     string
	StableFactKey    string
	SourceSystem     string
	SourceRecordID   string
	SourceConfidence string
	CollectorKind    string
	ObservedAt       time.Time
}

type terraformStateSnapshotContext struct {
	Lineage     string
	Serial      int64
	BackendKind string
	LocatorHash string
	StatePath   string
}

func extractTerraformStateRows(mat *CanonicalMaterialization, envelopes []facts.Envelope) {
	if mat == nil || len(envelopes) == 0 {
		return
	}

	snapshot := terraformStateSnapshot(envelopes)
	tagHashesByResource := terraformStateTagHashesByResource(envelopes)
	moduleRows := []TerraformStateModuleRow{}
	for _, envelope := range envelopes {
		switch envelope.FactKind {
		case facts.TerraformStateResourceFactKind:
			if row, ok := terraformStateResourceRow(
				mat.ScopeID,
				snapshot,
				tagHashesByResource,
				envelope,
			); ok {
				mat.TerraformStateResources = append(mat.TerraformStateResources, row)
			}
		case facts.TerraformStateModuleFactKind:
			if row, ok := terraformStateModuleRow(mat.ScopeID, snapshot, envelope); ok {
				moduleRows = append(moduleRows, row)
			}
		case facts.TerraformStateOutputFactKind:
			if row, ok := terraformStateOutputRow(mat.ScopeID, snapshot, envelope); ok {
				mat.TerraformStateOutputs = append(mat.TerraformStateOutputs, row)
			}
		}
	}
	mat.TerraformStateModules = append(mat.TerraformStateModules, aggregateTerraformStateModuleRows(moduleRows)...)
}

func validateTerraformStateSchemaVersion(envelope facts.Envelope) error {
	want, ok := facts.TerraformStateSchemaVersion(envelope.FactKind)
	if !ok {
		return nil
	}
	got := strings.TrimSpace(envelope.SchemaVersion)
	if got == "" {
		return fmt.Errorf("terraform state fact %q schema_version must not be blank", envelope.FactID)
	}
	if got != want {
		return fmt.Errorf(
			"terraform state fact %q schema_version %q is unsupported for %s; want %q",
			envelope.FactID,
			got,
			envelope.FactKind,
			want,
		)
	}
	return nil
}

func terraformStateSnapshot(envelopes []facts.Envelope) terraformStateSnapshotContext {
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.TerraformStateSnapshotFactKind {
			continue
		}
		lineage, _ := payloadString(envelope.Payload, "lineage")
		backendKind, _ := payloadString(envelope.Payload, "backend_kind")
		locatorHash, _ := payloadString(envelope.Payload, "locator_hash")
		serial, _ := payloadInt(envelope.Payload, "serial")
		return terraformStateSnapshotContext{
			Lineage:     lineage,
			Serial:      int64(serial),
			BackendKind: backendKind,
			LocatorHash: locatorHash,
			StatePath:   terraformStatePath(backendKind, locatorHash, envelope.ScopeID),
		}
	}
	return terraformStateSnapshotContext{}
}

func terraformStateResourceRow(
	scopeID string,
	snapshot terraformStateSnapshotContext,
	tagHashesByResource map[string][]string,
	envelope facts.Envelope,
) (TerraformStateResourceRow, bool) {
	address, _ := payloadString(envelope.Payload, "address")
	if address == "" {
		return TerraformStateResourceRow{}, false
	}
	mode, _ := payloadString(envelope.Payload, "mode")
	resourceType, _ := payloadString(envelope.Payload, "type")
	name, _ := payloadString(envelope.Payload, "name")
	moduleAddress, _ := payloadString(envelope.Payload, "module")
	providerAddress, _ := payloadString(envelope.Payload, "provider")
	sourceSystem := terraformStateSourceSystem(envelope)
	return TerraformStateResourceRow{
		UID:                terraformStateUID("resource", scopeID, snapshot.Lineage, address),
		Address:            address,
		Mode:               mode,
		ResourceType:       resourceType,
		Name:               name,
		ModuleAddress:      moduleAddress,
		ProviderAddress:    providerAddress,
		Lineage:            snapshot.Lineage,
		Serial:             snapshot.Serial,
		BackendKind:        snapshot.BackendKind,
		LocatorHash:        snapshot.LocatorHash,
		StatePath:          snapshot.StatePath,
		SourceFactID:       envelope.FactID,
		StableFactKey:      envelope.StableFactKey,
		SourceSystem:       sourceSystem,
		SourceRecordID:     envelope.SourceRef.SourceRecordID,
		SourceConfidence:   envelope.SourceConfidence,
		CollectorKind:      envelope.CollectorKind,
		CorrelationAnchors: terraformStateCorrelationAnchors(envelope.Payload),
		TagKeyHashes:       tagHashesByResource[address],
		ObservedAt:         envelope.ObservedAt,
	}, true
}

func terraformStateModuleRow(
	scopeID string,
	snapshot terraformStateSnapshotContext,
	envelope facts.Envelope,
) (TerraformStateModuleRow, bool) {
	moduleAddress, _ := payloadString(envelope.Payload, "module_address")
	if moduleAddress == "" {
		return TerraformStateModuleRow{}, false
	}
	resourceCount, _ := payloadInt(envelope.Payload, "resource_count")
	sourceSystem := terraformStateSourceSystem(envelope)
	return TerraformStateModuleRow{
		UID:              terraformStateUID("module", scopeID, snapshot.Lineage, moduleAddress),
		ModuleAddress:    moduleAddress,
		ResourceCount:    int64(resourceCount),
		Lineage:          snapshot.Lineage,
		Serial:           snapshot.Serial,
		BackendKind:      snapshot.BackendKind,
		LocatorHash:      snapshot.LocatorHash,
		StatePath:        snapshot.StatePath,
		SourceFactID:     envelope.FactID,
		StableFactKey:    envelope.StableFactKey,
		SourceSystem:     sourceSystem,
		SourceRecordID:   envelope.SourceRef.SourceRecordID,
		SourceConfidence: envelope.SourceConfidence,
		CollectorKind:    envelope.CollectorKind,
		ObservedAt:       envelope.ObservedAt,
	}, true
}

func aggregateTerraformStateModuleRows(rows []TerraformStateModuleRow) []TerraformStateModuleRow {
	if len(rows) == 0 {
		return nil
	}
	byModule := make(map[string]TerraformStateModuleRow, len(rows))
	for _, row := range rows {
		existing, ok := byModule[row.ModuleAddress]
		if !ok {
			byModule[row.ModuleAddress] = row
			continue
		}
		existing.ResourceCount += row.ResourceCount
		if row.ObservedAt.After(existing.ObservedAt) {
			existing.SourceFactID = row.SourceFactID
			existing.StableFactKey = row.StableFactKey
			existing.SourceRecordID = row.SourceRecordID
			existing.ObservedAt = row.ObservedAt
		}
		byModule[row.ModuleAddress] = existing
	}
	moduleAddresses := make([]string, 0, len(byModule))
	for moduleAddress := range byModule {
		moduleAddresses = append(moduleAddresses, moduleAddress)
	}
	sort.Strings(moduleAddresses)
	aggregated := make([]TerraformStateModuleRow, 0, len(moduleAddresses))
	for _, moduleAddress := range moduleAddresses {
		aggregated = append(aggregated, byModule[moduleAddress])
	}
	return aggregated
}

func terraformStateOutputRow(
	scopeID string,
	snapshot terraformStateSnapshotContext,
	envelope facts.Envelope,
) (TerraformStateOutputRow, bool) {
	name, _ := payloadString(envelope.Payload, "name")
	if name == "" {
		return TerraformStateOutputRow{}, false
	}
	sensitive := false
	if ptr := payloadBoolPtr(envelope.Payload, "sensitive"); ptr != nil {
		sensitive = *ptr
	}
	valueShape, _ := payloadString(envelope.Payload, "value_shape")
	if valueShape == "" && payloadHasKey(envelope.Payload, "value") {
		valueShape = "scalar"
		if sensitive {
			valueShape = "redacted_scalar"
		}
	}
	sourceSystem := terraformStateSourceSystem(envelope)
	return TerraformStateOutputRow{
		UID:              terraformStateUID("output", scopeID, snapshot.Lineage, name),
		Name:             name,
		Sensitive:        sensitive,
		ValueShape:       valueShape,
		Lineage:          snapshot.Lineage,
		Serial:           snapshot.Serial,
		BackendKind:      snapshot.BackendKind,
		LocatorHash:      snapshot.LocatorHash,
		StatePath:        snapshot.StatePath,
		SourceFactID:     envelope.FactID,
		StableFactKey:    envelope.StableFactKey,
		SourceSystem:     sourceSystem,
		SourceRecordID:   envelope.SourceRef.SourceRecordID,
		SourceConfidence: envelope.SourceConfidence,
		CollectorKind:    envelope.CollectorKind,
		ObservedAt:       envelope.ObservedAt,
	}, true
}

func terraformStateSourceSystem(envelope facts.Envelope) string {
	if sourceSystem := strings.TrimSpace(envelope.SourceRef.SourceSystem); sourceSystem != "" {
		return sourceSystem
	}
	return strings.TrimSpace(envelope.CollectorKind)
}

func terraformStatePath(backendKind, locatorHash, scopeID string) string {
	backendKind = strings.TrimSpace(backendKind)
	locatorHash = strings.TrimSpace(locatorHash)
	if backendKind != "" && locatorHash != "" {
		return "tfstate://" + backendKind + "/" + locatorHash
	}
	return "tfstate://" + strings.TrimSpace(scopeID)
}

func terraformStateUID(kind, scopeID, lineage, key string) string {
	return facts.StableID("TerraformStateCanonicalNode", map[string]any{
		"kind":    kind,
		"scope":   scopeID,
		"lineage": lineage,
		"key":     key,
	})
}

func terraformStateCorrelationAnchors(payload map[string]any) []string {
	raw, ok := payload["correlation_anchors"].([]any)
	if !ok || len(raw) == 0 {
		return nil
	}
	anchors := make([]string, 0, len(raw))
	for _, item := range raw {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		kind, _ := payloadString(entry, "anchor_kind")
		hash, _ := payloadString(entry, "value_hash")
		if kind == "" || hash == "" {
			continue
		}
		anchors = append(anchors, kind+":"+hash)
	}
	sort.Strings(anchors)
	if len(anchors) == 0 {
		return nil
	}
	return anchors
}

func terraformStateTagHashesByResource(envelopes []facts.Envelope) map[string][]string {
	tagHashes := map[string][]string{}
	seen := map[string]struct{}{}
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.TerraformStateTagObservationFactKind {
			continue
		}
		address, _ := payloadString(envelope.Payload, "resource_address")
		hash, _ := payloadString(envelope.Payload, "tag_key_hash")
		if address == "" || hash == "" {
			continue
		}
		key := address + "\x00" + hash
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		tagHashes[address] = append(tagHashes[address], hash)
	}
	for address := range tagHashes {
		sort.Strings(tagHashes[address])
	}
	return tagHashes
}
