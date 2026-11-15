// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package manifest

import (
	"context"
	"database/sql"
	"log/slog"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/packageidentity"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// ManifestDependencyRow is one query-facing declared manifest dependency:
// the package name, the declared version or range, the manifest file that
// declares it, the ecosystem and exact package manager, and the normalized
// dependency scope (runtime, dev, or test). Scope is runtime unless the fact
// marks the dependency a development dependency, and test when the fact's raw
// scope says test. Section stays the raw manifest section (dependencies,
// devDependencies, and so on).
type ManifestDependencyRow struct {
	PackageName    string `json:"package_name"`
	DeclaredRange  string `json:"declared_range,omitempty"`
	ManifestFile   string `json:"manifest_file,omitempty"`
	Ecosystem      string `json:"ecosystem,omitempty"`
	PackageManager string `json:"package_manager,omitempty"`
	Section        string `json:"section,omitempty"`
	Scope          string `json:"scope"`
	FactID         string `json:"fact_id"`
	GenerationID   string `json:"generation_id,omitempty"`
	ObservedAt     string `json:"observed_at,omitempty"`
}

// ManifestDependencyFreshness is the generation state behind one
// repository-scoped manifest dependency page.
type ManifestDependencyFreshness struct {
	ScopeFound         bool   `json:"scope_found"`
	ActiveGenerationID string `json:"active_generation_id,omitempty"`
	GenerationStatus   string `json:"generation_status,omitempty"`
	ObservedAt         string `json:"observed_at,omitempty"`
	FreshnessState     string `json:"freshness_state"`
}

// ManifestDependencyPage is one bounded page of a repository's declared
// manifest dependencies with the freshness behind it.
type ManifestDependencyPage struct {
	Rows      []ManifestDependencyRow
	Freshness ManifestDependencyFreshness
}

// ManifestDependencyStore is the query-local port behind the
// repository-scoped manifest dependency read. fetchLimit is the store's +1
// lookahead bound: the caller passes its page limit plus one and derives
// truncation from the raw fetched count. includeDev selects the dependency
// scope: false (the default) returns only runtime dependencies; true also
// returns dev and test dependencies. refused reports a refused delta base
// (#10109): the page is unusable and the caller must not serve it.
type ManifestDependencyStore interface {
	ListRepositoryManifestDependencyFacts(
		ctx context.Context,
		scopeID string,
		cursorObservedAt *time.Time,
		cursorFactID string,
		fetchLimit int,
		includeDev bool,
	) (ManifestDependencyPage, bool, error)
}

// PostgresManifestDependencyStore adapts the active-generation manifest
// dependency facts in Postgres to the query package's manifest response
// contract. It is the only query type that touches the fact driver.
type PostgresManifestDependencyStore struct {
	store *postgres.FactStore
}

// NewPostgresManifestDependencyStore creates a query adapter over the
// manifest dependency facts in Postgres. A nil database fails the read
// closed instead of panicking.
func NewPostgresManifestDependencyStore(db *sql.DB, instruments *telemetry.Instruments, logger *slog.Logger) *PostgresManifestDependencyStore {
	if db == nil {
		return &PostgresManifestDependencyStore{}
	}
	store := postgres.NewFactStore(postgres.SQLDB{DB: db})
	if instruments != nil {
		store = store.WithInstruments(instruments)
	}
	if logger != nil {
		store = store.WithLogger(logger)
	}
	return &PostgresManifestDependencyStore{store: store}
}

// ListRepositoryManifestDependencyFacts loads one bounded page of the
// repository's declared manifest dependencies with the freshness behind it.
func (s *PostgresManifestDependencyStore) ListRepositoryManifestDependencyFacts(
	ctx context.Context,
	scopeID string,
	cursorObservedAt *time.Time,
	cursorFactID string,
	fetchLimit int,
	includeDev bool,
) (ManifestDependencyPage, bool, error) {
	if s == nil || s.store == nil {
		return ManifestDependencyPage{}, false, nil
	}
	envelopes, freshness, refused, err := s.store.ListActiveRepositoryManifestDependencyFacts(
		ctx, scopeID, cursorObservedAt, cursorFactID, fetchLimit, includeDev)
	if err != nil {
		return ManifestDependencyPage{}, refused, err
	}
	rows := make([]ManifestDependencyRow, 0, len(envelopes))
	for _, envelope := range envelopes {
		rows = append(rows, manifestDependencyRow(envelope.Payload, envelope.FactID, envelope.GenerationID, envelope.ObservedAt))
	}
	return ManifestDependencyPage{
		Rows:      rows,
		Freshness: manifestDependencyFreshness(freshness),
	}, refused, nil
}

// manifestDependencyRow maps one manifest dependency fact payload onto the
// query-facing row. Ecosystem normalizes the stored package manager to
// Eshu's canonical ecosystem; the sbt manager has no ecosystem spelling, so
// it maps to the maven family it joins. The mapping inverts
// packageidentity.ManifestPackageManagersForEcosystem: the maven-family test
// below pins that expansion, so a new family member fails the test instead
// of drifting silently.
func manifestDependencyRow(payload map[string]any, factID, generationID string, observedAt time.Time) ManifestDependencyRow {
	metadata := payloadcore.PayloadMap(payload, "entity_metadata")
	manager := payloadcore.PayloadStr(metadata, "package_manager")
	row := ManifestDependencyRow{
		PackageName:    payloadcore.PayloadStr(payload, "entity_name"),
		DeclaredRange:  payloadcore.PayloadStr(metadata, "version"),
		ManifestFile:   payloadcore.PayloadStr(payload, "relative_path"),
		Ecosystem:      manifestDependencyEcosystem(manager),
		PackageManager: manager,
		Section:        payloadcore.PayloadStr(metadata, "section"),
		Scope: manifestDependencyScope(
			payloadcore.PayloadBool(metadata, "development_dependency"),
			payloadcore.PayloadStr(metadata, "dependency_scope"),
		),
		FactID:       factID,
		GenerationID: generationID,
	}
	if !observedAt.IsZero() {
		row.ObservedAt = observedAt.UTC().Format(time.RFC3339Nano)
	}
	return row
}

// manifestDependencyScope normalizes one manifest dependency's dev-or-runtime
// signal to the query-facing scope: runtime unless the fact marks the
// dependency a development dependency, and test when the fact's raw scope
// carries "test". A missing signal never invents a dev claim.
func manifestDependencyScope(development bool, rawScope string) string {
	if !development {
		return "runtime"
	}
	if strings.Contains(strings.ToLower(rawScope), "test") {
		return "test"
	}
	return "dev"
}

// manifestDependencyEcosystem normalizes one stored package manager to
// Eshu's canonical ecosystem name, or "" when the manager has no ecosystem
// spelling. Callers always see the exact manager beside it.
func manifestDependencyEcosystem(packageManager string) string {
	if ecosystem := packageidentity.NormalizeEcosystem(packageidentity.Ecosystem(packageManager)); ecosystem != "" {
		return string(ecosystem)
	}
	if packageidentity.LowerASCII(strings.TrimSpace(packageManager)) == "sbt" {
		return string(packageidentity.EcosystemMaven)
	}
	return ""
}

// manifestDependencyFreshness renders the store freshness for the response:
// fresh when the read served an active generation, unavailable otherwise.
func manifestDependencyFreshness(freshness postgres.RepositoryManifestFreshness) ManifestDependencyFreshness {
	rendered := ManifestDependencyFreshness{
		ScopeFound:         freshness.ScopeFound,
		ActiveGenerationID: freshness.ActiveGenerationID,
		GenerationStatus:   freshness.GenerationStatus,
		FreshnessState:     string(querycontract.FreshnessUnavailable),
	}
	if !freshness.GenerationObservedAt.IsZero() {
		rendered.ObservedAt = freshness.GenerationObservedAt.UTC().Format(time.RFC3339Nano)
	}
	if freshness.ScopeFound && freshness.GenerationStatus == "active" {
		rendered.FreshnessState = string(querycontract.FreshnessFresh)
	}
	return rendered
}
