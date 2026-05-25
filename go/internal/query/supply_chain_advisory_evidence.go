package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/lib/pq"
)

const (
	advisoryEvidenceCapability       = "supply_chain.advisory_evidence.list"
	advisoryEvidenceMaxLimit         = 200
	advisoryEvidenceMaxFactRows      = 5000
	advisoryEvidenceFreshnessCurrent = "active"
)

// AdvisoryEvidenceStore reads source-only vulnerability advisory evidence.
type AdvisoryEvidenceStore interface {
	ListAdvisoryEvidence(context.Context, AdvisoryEvidenceFilter) ([]AdvisoryEvidenceRow, error)
}

// AdvisoryEvidenceFilter bounds source-evidence reads to an advisory, CVE, or
// package anchor. Source narrows an already anchored read.
type AdvisoryEvidenceFilter struct {
	CVEID            string
	AdvisoryID       string
	PackageID        string
	Source           string
	AfterAdvisoryKey string
	Limit            int
}

// AdvisoryEvidenceRow is one canonical advisory identity with source-specific
// evidence attached. It is source evidence only and does not imply repository,
// image, workload, or package impact.
type AdvisoryEvidenceRow struct {
	AdvisoryKey         string                       `json:"advisory_key"`
	CanonicalID         string                       `json:"canonical_id"`
	CVEIDs              []string                     `json:"cve_ids,omitempty"`
	GHSAIDs             []string                     `json:"ghsa_ids,omitempty"`
	OSVIDs              []string                     `json:"osv_ids,omitempty"`
	SourceIDs           []string                     `json:"source_ids,omitempty"`
	Sources             []AdvisorySourceEvidence     `json:"sources,omitempty"`
	AffectedPackages    []AdvisoryAffectedPackage    `json:"affected_packages,omitempty"`
	AffectedProducts    []AdvisoryAffectedProduct    `json:"affected_products,omitempty"`
	EPSS                []AdvisoryEPSSObservation    `json:"epss,omitempty"`
	KEV                 []AdvisoryKEVObservation     `json:"kev,omitempty"`
	References          []AdvisoryReferenceEvidence  `json:"references,omitempty"`
	SourceDisagreements []AdvisorySourceDisagreement `json:"source_disagreements,omitempty"`
	EvidenceFactIDs     []string                     `json:"evidence_fact_ids,omitempty"`
	LatestObservedAt    string                       `json:"latest_observed_at,omitempty"`
	SourceFreshness     string                       `json:"source_freshness,omitempty"`
	SourceConfidence    string                       `json:"source_confidence,omitempty"`
}

// AdvisorySourceEvidence preserves one source-reported advisory identity,
// severity, weakness, and withdrawal observation.
type AdvisorySourceEvidence struct {
	Source        string              `json:"source"`
	AdvisoryID    string              `json:"advisory_id,omitempty"`
	CVEID         string              `json:"cve_id,omitempty"`
	GHSAID        string              `json:"ghsa_id,omitempty"`
	Aliases       []string            `json:"aliases,omitempty"`
	PublishedAt   string              `json:"published_at,omitempty"`
	ModifiedAt    string              `json:"modified_at,omitempty"`
	WithdrawnAt   string              `json:"withdrawn_at,omitempty"`
	SeverityLabel string              `json:"severity_label,omitempty"`
	CVSSScore     float64             `json:"cvss_score,omitempty"`
	CVSSVector    string              `json:"cvss_vector,omitempty"`
	CVSSVectorV2  string              `json:"cvss_v2,omitempty"`
	CVSSVectorV3  string              `json:"cvss_v3,omitempty"`
	CVSSVectorV4  string              `json:"cvss_v4,omitempty"`
	CVSSMetrics   map[string]any      `json:"cvss_metrics,omitempty"`
	Severity      []map[string]string `json:"severity,omitempty"`
	CWEs          []string            `json:"cwes,omitempty"`
	SourceFactIDs []string            `json:"source_fact_ids,omitempty"`
}

