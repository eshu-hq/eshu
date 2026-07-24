// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/correlation/cloudinventory"
	"github.com/eshu-hq/eshu/go/internal/correlation/drift/cloudruntime"
	"github.com/eshu-hq/eshu/go/internal/correlation/drift/multicloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// PostgresMultiCloudRuntimeDriftEvidenceLoader builds the canonical
// cloud_resource_uid-keyed join for the multi_cloud_runtime_drift reducer domain
// (issues #1997, #1998). It loads observed provider inventory facts (AWS, GCP,
// Azure) for one collector generation, active Terraform-state rows whose
// provider-native identity resolves into the same uid keyspace, and config rows
// from the resolved Terraform backend owner. Observed and Terraform layers join
// on one cloud_resource_uid resolved by cloudinventory; the loader never invents
// a second keyspace and never fabricates a uid for an unresolved identity.
//
// The loader is read-only and side-effect free. It mirrors
// PostgresAWSCloudRuntimeDriftEvidenceLoader's state/config resolution but keys
// every layer on the provider-neutral canonical uid instead of an ARN, so AWS,
// GCP, and Azure share one drift path.
type PostgresMultiCloudRuntimeDriftEvidenceLoader struct {
	// DB executes the bounded source-fact reads.
	DB Queryer
	// ConfigResolver anchors a state_snapshot:* scope to the owning repo config
	// snapshot. Nil or unresolved ownership marks state-backed resources unknown
	// because absence of config is not proven.
	ConfigResolver AWSCloudRuntimeDriftConfigResolver
	// Tracer, when set, wraps the load so an operator can see which sub-scan is
	// slow without instrumenting each call site.
	Tracer trace.Tracer
	// Logger, when set, records bounded skip diagnostics for undecodable rows.
	Logger *slog.Logger
	// Instruments is threaded into the reused Terraform config loader so its
	// unresolved-module observability stays wired.
	Instruments *telemetry.Instruments
}

// multiCloudObservedRow couples one resolved observed inventory fact with the
// canonical uid it keys into and the provider raw identity that produced it.
type multiCloudObservedRow struct {
	uid          string
	provider     string
	rawIdentity  string
	resourceType string
	resource     *cloudruntime.ResourceRow
}

// multiCloudStateRow couples one active Terraform-state resource with the
// canonical uid its provider-native identity resolved to. scopeID and
// generationID anchor the config-owner resolution and conflict detection.
type multiCloudStateRow struct {
	uid          string
	scopeID      string
	generationID string
	resource     *cloudruntime.ResourceRow
}

// LoadMultiCloudRuntimeDriftEvidence implements
// reducer.MultiCloudRuntimeDriftEvidenceLoader. It returns one multicloud.Row per
// resolved canonical identity in the runtime scope, joining observed, Terraform
// state, and Terraform config layers on cloud_resource_uid.
func (l PostgresMultiCloudRuntimeDriftEvidenceLoader) LoadMultiCloudRuntimeDriftEvidence(
	ctx context.Context,
	scopeID string,
	generationID string,
) ([]multicloud.Row, error) {
	if l.DB == nil {
		return nil, fmt.Errorf("multi cloud runtime drift evidence database is required")
	}
	scopeID = strings.TrimSpace(scopeID)
	generationID = strings.TrimSpace(generationID)
	if scopeID == "" {
		return nil, fmt.Errorf("multi cloud scope ID must not be blank")
	}
	if generationID == "" {
		return nil, fmt.Errorf("multi cloud generation ID must not be blank")
	}

	if l.Tracer != nil {
		var span trace.Span
		ctx, span = l.Tracer.Start(ctx, telemetry.SpanReducerMultiCloudRuntimeDriftEvidenceLoad)
		defer span.End()
	}

	observedByUID, uids, identityByUID, err := l.loadObservedResources(ctx, scopeID, generationID)
	if err != nil {
		return nil, err
	}
	if len(uids) == 0 {
		return nil, nil
	}

	stateByUID, err := l.loadActiveStateResourcesByUID(ctx, identityByUID)
	if err != nil {
		return nil, err
	}
	configByStateScope, err := l.loadConfigByStateScope(ctx, stateByUID)
	if err != nil {
		return nil, err
	}

	rows := make([]multicloud.Row, 0, len(uids))
	for _, uid := range uids {
		observed := observedByUID[uid]
		row := multicloud.Row{
			Provider:         observed.provider,
			RawIdentity:      observed.rawIdentity,
			CloudResourceUID: uid,
			ResourceType:     observed.resourceType,
			ScopeID:          scopeID,
			Cloud:            observed.resource,
		}
		applyMultiCloudStateAndConfig(&row, stateByUID[uid], configByStateScope)
		row.WarningFlags = append(row.WarningFlags, containerImagesTruncatedWarning(row.Cloud, row.State)...)
		rows = append(rows, row)
	}
	return rows, nil
}

