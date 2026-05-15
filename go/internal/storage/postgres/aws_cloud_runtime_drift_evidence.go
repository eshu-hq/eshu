package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/correlation/drift/cloudruntime"
	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// AWSCloudRuntimeDriftConfigResolver resolves a Terraform state backend to the
// latest config commit that owns it.
type AWSCloudRuntimeDriftConfigResolver interface {
	ResolveConfigCommitForBackend(
		ctx context.Context,
		backendKind string,
		locatorHash string,
	) (tfstatebackend.CommitAnchor, error)
}

// PostgresAWSCloudRuntimeDriftEvidenceLoader builds the ARN-keyed join for the
// aws_cloud_runtime_drift reducer domain. It loads AWS resources from one AWS
// generation, active Terraform-state rows for only those ARNs, and config rows
// from the resolved backend owner when Eshu can prove one.
type PostgresAWSCloudRuntimeDriftEvidenceLoader struct {
	DB Queryer
	// ConfigResolver anchors a state_snapshot:* scope to the owning repo
	// snapshot. Nil or unresolved ownership suppresses unmanaged findings for
	// state-backed resources because absence of config is not proven.
	ConfigResolver AWSCloudRuntimeDriftConfigResolver
	Tracer         trace.Tracer
	Logger         *slog.Logger
	// Instruments is passed into the Terraform config loader so reused
	// module-prefix joining preserves unresolved-module observability.
	Instruments *telemetry.Instruments
}

type awsRuntimeStateResourceRow struct {
	scopeID      string
	generationID string
	resource     *cloudruntime.ResourceRow
}

