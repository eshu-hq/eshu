// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packagereg

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/query/querydecode"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/pgarray"
)

// These three kinds are GOVERNED reducer-derived facts per the #4784 ADR
// (docs/internal/design/4784-reducer-derived-fact-governance.md — "W1 governs
// all three package correlation kinds together"). Governance landed in full
// (#5461): sdk/go/factschema/reducerderived/v1/package_correlations.go holds
// the typed structs, the generated JSON Schemas exist under
// sdk/go/factschema/schema/, and go/internal/reducer/package_correlation_writer.go
// is the typed writer. decodePackageRegistryCorrelationRow below decodes each
// kind through the typed factschema seam
// (factschema_decode_package_correlations.go) rather than a raw payload map
// read.
const (
	packageOwnershipCorrelationFactKind   = "reducer_package_ownership_correlation"
	packageConsumptionCorrelationFactKind = "reducer_package_consumption_correlation"
	packagePublicationCorrelationFactKind = "reducer_package_publication_correlation"
)

// PackageRegistryCorrelationStore reads reducer-owned package correlations.
//
// filter.Limit is the caller's requested VISIBLE row count (never pre-add a
// "+1" lookahead the way earlier callers used to): the store fetches one
// extra fact beyond Limit internally so PackageRegistryCorrelationPage's
// Truncated and NextCursorCorrelationID are derived from the raw fetched fact
// count/fact_id sequence, never from len(Rows) (#5816 finding on #5461).
type PackageRegistryCorrelationStore interface {
	ListPackageRegistryCorrelations(
		context.Context,
		PackageRegistryCorrelationFilter,
	) (PackageRegistryCorrelationPage, error)
}

// PackageRegistryCorrelationFilter bounds package correlation reads to one
// package or repository, with optional relationship-kind and cursor filters.
//
// PackageIDs anchors a single bounded read to a set of packages
// (payload package_id IN PackageIDs). It is the batched companion to the
// scalar PackageID anchor and exists so the repo-scoped dependency-chain
// resolver can fetch publication/ownership correlations for every package a
// repository consumes in one round trip instead of one round trip per package.
//
// RelationshipKinds restricts the read to a specific set of relationship kinds
// (payload relationship_kind IN RelationshipKinds) when non-empty. The
// dependency-chain phase-2 read uses this to fetch only publisher kinds
// (publication, ownership) before the LIMIT page, ensuring popular packages
// with many consumer rows do not push publisher rows off the bounded page.
type PackageRegistryCorrelationFilter struct {
	PackageID            string
	PackageIDs           []string
	RepositoryID         string
	RelationshipKind     string
	RelationshipKinds    []string
	AfterCorrelationID   string
	AllowedRepositoryIDs []string
	AllowedScopeIDs      []string
	// Limit is the caller's requested VISIBLE row count. Do not pre-add a "+1"
	// lookahead here: PostgresPackageRegistryCorrelationStore fetches one
	// extra fact internally and derives PackageRegistryCorrelationPage's
	// Truncated/NextCursorCorrelationID from that raw fetch, never from
	// len(Rows) (#5816 finding on #5461).
	Limit int
}

// PackageRegistryCorrelationRow is one durable package ownership, publication,
// or consumption row decoded from reducer fact payloads.
type PackageRegistryCorrelationRow struct {
	CorrelationID          string
	RelationshipKind       string
	PackageID              string
	VersionID              string
	Version                string
	PublishedAt            string
	Ecosystem              string
	PackageName            string
	RepositoryID           string
	RepositoryName         string
	SourceURL              string
	CandidateRepositoryIDs []string
	RelativePath           string
	ManifestSection        string
	DependencyRange        string
	Outcome                string
	Reason                 string
	ProvenanceOnly         bool
	CanonicalWrites        int
	EvidenceFactIDs        []string
}