// applyMultiCloudStateAndConfig folds the joined Terraform-state rows for one uid
// onto the row, attaching config when the resolved backend owner declares the
// state address and marking conflicting state owners ambiguous. It mirrors the
// AWS loader fold so AWS and multi-cloud agree on the state/config decision.
func applyMultiCloudStateAndConfig(
	row *multicloud.Row,
	stateRows []multiCloudStateRow,
	configByStateScope map[string]awsRuntimeConfigRows,
) {
	if multiCloudHasConflictingStateRows(stateRows) {
		row.FindingKind = cloudruntime.FindingKindAmbiguousCloudResource
		row.ManagementStatus = cloudruntime.ManagementStatusAmbiguous
		row.MissingEvidence = append(row.MissingEvidence, "single_terraform_state_owner")
		row.WarningFlags = append(row.WarningFlags, "ambiguous_terraform_state_owner")
		return
	}
	for _, stateRow := range stateRows {
		if row.State == nil {
			row.State = stateRow.resource
		}
		config, ok := configByStateScope[stateRow.scopeID]
		if !ok {
			continue
		}
		if config.ownerIssue != "" {
			row.FindingKind = config.ownerIssue
			row.ManagementStatus = awsRuntimeManagementStatusForOwnerIssue(config.ownerIssue)
			row.MissingEvidence = append(row.MissingEvidence, awsRuntimeMissingEvidenceForOwnerIssue(config.ownerIssue))
			row.WarningFlags = append(row.WarningFlags, awsRuntimeWarningFlagForOwnerIssue(config.ownerIssue))
			continue
		}
		if configRow := config.byAddress[stateRow.resource.Address]; configRow != nil {
			row.Config = configRow
		}
	}
}