// AdvisoryAffectedPackage preserves package-native affected range and fixed
// version evidence from OSV, GHSA, GLAD, or vendor package advisories.
type AdvisoryAffectedPackage struct {
	Source              string           `json:"source"`
	AdvisoryID          string           `json:"advisory_id,omitempty"`
	CVEID               string           `json:"cve_id,omitempty"`
	GHSAID              string           `json:"ghsa_id,omitempty"`
	Ecosystem           string           `json:"ecosystem,omitempty"`
	PackageID           string           `json:"package_id,omitempty"`
	PURL                string           `json:"purl,omitempty"`
	AffectedRange       string           `json:"affected_range,omitempty"`
	ParsedAffectedRange map[string]any   `json:"parsed_affected_range,omitempty"`
	AffectedRanges      []map[string]any `json:"affected_ranges,omitempty"`
	AffectedVersions    []string         `json:"affected_versions,omitempty"`
	FixedVersions       []string         `json:"fixed_versions,omitempty"`
	SourceFactID        string           `json:"source_fact_id,omitempty"`
}

// AdvisoryAffectedProduct preserves NVD product/CPE applicability evidence.
type AdvisoryAffectedProduct struct {
	Source                      string `json:"source"`
	CVEID                       string `json:"cve_id,omitempty"`
	Criteria                    string `json:"criteria,omitempty"`
	MatchCriteriaID             string `json:"match_criteria_id,omitempty"`
	Vulnerable                  bool   `json:"vulnerable"`
	VersionStartIncluding       string `json:"version_start_including,omitempty"`
	VersionStartExcluding       string `json:"version_start_excluding,omitempty"`
	VersionEndIncluding         string `json:"version_end_including,omitempty"`
	VersionEndExcluding         string `json:"version_end_excluding,omitempty"`
	SourceConfigurationOperator string `json:"source_configuration_operator,omitempty"`
	SourceConfigurationNegate   bool   `json:"source_configuration_negate,omitempty"`
	SourceNodeOperator          string `json:"source_node_operator,omitempty"`
	SourceNodeNegate            bool   `json:"source_node_negate,omitempty"`
	SourceFactID                string `json:"source_fact_id,omitempty"`
}

// AdvisoryEPSSObservation preserves one FIRST EPSS score observation.
type AdvisoryEPSSObservation struct {
	Source      string `json:"source"`
	CVEID       string `json:"cve_id,omitempty"`
	Probability string `json:"probability,omitempty"`
	Percentile  string `json:"percentile,omitempty"`
	ScoreDate   string `json:"score_date,omitempty"`
	FactID      string `json:"fact_id,omitempty"`
}

// AdvisoryKEVObservation preserves one CISA KEV known-exploited observation.
type AdvisoryKEVObservation struct {
	Source                     string   `json:"source"`
	CVEID                      string   `json:"cve_id,omitempty"`
	DateAdded                  string   `json:"date_added,omitempty"`
	RequiredAction             string   `json:"required_action,omitempty"`
	DueDate                    string   `json:"due_date,omitempty"`
	KnownRansomwareCampaignUse string   `json:"known_ransomware_campaign_use,omitempty"`
	CWEs                       []string `json:"cwes,omitempty"`
	FactID                     string   `json:"fact_id,omitempty"`
}

// AdvisoryReferenceEvidence preserves one sanitized source reference URL.
type AdvisoryReferenceEvidence struct {
	Source        string `json:"source"`
	AdvisoryID    string `json:"advisory_id,omitempty"`
	CVEID         string `json:"cve_id,omitempty"`
	ReferenceType string `json:"reference_type,omitempty"`
	URL           string `json:"url,omitempty"`
	FactID        string `json:"fact_id,omitempty"`
}

// AdvisorySourceDisagreement records a source-level disagreement without
// selecting a winner.
type AdvisorySourceDisagreement struct {
	Field  string                      `json:"field"`
	Values []AdvisoryDisagreementValue `json:"values"`
}

// AdvisoryDisagreementValue is one source/value pair inside a disagreement.
type AdvisoryDisagreementValue struct {
	Source string `json:"source"`
	Value  string `json:"value"`
}

type advisoryEvidenceQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