// PackageRegistryCorrelationQueryer is the minimal Postgres surface the
// correlation read model needs. It is exported so root's compatibility
// constructor can forward any caller-supplied queryer -- a *sql.DB, a *sql.Tx,
// or a wrapper -- rather than narrowing the parameter to *sql.DB (#6060).
type PackageRegistryCorrelationQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// PostgresPackageRegistryCorrelationStore reads reducer package correlation
// facts from Postgres with package/repository scoped filters.
type PostgresPackageRegistryCorrelationStore struct {
	DB PackageRegistryCorrelationQueryer
}

// NewPostgresPackageRegistryCorrelationStore creates the Postgres-backed
// package correlation read model.
func NewPostgresPackageRegistryCorrelationStore(db PackageRegistryCorrelationQueryer) PostgresPackageRegistryCorrelationStore {
	return PostgresPackageRegistryCorrelationStore{DB: db}
}

// ListPackageRegistryCorrelations returns a bounded page of active reducer
// package ownership, publication, or consumption correlation facts.
// filter.Limit is the caller's requested VISIBLE row count; this fetches one
// extra fact beyond it (the "+1" lookahead) so the returned
// PackageRegistryCorrelationPage's Truncated and NextCursorCorrelationID
// reflect the RAW fetched fact count/fact_id sequence rather than how many
// facts in the window survived typed decode (#5816 finding on #5461: a
// malformed or unsupported-version fact inside the window must not make a
// truncated page report itself complete).
func (s PostgresPackageRegistryCorrelationStore) ListPackageRegistryCorrelations(
	ctx context.Context,
	filter PackageRegistryCorrelationFilter,
) (PackageRegistryCorrelationPage, error) {
	if s.DB == nil {
		return PackageRegistryCorrelationPage{}, fmt.Errorf("package registry correlation database is required")
	}
	if filter.PackageID == "" && filter.RepositoryID == "" && len(filter.PackageIDs) == 0 {
		return PackageRegistryCorrelationPage{}, fmt.Errorf("package_id, package_ids, or repository_id is required")
	}
	if filter.Limit <= 0 || filter.Limit > packageRegistryMaxLimit {
		return PackageRegistryCorrelationPage{}, fmt.Errorf("limit must be between 1 and %d", packageRegistryMaxLimit)
	}
	fetchLimit := filter.Limit + 1

	rows, err := s.DB.QueryContext(
		ctx,
		listPackageRegistryCorrelationsQuery,
		packageRegistryCorrelationFactKinds(),
		filter.PackageID,
		filter.RepositoryID,
		filter.RelationshipKind,
		filter.AfterCorrelationID,
		fetchLimit,
		pgarray.Array(filter.AllowedRepositoryIDs),
		pgarray.Array(filter.AllowedScopeIDs),
		pgarray.Array(filter.PackageIDs),
		pgarray.Array(filter.RelationshipKinds),
	)
	if err != nil {
		return PackageRegistryCorrelationPage{}, fmt.Errorf("list package registry correlations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	facts := make([]packageRegistryCorrelationFactRow, 0, fetchLimit)
	for rows.Next() {
		var factID string
		var factKind string
		var schemaVersion string
		var payloadBytes []byte
		if err := rows.Scan(&factID, &factKind, &schemaVersion, &payloadBytes); err != nil {
			return PackageRegistryCorrelationPage{}, fmt.Errorf("list package registry correlations: %w", err)
		}
		facts = append(facts, packageRegistryCorrelationFactRow{
			FactID:        factID,
			FactKind:      factKind,
			SchemaVersion: schemaVersion,
			Payload:       payloadBytes,
		})
	}
	if err := rows.Err(); err != nil {
		return PackageRegistryCorrelationPage{}, fmt.Errorf("list package registry correlations: %w", err)
	}
	page, err := buildPackageRegistryCorrelationPage(facts, fetchLimit)
	if err != nil {
		return PackageRegistryCorrelationPage{}, err
	}
	return page, nil
}

const listPackageRegistryCorrelationsQuery = `
SELECT fact.fact_id, fact.fact_kind, fact.schema_version, fact.payload
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.fact_kind = ANY($1::text[])
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND ($2 = '' OR fact.payload->>'package_id' = $2)
  AND ($3 = '' OR fact.payload->>'repository_id' = $3)
  AND ($4 = '' OR fact.payload->>'relationship_kind' = $4)
  AND ($5 = '' OR fact.fact_id > $5)
  AND (
    COALESCE(cardinality($9::text[]), 0) = 0
    OR fact.payload->>'package_id' = ANY($9::text[])
  )
  AND (
    COALESCE(cardinality($10::text[]), 0) = 0
    OR fact.payload->>'relationship_kind' = ANY($10::text[])
  )
  AND (
    (COALESCE(cardinality($7::text[]), 0) = 0 AND COALESCE(cardinality($8::text[]), 0) = 0)
    OR fact.payload->>'repository_id' = ANY($7::text[])
    OR fact.payload->'candidate_repository_ids' ?| $7::text[]
    OR fact.scope_id = ANY($8::text[])
  )
ORDER BY fact.fact_id ASC
LIMIT $6
`

func packageRegistryCorrelationFactKinds() []string {
	return []string{
		packageOwnershipCorrelationFactKind,
		packageConsumptionCorrelationFactKind,
		packagePublicationCorrelationFactKind,
	}
}

// decodePackageRegistryCorrelationRow decodes one scanned fact row into a
// PackageRegistryCorrelationRow through the typed factschema seam matching
// its fact kind (factschema_decode_package_correlations.go). ok is false and
// err is nil when the fact fails ANY classified decode — a *querydecode.Error,
// logged at debug level — so the caller drops the row rather than emitting a
// wrong-looking row. The motivating case is the fact's required identity
// field (package_id) missing or null, but the same drop path also covers any
// other classified decode failure: sdk/go/factschema/decode_map.go's
// assignField does a strict type assertion per field kind with no coercion
// (for example the reflect.String case rejects any non-string raw value with
// "want string, got %T"), so a plain field-type mismatch on any named field —
// not only package_id — also yields a classified *factschema.DecodeError and
// is dropped the same way. err is non-nil only for a structurally malformed
// payload (invalid JSON) or an unrecognized fact kind, both of which abort
// the whole list call: packageRegistryCorrelationFactKinds bounds the SQL
// read to exactly these three kinds, so an unrecognized kind here signals a
// query/decode drift bug, not a per-fact data-quality issue.
func decodePackageRegistryCorrelationRow(
	factID string,
	factKind string,
	schemaVersion string,
	payloadBytes []byte,
) (PackageRegistryCorrelationRow, bool, error) {
	var payload map[string]any
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return PackageRegistryCorrelationRow{}, false, fmt.Errorf("decode package registry correlation: %w", err)
	}
	in := packageCorrelationDecodeInput{FactID: factID, SchemaVersion: schemaVersion, Payload: payload}

	switch factKind {
	case packageOwnershipCorrelationFactKind:
		correlation, err := decodeReducerPackageOwnershipCorrelation(in)
		if err != nil {
			logPackageRegistryCorrelationDecodeDrop(err)
			return PackageRegistryCorrelationRow{}, false, nil
		}
		return PackageRegistryCorrelationRow{
			CorrelationID:          factID,
			RelationshipKind:       workItemDerefString(correlation.RelationshipKind),
			PackageID:              correlation.PackageID,
			VersionID:              workItemDerefString(correlation.VersionID),
			RepositoryID:           workItemDerefString(correlation.RepositoryID),
			RepositoryName:         workItemDerefString(correlation.RepositoryName),
			SourceURL:              workItemDerefString(correlation.SourceURL),
			CandidateRepositoryIDs: correlation.CandidateRepositoryIDs,
			Outcome:                workItemDerefString(correlation.Outcome),
			Reason:                 workItemDerefString(correlation.Reason),
			ProvenanceOnly:         workItemDerefBool(correlation.ProvenanceOnly),
			CanonicalWrites:        packageCorrelationDerefInt(correlation.CanonicalWrites),
			EvidenceFactIDs:        correlation.EvidenceFactIDs,
		}, true, nil

	case packageConsumptionCorrelationFactKind:
		correlation, err := decodeReducerPackageConsumptionCorrelation(in)
		if err != nil {
			logPackageRegistryCorrelationDecodeDrop(err)
			return PackageRegistryCorrelationRow{}, false, nil
		}
		return PackageRegistryCorrelationRow{
			CorrelationID:    factID,
			RelationshipKind: workItemDerefString(correlation.RelationshipKind),
			PackageID:        correlation.PackageID,
			Ecosystem:        workItemDerefString(correlation.Ecosystem),
			PackageName:      workItemDerefString(correlation.PackageName),
			RepositoryID:     workItemDerefString(correlation.RepositoryID),
			RepositoryName:   workItemDerefString(correlation.RepositoryName),
			RelativePath:     workItemDerefString(correlation.RelativePath),
			ManifestSection:  workItemDerefString(correlation.ManifestSection),
			DependencyRange:  workItemDerefString(correlation.DependencyRange),
			Outcome:          workItemDerefString(correlation.Outcome),
			Reason:           workItemDerefString(correlation.Reason),
			ProvenanceOnly:   workItemDerefBool(correlation.ProvenanceOnly),
			CanonicalWrites:  packageCorrelationDerefInt(correlation.CanonicalWrites),
			EvidenceFactIDs:  correlation.EvidenceFactIDs,
		}, true, nil

	case packagePublicationCorrelationFactKind:
		correlation, err := decodeReducerPackagePublicationCorrelation(in)
		if err != nil {
			logPackageRegistryCorrelationDecodeDrop(err)
			return PackageRegistryCorrelationRow{}, false, nil
		}
		return PackageRegistryCorrelationRow{
			CorrelationID:          factID,
			RelationshipKind:       workItemDerefString(correlation.RelationshipKind),
			PackageID:              correlation.PackageID,
			VersionID:              workItemDerefString(correlation.VersionID),
			Version:                workItemDerefString(correlation.Version),
			PublishedAt:            workItemDerefString(correlation.PublishedAt),
			RepositoryID:           workItemDerefString(correlation.RepositoryID),
			RepositoryName:         workItemDerefString(correlation.RepositoryName),
			SourceURL:              workItemDerefString(correlation.SourceURL),
			CandidateRepositoryIDs: correlation.CandidateRepositoryIDs,
			Outcome:                workItemDerefString(correlation.Outcome),
			Reason:                 workItemDerefString(correlation.Reason),
			ProvenanceOnly:         workItemDerefBool(correlation.ProvenanceOnly),
			CanonicalWrites:        packageCorrelationDerefInt(correlation.CanonicalWrites),
			EvidenceFactIDs:        correlation.EvidenceFactIDs,
		}, true, nil

	default:
		return PackageRegistryCorrelationRow{}, false, fmt.Errorf("decode package registry correlation: unrecognized fact kind %q", factKind)
	}
}

// logPackageRegistryCorrelationDecodeDrop logs one dropped package
// correlation fact at debug level, naming the fact id, fact kind, and missing
// field so an operator can locate the malformed row in fact_records. Mirrors
// work_item_evidence.go's logWorkItemEvidenceDecodeDrop.
func logPackageRegistryCorrelationDecodeDrop(err error) {
	var decodeErr *querydecode.Error
	if !errors.As(err, &decodeErr) {
		slog.Debug("package registry correlation fact dropped from list: decode error", slog.String("error", err.Error()))
		return
	}
	attrs := []any{
		slog.String("fact_id", decodeErr.FactID),
		slog.String("fact_kind", decodeErr.FactKind),
		slog.String("classification", decodeErr.Classification),
	}
	if decodeErr.Field != "" {
		attrs = append(attrs, slog.String("missing_field", decodeErr.Field))
	}
	slog.Debug("package registry correlation fact dropped from list", attrs...)
}