func (l PostgresMultiCloudRuntimeDriftEvidenceLoader) loadObservedResources(
	ctx context.Context,
	scopeID string,
	generationID string,
) (map[string]multiCloudObservedRow, []string, map[string]string, error) {
	rows, err := l.DB.QueryContext(ctx, listMultiCloudObservedResourcesForGenerationQuery, scopeID, generationID)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("list multi cloud observed resources: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := map[string]multiCloudObservedRow{}
	identityByUID := map[string]string{}
	for rows.Next() {
		var factKind, rawIdentity string
		var payload []byte
		if err := rows.Scan(&factKind, &rawIdentity, &payload); err != nil {
			return nil, nil, nil, fmt.Errorf("scan multi cloud observed resource: %w", err)
		}
		observed, ok := multiCloudObservedRowFromRow(scopeID, factKind, rawIdentity, payload)
		if !ok {
			l.logSkippedRow(ctx, scopeID, generationID, factKind, rawIdentity, "multi_cloud_observed_unresolved")
			continue
		}
		// First-write-wins on uid: a duplicate identity in one generation is a
		// collector defect, not a join signal.
		if _, exists := out[observed.uid]; !exists {
			out[observed.uid] = observed
			identityByUID[observed.uid] = observed.rawIdentity
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, nil, fmt.Errorf("iterate multi cloud observed resources: %w", err)
	}
	uids := make([]string, 0, len(out))
	for uid := range out {
		uids = append(uids, uid)
	}
	sort.Strings(uids)
	return out, uids, identityByUID, nil
}

// loadActiveStateResourcesByUID joins active Terraform-state rows to observed
// uids through an allowlist of the observed provider raw identities. AWS ARNs and
// GCP full resource names are case-significant and match exactly. Azure ARM ids
// are case-insensitive per Azure, and the shared cloud_resource_uid keyspace
// lower-cases them before hashing, so the Azure side of the join is case-folded:
// the SQL surfaces a /subscriptions/-rooted state row whose attributes.id differs
// only in casing from the observed arm_resource_id, and this loader maps the
// returned state identity back to the observed uid through an exact lookup first
// and an Azure-only case-folded lookup second (see uidForMatchedStateIdentity). A
// state identity that resolves to no observed uid carries no canonical join key
// and is dropped rather than guessed onto the wrong resource.
func (l PostgresMultiCloudRuntimeDriftEvidenceLoader) loadActiveStateResourcesByUID(
	ctx context.Context,
	identityByUID map[string]string,
) (map[string][]multiCloudStateRow, error) {
	identities := make([]string, 0, len(identityByUID))
	uidByIdentity := make(map[string]string, len(identityByUID))
	uidByAzureFold := make(map[string]string, len(identityByUID))
	for uid, identity := range identityByUID {
		identities = append(identities, identity)
		uidByIdentity[identity] = uid
		if key, ok := azureStateFoldKey(identity); ok {
			uidByAzureFold[key] = uid
		}
	}
	sort.Strings(identities)

	encoded, err := json.Marshal(identities)
	if err != nil {
		return nil, fmt.Errorf("marshal multi cloud identity allowlist: %w", err)
	}
	rows, err := l.DB.QueryContext(
		ctx,
		listActiveStateResourcesForMultiCloudIdentitiesQuery,
		string(encoded),
	)
	if err != nil {
		return nil, fmt.Errorf("list active terraform state resources for multi cloud identities: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := map[string][]multiCloudStateRow{}
	for rows.Next() {
		var scopeID, generationID, address, matchedIdentity string
		var payload []byte
		if err := rows.Scan(&scopeID, &generationID, &address, &matchedIdentity, &payload); err != nil {
			return nil, fmt.Errorf("scan active terraform state resource for multi cloud identity: %w", err)
		}
		uid, ok := uidForMatchedStateIdentity(uidByIdentity, uidByAzureFold, matchedIdentity)
		if !ok {
			// The matched identity is not the one that produced an observed uid
			// (for example a coincidental cross-provider attribute collision); the
			// row carries no canonical join key and is dropped.
			continue
		}
		resource, ok := multiCloudStateRowFromPayload(scopeID, address, payload)
		if !ok {
			l.logSkippedRow(ctx, scopeID, generationID, "terraform_state_resource", address, "multi_cloud_state_payload_decode")
			continue
		}
		out[uid] = append(out[uid], multiCloudStateRow{
			uid:          uid,
			scopeID:      strings.TrimSpace(scopeID),
			generationID: strings.TrimSpace(generationID),
			resource:     resource,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active terraform state resources for multi cloud identity: %w", err)
	}
	return out, nil
}

// loadConfigByStateScope resolves config rows for each distinct Terraform-state
// scope by delegating to the AWS loader's backend-owner resolution, which is
// provider-neutral: it parses the state_snapshot scope, resolves the owning
// config commit, and loads config rows keyed by Terraform address. Reusing it
// keeps AWS and multi-cloud on one config-ownership decision.
func (l PostgresMultiCloudRuntimeDriftEvidenceLoader) loadConfigByStateScope(
	ctx context.Context,
	stateByUID map[string][]multiCloudStateRow,
) (map[string]awsRuntimeConfigRows, error) {
	awsStateByARN := map[string][]awsRuntimeStateResourceRow{}
	for uid, stateRows := range stateByUID {
		for _, stateRow := range stateRows {
			awsStateByARN[uid] = append(awsStateByARN[uid], awsRuntimeStateResourceRow{
				scopeID:      stateRow.scopeID,
				generationID: stateRow.generationID,
				resource:     stateRow.resource,
			})
		}
	}
	awsLoader := PostgresAWSCloudRuntimeDriftEvidenceLoader(l)
	return awsLoader.loadConfigByStateScope(ctx, awsStateByARN)
}

func multiCloudHasConflictingStateRows(rows []multiCloudStateRow) bool {
	if len(rows) < 2 {
		return false
	}
	first := multiCloudStateConflictKey(rows[0])
	for _, row := range rows[1:] {
		if multiCloudStateConflictKey(row) != first {
			return true
		}
	}
	return false
}

func multiCloudStateConflictKey(row multiCloudStateRow) string {
	return strings.Join([]string{
		strings.TrimSpace(row.scopeID),
		strings.TrimSpace(row.generationID),
		strings.TrimSpace(row.resource.Address),
	}, "\x00")
}

// multiCloudSourceFactProvider maps one inventory source fact kind to its
// normalized provider token. The set is closed and kept in lockstep with the SQL
// allowlist so a new provider must be added in both places.
var multiCloudSourceFactProvider = map[string]string{
	facts.AWSResourceFactKind:        cloudinventory.ProviderAWS,
	facts.GCPCloudResourceFactKind:   cloudinventory.ProviderGCP,
	facts.AzureCloudResourceFactKind: cloudinventory.ProviderAzure,
}

// multiCloudObservedRowFromRow maps one observed inventory source-fact row into
// the resolved observed view. Rows whose identity does not resolve into the
// shared canonical uid keyspace are dropped so the join never receives evidence
// it cannot key.
func multiCloudObservedRowFromRow(
	scopeID string,
	factKind string,
	rawIdentity string,
	payload []byte,
) (multiCloudObservedRow, bool) {
	provider, ok := multiCloudSourceFactProvider[factKind]
	if !ok {
		return multiCloudObservedRow{}, false
	}
	rawIdentity = strings.TrimSpace(rawIdentity)
	if rawIdentity == "" {
		return multiCloudObservedRow{}, false
	}
	resolution := cloudinventory.ResolveProviderIdentity(provider, rawIdentity)
	if resolution.Outcome != cloudinventory.ResolutionOutcomeAdmitted {
		return multiCloudObservedRow{}, false
	}

	var decoded map[string]any
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &decoded); err != nil {
			return multiCloudObservedRow{}, false
		}
	}
	resourceType := strings.TrimSpace(coerceJSONString(decoded[multiCloudResourceTypeKey(factKind)]))
	tags := coerceStringTags(multiCloudTagsFromPayload(decoded))
	attributesPayload, _ := decoded["attributes"].(map[string]any)
	attributes, containerImages, truncated := cloudObservedValueAttributes(resourceType, attributesPayload)

	return multiCloudObservedRow{
		uid:          resolution.CloudResourceUID,
		provider:     provider,
		rawIdentity:  rawIdentity,
		resourceType: resourceType,
		resource: &cloudruntime.ResourceRow{
			ARN:                      rawIdentity,
			ResourceType:             resourceType,
			ScopeID:                  strings.TrimSpace(scopeID),
			Tags:                     tags,
			Attributes:               attributes,
			ContainerImages:          containerImages,
			ContainerImagesTruncated: truncated,
		},
	}, true
}

// multiCloudResourceTypeKey returns the payload key each provider stores its
// resource/asset type under.
func multiCloudResourceTypeKey(factKind string) string {
	switch factKind {
	case facts.GCPCloudResourceFactKind:
		return "asset_type"
	default:
		return "resource_type"
	}
}

// multiCloudTagsFromPayload extracts raw provider tags/labels for evidence. Tags
// stay raw source evidence; the loader never normalizes them into environment or
// ownership truth.
func multiCloudTagsFromPayload(decoded map[string]any) map[string]any {
	if decoded == nil {
		return nil
	}
	if tags, ok := decoded["tags"].(map[string]any); ok {
		return tags
	}
	if labels, ok := decoded["labels"].(map[string]any); ok {
		return labels
	}
	return nil
}

// multiCloudStateRowFromPayload decodes one terraform_state_resource payload into
// the normalized address-keyed view. The Terraform address is the meaningful
// declared identity for the config join; a blank address yields no usable row.
func multiCloudStateRowFromPayload(scopeID, address string, payload []byte) (*cloudruntime.ResourceRow, bool) {
	var decoded struct {
		Address    string         `json:"address"`
		Type       string         `json:"type"`
		Attributes map[string]any `json:"attributes"`
	}
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &decoded); err != nil {
			return nil, false
		}
	}
	if decoded.Address != "" {
		address = decoded.Address
	}
	address = strings.TrimSpace(address)
	if address == "" {
		return nil, false
	}
	resourceType := strings.TrimSpace(decoded.Type)
	attributes, containerImages, truncated := stateDeclaredValueAttributes(resourceType, decoded.Attributes)
	return &cloudruntime.ResourceRow{
		Address:                  address,
		ResourceType:             resourceType,
		ScopeID:                  strings.TrimSpace(scopeID),
		Attributes:               attributes,
		ContainerImages:          containerImages,
		ContainerImagesTruncated: truncated,
	}, true
}

func (l PostgresMultiCloudRuntimeDriftEvidenceLoader) logSkippedRow(
	ctx context.Context,
	scopeID string,
	generationID string,
	factKind string,
	identity string,
	failureClass string,
) {
	if l.Logger == nil {
		return
	}
	attrs := []slog.Attr{
		slog.String(telemetry.LogKeyScopeID, scopeID),
		slog.String(telemetry.LogKeyGenerationID, generationID),
		slog.String(telemetry.LogKeyFailureClass, failureClass),
		slog.String("fact_kind", factKind),
	}
	attrs = append(attrs, telemetry.SafeResourceLogAttrs(identity)...)
	l.Logger.LogAttrs(ctx, slog.LevelWarn, "multi cloud runtime drift evidence loader skipped row", attrs...)
}