// LoadAWSCloudRuntimeDriftEvidence implements reducer.AWSCloudRuntimeDriftEvidenceLoader.
func (l PostgresAWSCloudRuntimeDriftEvidenceLoader) LoadAWSCloudRuntimeDriftEvidence(
	ctx context.Context,
	scopeID string,
	generationID string,
) ([]cloudruntime.AddressedRow, error) {
	if l.DB == nil {
		return nil, fmt.Errorf("aws cloud runtime drift evidence database is required")
	}
	scopeID = strings.TrimSpace(scopeID)
	generationID = strings.TrimSpace(generationID)
	if scopeID == "" {
		return nil, fmt.Errorf("aws scope ID must not be blank")
	}
	if generationID == "" {
		return nil, fmt.Errorf("aws generation ID must not be blank")
	}

	if l.Tracer != nil {
		var span trace.Span
		ctx, span = l.Tracer.Start(ctx, telemetry.SpanReducerAWSRuntimeDriftEvidenceLoad)
		defer span.End()
	}

	cloudByARN, arns, err := l.loadAWSRuntimeResources(ctx, scopeID, generationID)
	if err != nil {
		return nil, err
	}
	if len(arns) == 0 {
		return nil, nil
	}

	stateByARN, err := l.loadActiveStateResourcesByARN(ctx, scopeID, generationID, arns)
	if err != nil {
		return nil, err
	}
	configByStateScope, err := l.loadConfigByStateScope(ctx, stateByARN)
	if err != nil {
		return nil, err
	}

	rows := make([]cloudruntime.AddressedRow, 0, len(arns))
	for _, arn := range arns {
		row := cloudruntime.AddressedRow{
			ARN:          arn,
			ResourceType: cloudByARN[arn].ResourceType,
			Cloud:        cloudByARN[arn],
		}
		for _, stateRow := range stateByARN[arn] {
			if row.State == nil {
				row.State = stateRow.resource
			}
			if awsRuntimeHasConflictingStateRows(stateByARN[arn]) {
				row.FindingKind = cloudruntime.FindingKindAmbiguousCloudResource
				row.ManagementStatus = cloudruntime.ManagementStatusAmbiguous
				row.MissingEvidence = append(row.MissingEvidence, "single_terraform_state_owner")
				row.WarningFlags = append(row.WarningFlags, "ambiguous_terraform_state_owner")
				continue
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
		rows = append(rows, row)
	}
	return rows, nil
}

func (l PostgresAWSCloudRuntimeDriftEvidenceLoader) loadAWSRuntimeResources(
	ctx context.Context,
	scopeID string,
	generationID string,
) (map[string]*cloudruntime.ResourceRow, []string, error) {
	rows, err := l.DB.QueryContext(ctx, listAWSCloudRuntimeResourcesForGenerationQuery, scopeID, generationID)
	if err != nil {
		return nil, nil, fmt.Errorf("list aws runtime resources: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := map[string]*cloudruntime.ResourceRow{}
	for rows.Next() {
		var arn string
		var payload []byte
		if err := rows.Scan(&arn, &payload); err != nil {
			return nil, nil, fmt.Errorf("scan aws runtime resource: %w", err)
		}
		row, ok := awsRuntimeResourceRowFromPayload(scopeID, payload)
		if !ok {
			l.logDecodeFailure(ctx, scopeID, generationID, arn, "aws_resource_payload_decode")
			continue
		}
		if _, exists := out[row.ARN]; !exists {
			out[row.ARN] = row
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate aws runtime resources: %w", err)
	}
	arns := make([]string, 0, len(out))
	for arn := range out {
		arns = append(arns, arn)
	}
	sort.Strings(arns)
	return out, arns, nil
}

func (l PostgresAWSCloudRuntimeDriftEvidenceLoader) loadActiveStateResourcesByARN(
	ctx context.Context,
	awsScopeID string,
	awsGenerationID string,
	arns []string,
) (map[string][]awsRuntimeStateResourceRow, error) {
	encoded, err := json.Marshal(arns)
	if err != nil {
		return nil, fmt.Errorf("marshal aws arn allowlist: %w", err)
	}
	rows, err := l.DB.QueryContext(
		ctx,
		listActiveStateResourcesForAWSARNsQuery,
		awsScopeID,
		awsGenerationID,
		string(encoded),
	)
	if err != nil {
		return nil, fmt.Errorf("list active terraform state resources for aws arns: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := map[string][]awsRuntimeStateResourceRow{}
	for rows.Next() {
		var scopeID, generationID, address string
		var payload []byte
		if err := rows.Scan(&scopeID, &generationID, &address, &payload); err != nil {
			return nil, fmt.Errorf("scan active terraform state resource for aws arn: %w", err)
		}
		resource, ok := awsRuntimeStateRowFromPayload(scopeID, address, payload)
		if !ok {
			l.logDecodeFailure(ctx, scopeID, generationID, address, "state_resource_payload_decode")
			continue
		}
		out[resource.ARN] = append(out[resource.ARN], awsRuntimeStateResourceRow{
			scopeID:      strings.TrimSpace(scopeID),
			generationID: strings.TrimSpace(generationID),
			resource:     resource,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active terraform state resources for aws arns: %w", err)
	}
	return out, nil
}

type awsRuntimeConfigRows struct {
	byAddress  map[string]*cloudruntime.ResourceRow
	ownerIssue cloudruntime.FindingKind
}

func (l PostgresAWSCloudRuntimeDriftEvidenceLoader) loadConfigByStateScope(
	ctx context.Context,
	stateByARN map[string][]awsRuntimeStateResourceRow,
) (map[string]awsRuntimeConfigRows, error) {
	out := map[string]awsRuntimeConfigRows{}
	for _, stateRows := range stateByARN {
		for _, stateRow := range stateRows {
			if _, seen := out[stateRow.scopeID]; seen {
				continue
			}
			backendKind, locatorHash, ok := parseStateSnapshotScope(stateRow.scopeID)
			if !ok || l.ConfigResolver == nil {
				out[stateRow.scopeID] = awsRuntimeConfigRows{
					ownerIssue: cloudruntime.FindingKindUnknownCloudResource,
				}
				continue
			}
			anchor, err := l.ConfigResolver.ResolveConfigCommitForBackend(ctx, backendKind, locatorHash)
			switch {
			case errors.Is(err, tfstatebackend.ErrAmbiguousBackendOwner):
				out[stateRow.scopeID] = awsRuntimeConfigRows{
					ownerIssue: cloudruntime.FindingKindAmbiguousCloudResource,
				}
				continue
			case errors.Is(err, tfstatebackend.ErrNoConfigRepoOwnsBackend):
				out[stateRow.scopeID] = awsRuntimeConfigRows{
					ownerIssue: cloudruntime.FindingKindUnknownCloudResource,
				}
				continue
			case err != nil:
				return nil, fmt.Errorf("resolve aws runtime drift config owner: %w", err)
			}
			configRows, err := l.loadConfigRowsForAnchor(ctx, anchor)
			if err != nil {
				return nil, err
			}
			out[stateRow.scopeID] = awsRuntimeConfigRows{byAddress: configRows}
		}
	}
	return out, nil
}

func awsRuntimeHasConflictingStateRows(rows []awsRuntimeStateResourceRow) bool {
	if len(rows) < 2 {
		return false
	}
	first := awsRuntimeStateConflictKey(rows[0])
	for _, row := range rows[1:] {
		if awsRuntimeStateConflictKey(row) != first {
			return true
		}
	}
	return false
}

func awsRuntimeStateConflictKey(row awsRuntimeStateResourceRow) string {
	return strings.Join([]string{
		strings.TrimSpace(row.scopeID),
		strings.TrimSpace(row.generationID),
		strings.TrimSpace(row.resource.Address),
	}, "\x00")
}

func awsRuntimeManagementStatusForOwnerIssue(kind cloudruntime.FindingKind) string {
	switch kind {
	case cloudruntime.FindingKindAmbiguousCloudResource:
		return cloudruntime.ManagementStatusAmbiguous
	case cloudruntime.FindingKindUnknownCloudResource:
		return cloudruntime.ManagementStatusUnknown
	default:
		return ""
	}
}

func awsRuntimeMissingEvidenceForOwnerIssue(kind cloudruntime.FindingKind) string {
	switch kind {
	case cloudruntime.FindingKindAmbiguousCloudResource:
		return "single_terraform_config_owner"
	case cloudruntime.FindingKindUnknownCloudResource:
		return "terraform_config_owner"
	default:
		return ""
	}
}

func awsRuntimeWarningFlagForOwnerIssue(kind cloudruntime.FindingKind) string {
	switch kind {
	case cloudruntime.FindingKindAmbiguousCloudResource:
		return "ambiguous_terraform_backend_owner"
	case cloudruntime.FindingKindUnknownCloudResource:
		return "unresolved_terraform_backend_owner"
	default:
		return ""
	}
}

func (l PostgresAWSCloudRuntimeDriftEvidenceLoader) loadConfigRowsForAnchor(
	ctx context.Context,
	anchor tfstatebackend.CommitAnchor,
) (map[string]*cloudruntime.ResourceRow, error) {
	loader := PostgresDriftEvidenceLoader{
		DB:          l.DB,
		Logger:      l.Logger,
		Instruments: l.Instruments,
	}
	recorder := loader.unresolvedRecorder()
	prefixMap, err := loader.buildModulePrefixMap(ctx, anchor.ScopeID, anchor.CommitID, recorder)
	if err != nil {
		return nil, err
	}
	configRows, err := loader.loadConfigByAddress(ctx, anchor.ScopeID, anchor.CommitID, prefixMap)
	if err != nil {
		return nil, err
	}
	out := make(map[string]*cloudruntime.ResourceRow, len(configRows))
	for address, row := range configRows {
		out[address] = &cloudruntime.ResourceRow{
			Address:      row.Address,
			ResourceType: row.ResourceType,
			ScopeID:      anchor.ScopeID,
		}
	}
	return out, nil
}

func awsRuntimeResourceRowFromPayload(scopeID string, payload []byte) (*cloudruntime.ResourceRow, bool) {
	var decoded struct {
		ARN          string         `json:"arn"`
		ResourceID   string         `json:"resource_id"`
		ResourceType string         `json:"resource_type"`
		Tags         map[string]any `json:"tags"`
	}
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &decoded); err != nil {
			return nil, false
		}
	}
	arn := strings.TrimSpace(decoded.ARN)
	if arn == "" {
		return nil, false
	}
	return &cloudruntime.ResourceRow{
		ARN:          arn,
		ResourceID:   strings.TrimSpace(decoded.ResourceID),
		ResourceType: strings.TrimSpace(decoded.ResourceType),
		ScopeID:      strings.TrimSpace(scopeID),
		Tags:         coerceStringTags(decoded.Tags),
	}, true
}

func awsRuntimeStateRowFromPayload(scopeID, address string, payload []byte) (*cloudruntime.ResourceRow, bool) {
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
	arn := strings.TrimSpace(coerceJSONString(decoded.Attributes["arn"]))
	if address == "" || arn == "" {
		return nil, false
	}
	return &cloudruntime.ResourceRow{
		ARN:          arn,
		Address:      address,
		ResourceType: strings.TrimSpace(decoded.Type),
		ScopeID:      strings.TrimSpace(scopeID),
	}, true
}

func coerceStringTags(tags map[string]any) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	out := make(map[string]string, len(tags))
	for key, value := range tags {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out[key] = coerceJSONString(value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parseStateSnapshotScope(scopeID string) (backendKind, locatorHash string, ok bool) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(scopeID), "state_snapshot:")
	if !ok {
		return "", "", false
	}
	backendKind, locatorHash, ok = strings.Cut(rest, ":")
	backendKind = strings.TrimSpace(backendKind)
	locatorHash = strings.TrimSpace(locatorHash)
	return backendKind, locatorHash, ok && backendKind != "" && locatorHash != ""
}

func (l PostgresAWSCloudRuntimeDriftEvidenceLoader) logDecodeFailure(
	ctx context.Context,
	scopeID string,
	generationID string,
	identity string,
	failureClass string,
) {
	if l.Logger == nil {
		return
	}
	l.Logger.LogAttrs(ctx, slog.LevelWarn, "aws runtime drift evidence loader skipped resource",
		slog.String(telemetry.LogKeyScopeID, scopeID),
		slog.String(telemetry.LogKeyGenerationID, generationID),
		slog.String("resource.identity", identity),
		slog.String(telemetry.LogKeyFailureClass, failureClass),
	)
}
