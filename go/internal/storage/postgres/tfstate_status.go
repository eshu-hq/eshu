package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

// listTerraformStateLastSerials returns the most recent observed serial per
// state_snapshot scope keyed by safe_locator_hash. The query bounds itself to
// active or pending generations and is bounded by the number of distinct
// Terraform-state scopes; no per-locator limit is needed because the result
// already reduces to one row per locator.
func listTerraformStateLastSerials(
	ctx context.Context,
	queryer Queryer,
) ([]statuspkg.TerraformStateLocatorSerial, error) {
	rows, err := queryer.QueryContext(ctx, terraformStateLastSerialQuery)
	if err != nil {
		return nil, fmt.Errorf("list terraform state last serials: %w", err)
	}
	defer func() { _ = rows.Close() }()

	serials := []statuspkg.TerraformStateLocatorSerial{}
	for rows.Next() {
		var locatorHash string
		var backendKind string
		var lineage string
		var serialText string
		var generationID string
		var observedAt sql.NullTime
		if scanErr := rows.Scan(
			&locatorHash,
			&backendKind,
			&lineage,
			&serialText,
			&generationID,
			&observedAt,
		); scanErr != nil {
			return nil, fmt.Errorf("list terraform state last serials: %w", scanErr)
		}
		serial, parseErr := strconv.ParseInt(strings.TrimSpace(serialText), 10, 64)
		if parseErr != nil {
			// Skip rows with malformed generation IDs rather than failing the
			// whole admin status query; this is observability data.
			continue
		}
		row := statuspkg.TerraformStateLocatorSerial{
			SafeLocatorHash: strings.TrimSpace(locatorHash),
			BackendKind:     strings.TrimSpace(backendKind),
			Lineage:         strings.TrimSpace(lineage),
			Serial:          serial,
			GenerationID:    strings.TrimSpace(generationID),
		}
		if observedAt.Valid {
			row.ObservedAt = observedAt.Time.UTC()
		}
		serials = append(serials, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list terraform state last serials: %w", err)
	}
	return serials, nil
}

// listTerraformStateRecentWarnings returns up to limit warning_fact rows per
// safe_locator_hash. Limit must be positive; callers that want the contract
// default should pass statuspkg.MaxTerraformStateRecentWarnings.
func listTerraformStateRecentWarnings(
	ctx context.Context,
	queryer Queryer,
	limit int,
) ([]statuspkg.TerraformStateLocatorWarning, error) {
	if limit <= 0 {
		limit = statuspkg.MaxTerraformStateRecentWarnings
	}
	rows, err := queryer.QueryContext(ctx, terraformStateRecentWarningsQuery, limit)
	if err != nil {
		return nil, fmt.Errorf("list terraform state recent warnings: %w", err)
	}
	defer func() { _ = rows.Close() }()

	warnings := []statuspkg.TerraformStateLocatorWarning{}
	for rows.Next() {
		var locatorHash string
		var backendKind string
		var warningKind string
		var reason string
		var source string
		var generationID string
		var observedAt sql.NullTime
		if scanErr := rows.Scan(
			&locatorHash,
			&backendKind,
			&warningKind,
			&reason,
			&source,
			&generationID,
			&observedAt,
		); scanErr != nil {
			return nil, fmt.Errorf("list terraform state recent warnings: %w", scanErr)
		}
		row := statuspkg.TerraformStateLocatorWarning{
			SafeLocatorHash: strings.TrimSpace(locatorHash),
			BackendKind:     strings.TrimSpace(backendKind),
			WarningKind:     strings.TrimSpace(warningKind),
			Reason:          strings.TrimSpace(reason),
			Source:          strings.TrimSpace(source),
			GenerationID:    strings.TrimSpace(generationID),
		}
		if observedAt.Valid {
			row.ObservedAt = observedAt.Time.UTC()
		}
		warnings = append(warnings, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list terraform state recent warnings: %w", err)
	}
	return warnings, nil
}

// terraformStateAdminEvidence is the bounded admin-status evidence shape
// returned by ReadTerraformStateAdminEvidence. Callers can either use the
// helper or inline the two list calls when they need to control timing.
type terraformStateAdminEvidence struct {
	LastSerials    []statuspkg.TerraformStateLocatorSerial
	RecentWarnings []statuspkg.TerraformStateLocatorWarning
}

// readTerraformStateAdminEvidence calls both query helpers in a single call so
// status readers can populate RawSnapshot.TerraformStateLastSerials and
// RawSnapshot.TerraformStateRecentWarnings in one place. Returns no error when
// either list is empty so admin status remains useful even on a fresh database.
func readTerraformStateAdminEvidence(
	ctx context.Context,
	queryer Queryer,
	limit int,
	asOf time.Time,
) (terraformStateAdminEvidence, error) {
	_ = asOf // reserved for future bounded-window queries.
	serials, err := listTerraformStateLastSerials(ctx, queryer)
	if err != nil {
		return terraformStateAdminEvidence{}, err
	}
	warnings, err := listTerraformStateRecentWarnings(ctx, queryer, limit)
	if err != nil {
		return terraformStateAdminEvidence{}, err
	}
	return terraformStateAdminEvidence{
		LastSerials:    serials,
		RecentWarnings: warnings,
	}, nil
}