type advisoryEvidenceFactRow struct {
	FactID           string
	FactKind         string
	SourceConfidence string
	ObservedAt       string
	Payload          map[string]any
}

// PostgresAdvisoryEvidenceStore reads active vulnerability source facts and
// groups them into canonical advisory evidence rows.
type PostgresAdvisoryEvidenceStore struct {
	DB advisoryEvidenceQueryer
}

// NewPostgresAdvisoryEvidenceStore creates the Postgres-backed advisory
// evidence read model.
func NewPostgresAdvisoryEvidenceStore(db advisoryEvidenceQueryer) PostgresAdvisoryEvidenceStore {
	return PostgresAdvisoryEvidenceStore{DB: db}
}

// ListAdvisoryEvidence returns one bounded page of source-only advisory
// evidence.
func (s PostgresAdvisoryEvidenceStore) ListAdvisoryEvidence(
	ctx context.Context,
	filter AdvisoryEvidenceFilter,
) ([]AdvisoryEvidenceRow, error) {
	filter = normalizeAdvisoryEvidenceFilter(filter)
	if s.DB == nil {
		return nil, fmt.Errorf("advisory evidence database is required")
	}
	if !filter.hasScope() {
		return nil, fmt.Errorf("cve_id, advisory_id, or package_id is required")
	}
	if filter.Limit <= 0 || filter.Limit > advisoryEvidenceMaxLimit+1 {
		return nil, fmt.Errorf("limit must be between 1 and %d for internal pagination", advisoryEvidenceMaxLimit+1)
	}
	rows, err := s.DB.QueryContext(
		ctx,
		listAdvisoryEvidenceQuery,
		pq.Array(advisoryEvidenceFactKinds),
		filter.CVEID,
		filter.AdvisoryID,
		filter.PackageID,
		filter.Source,
		advisoryEvidenceMaxFactRows,
	)
	if err != nil {
		return nil, fmt.Errorf("list advisory evidence: %w", err)
	}
	defer func() { _ = rows.Close() }()

	facts := make([]advisoryEvidenceFactRow, 0, advisoryEvidenceFactCapacity())
	for rows.Next() {
		var factID string
		var factKind string
		var sourceConfidence string
		var observedAt sql.NullTime
		var payloadBytes []byte
		if err := rows.Scan(&factID, &factKind, &sourceConfidence, &observedAt, &payloadBytes); err != nil {
			return nil, fmt.Errorf("list advisory evidence: %w", err)
		}
		var payload map[string]any
		if err := json.Unmarshal(payloadBytes, &payload); err != nil {
			return nil, fmt.Errorf("decode advisory evidence payload: %w", err)
		}
		facts = append(facts, advisoryEvidenceFactRow{
			FactID:           factID,
			FactKind:         factKind,
			SourceConfidence: sourceConfidence,
			ObservedAt:       formatNullTime(observedAt),
			Payload:          payload,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list advisory evidence: %w", err)
	}
	return pageAdvisoryEvidenceRows(buildAdvisoryEvidenceRows(facts), filter), nil
}

func (f AdvisoryEvidenceFilter) hasScope() bool {
	return f.CVEID != "" || f.AdvisoryID != "" || f.PackageID != ""
}

func normalizeAdvisoryEvidenceFilter(filter AdvisoryEvidenceFilter) AdvisoryEvidenceFilter {
	filter.CVEID = normalizeAdvisoryLookupID(filter.CVEID)
	filter.AdvisoryID = normalizeAdvisoryLookupID(filter.AdvisoryID)
	filter.PackageID = strings.TrimSpace(filter.PackageID)
	filter.Source = strings.ToLower(strings.TrimSpace(filter.Source))
	filter.AfterAdvisoryKey = normalizeAdvisoryLookupID(filter.AfterAdvisoryKey)
	return filter
}

func normalizeAdvisoryLookupID(value string) string {
	return normalizeAdvisoryDisplayID(strings.TrimSpace(value))
}

func advisoryEvidenceFactCapacity() int {
	return advisoryEvidenceMaxFactRows
}

func formatNullTime(value sql.NullTime) string {
	if !value.Valid {
		return ""
	}
	return value.Time.UTC().Format(time.RFC3339)
}

func pageAdvisoryEvidenceRows(rows []AdvisoryEvidenceRow, filter AdvisoryEvidenceFilter) []AdvisoryEvidenceRow {
	start := 0
	if after := normalizeAdvisoryLookupID(filter.AfterAdvisoryKey); after != "" {
		for idx, row := range rows {
			if advisoryEvidenceKeyEqual(row.AdvisoryKey, after) {
				start = idx + 1
				break
			}
		}
	}
	if start >= len(rows) {
		return nil
	}
	end := start + filter.Limit
	if end > len(rows) {
		end = len(rows)
	}
	return append([]AdvisoryEvidenceRow(nil), rows[start:end]...)
}

func advisoryEvidenceKeyEqual(left string, right string) bool {
	return strings.EqualFold(normalizeAdvisoryLookupID(left), normalizeAdvisoryLookupID(right))
}

func (h *SupplyChainHandler) listAdvisoryEvidence(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryAdvisoryEvidence,
		"GET /api/v0/supply-chain/advisories/evidence",
		advisoryEvidenceCapability,
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), advisoryEvidenceCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"advisory evidence requires the Postgres vulnerability source fact read model",
			ErrorCodeUnsupportedCapability,
			advisoryEvidenceCapability,
			h.profile(),
			requiredProfile(advisoryEvidenceCapability),
		)
		return
	}
	limit, ok := requiredAdvisoryEvidenceLimit(w, r)
	if !ok {
		return
	}
	filter := normalizeAdvisoryEvidenceFilter(AdvisoryEvidenceFilter{
		CVEID:            QueryParam(r, "cve_id"),
		AdvisoryID:       QueryParam(r, "advisory_id"),
		PackageID:        QueryParam(r, "package_id"),
		Source:           QueryParam(r, "source"),
		AfterAdvisoryKey: QueryParam(r, "after_advisory_key"),
		Limit:            limit + 1,
	})
	if !filter.hasScope() {
		WriteError(w, http.StatusBadRequest, "cve_id, advisory_id, or package_id is required")
		return
	}
	if h.AdvisoryEvidence == nil {
		WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"advisory evidence requires the Postgres vulnerability source fact read model",
			ErrorCodeBackendUnavailable,
			advisoryEvidenceCapability,
			h.profile(),
			requiredProfile(advisoryEvidenceCapability),
		)
		return
	}
	rows, err := h.AdvisoryEvidence.ListAdvisoryEvidence(r.Context(), filter)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	body := map[string]any{
		"advisories": rows,
		"count":      len(rows),
		"limit":      limit,
		"truncated":  truncated,
	}
	if truncated && len(rows) > 0 {
		body["next_cursor"] = map[string]string{"after_advisory_key": rows[len(rows)-1].AdvisoryKey}
	}
	WriteSuccess(w, r, http.StatusOK, body, BuildTruthEnvelope(
		h.profile(),
		advisoryEvidenceCapability,
		TruthBasisSemanticFacts,
		"resolved from active vulnerability source facts; advisory evidence remains source-only and does not imply package, repository, image, workload, or deployment impact",
	))
}

func requiredAdvisoryEvidenceLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := QueryParam(r, "limit")
	if raw == "" {
		WriteError(w, http.StatusBadRequest, "limit is required")
		return 0, false
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 || limit > advisoryEvidenceMaxLimit {
		WriteError(w, http.StatusBadRequest, fmt.Sprintf("limit must be between 1 and %d", advisoryEvidenceMaxLimit))
		return 0, false
	}
	return limit, true
}

var advisoryEvidenceFactKinds = []string{
	"vulnerability.cve",
	"vulnerability.affected_package",
	"vulnerability.affected_product",
	"vulnerability.epss_score",
	"vulnerability.known_exploited",
	"vulnerability.reference",
}

const listAdvisoryEvidenceQuery = `
WITH active AS (
    SELECT fact.fact_id, fact.fact_kind, fact.source_confidence, fact.observed_at, fact.payload
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
),
seed AS (
    SELECT *
    FROM active
    WHERE ($2 = ''
           OR UPPER(payload->>'cve_id') = UPPER($2)
           OR EXISTS (
               SELECT 1
               FROM jsonb_array_elements_text(CASE WHEN jsonb_typeof(payload->'aliases') = 'array' THEN payload->'aliases' ELSE '[]'::jsonb END) AS alias(value)
               WHERE UPPER(alias.value) = UPPER($2)
           )
           OR EXISTS (
               SELECT 1
               FROM jsonb_array_elements_text(CASE WHEN jsonb_typeof(payload->'correlation_anchors') = 'array' THEN payload->'correlation_anchors' ELSE '[]'::jsonb END) AS anchor(value)
               WHERE UPPER(anchor.value) = UPPER($2)
           ))
      AND ($3 = ''
           OR UPPER(payload->>'advisory_id') = UPPER($3)
           OR UPPER(payload->>'cve_id') = UPPER($3)
           OR UPPER(payload->>'ghsa_id') = UPPER($3)
           OR EXISTS (
               SELECT 1
               FROM jsonb_array_elements_text(CASE WHEN jsonb_typeof(payload->'aliases') = 'array' THEN payload->'aliases' ELSE '[]'::jsonb END) AS alias(value)
               WHERE UPPER(alias.value) = UPPER($3)
           )
           OR EXISTS (
               SELECT 1
               FROM jsonb_array_elements_text(CASE WHEN jsonb_typeof(payload->'correlation_anchors') = 'array' THEN payload->'correlation_anchors' ELSE '[]'::jsonb END) AS anchor(value)
               WHERE UPPER(anchor.value) = UPPER($3)
           ))
      AND ($4 = '' OR payload->>'package_id' = $4 OR payload->>'purl' = $4)
),
seed_keys AS (
    SELECT DISTINCT key_value
    FROM (
        SELECT payload->>'cve_id' AS key_value FROM seed
        UNION ALL SELECT payload->>'advisory_id' FROM seed
        UNION ALL SELECT payload->>'ghsa_id' FROM seed
        UNION ALL SELECT jsonb_array_elements_text(CASE WHEN jsonb_typeof(payload->'aliases') = 'array' THEN payload->'aliases' ELSE '[]'::jsonb END) FROM seed
        UNION ALL SELECT jsonb_array_elements_text(CASE WHEN jsonb_typeof(payload->'correlation_anchors') = 'array' THEN payload->'correlation_anchors' ELSE '[]'::jsonb END) FROM seed
    ) AS raw_keys
    WHERE NULLIF(TRIM(key_value), '') IS NOT NULL
)
SELECT fact_id, fact_kind, source_confidence, observed_at, payload
FROM active
WHERE ($5 = '' OR LOWER(payload->>'source') = LOWER($5))
  AND EXISTS (
      SELECT 1
      FROM seed_keys
      WHERE UPPER(payload->>'cve_id') = UPPER(key_value)
         OR UPPER(payload->>'advisory_id') = UPPER(key_value)
         OR UPPER(payload->>'ghsa_id') = UPPER(key_value)
         OR EXISTS (
             SELECT 1
             FROM jsonb_array_elements_text(CASE WHEN jsonb_typeof(payload->'aliases') = 'array' THEN payload->'aliases' ELSE '[]'::jsonb END) AS alias(value)
             WHERE UPPER(alias.value) = UPPER(key_value)
         )
         OR EXISTS (
             SELECT 1
             FROM jsonb_array_elements_text(CASE WHEN jsonb_typeof(payload->'correlation_anchors') = 'array' THEN payload->'correlation_anchors' ELSE '[]'::jsonb END) AS anchor(value)
             WHERE UPPER(anchor.value) = UPPER(key_value)
         )
         OR payload->>'package_id' = $4
  )
ORDER BY COALESCE(NULLIF(payload->>'cve_id', ''), NULLIF(payload->>'advisory_id', ''), NULLIF(payload->>'ghsa_id', ''), fact_id), fact_kind, fact_id
LIMIT $6
`
