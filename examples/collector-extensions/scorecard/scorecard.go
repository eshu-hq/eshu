// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scorecard

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"

	sdk "github.com/eshu-hq/eshu/sdk/go/collector"
)

const (
	// ComponentID is the manifest identity used for local allowlist policy.
	ComponentID = "dev.eshu.examples.scorecard"
	// CollectorKind is the collector family declared by the component manifest.
	CollectorKind = "scorecard"
	// SourceSystem identifies Scorecard JSON as the observed source system.
	SourceSystem = "openssf-scorecard"
	// MetricsPrefix is the component-owned metric prefix declared by the manifest.
	MetricsPrefix = "eshu_dp_example_scorecard_"
)

const (
	// FactKindSnapshot summarizes one Scorecard report as source evidence.
	FactKindSnapshot = "dev.eshu.examples.scorecard.snapshot"
	// FactKindCheck records one Scorecard check as source evidence.
	FactKindCheck = "dev.eshu.examples.scorecard.check"
	// FactKindWarning records reference-package warnings about degraded evidence.
	FactKindWarning = "dev.eshu.examples.scorecard.warning"
)

// Report is the subset of OpenSSF Scorecard JSON this reference package reads.
type Report struct {
	GeneratedAt time.Time  `json:"generated_at"`
	Repository  Repository `json:"repository"`
	Score       float64    `json:"score"`
	Checks      []Check    `json:"checks"`
}

// Repository identifies the scored repository without credentials.
type Repository struct {
	Name   string `json:"name"`
	Commit string `json:"commit"`
}

// Check describes one Scorecard check result.
type Check struct {
	Name    string   `json:"name"`
	Score   float64  `json:"score"`
	Reason  string   `json:"reason"`
	Details []string `json:"details,omitempty"`
}

// CollectOptions controls reference package emission for one claimed scope.
type CollectOptions struct {
	ObservedAt       time.Time
	SourceURI        string
	PreviousDigest   string
	WarningThreshold float64
}

// Contract returns the SDK fact families accepted for this reference package.
func Contract() sdk.Contract {
	return sdk.Contract{
		ProtocolVersion: sdk.ProtocolVersionV1Alpha1,
		Facts: []sdk.FactDeclaration{
			{
				Kind:             FactKindSnapshot,
				SchemaVersions:   []string{"1.0.0"},
				SourceConfidence: []sdk.SourceConfidence{sdk.SourceConfidenceReported},
			},
			{
				Kind:             FactKindCheck,
				SchemaVersions:   []string{"1.0.0"},
				SourceConfidence: []sdk.SourceConfidence{sdk.SourceConfidenceReported},
			},
			{
				Kind:             FactKindWarning,
				SchemaVersions:   []string{"1.0.0"},
				SourceConfidence: []sdk.SourceConfidence{sdk.SourceConfidenceReported},
			},
		},
	}
}

// LoadReport decodes Scorecard JSON from r.
func LoadReport(r io.Reader) (Report, error) {
	var report Report
	decoder := json.NewDecoder(r)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&report); err != nil {
		return Report{}, fmt.Errorf("decode scorecard report: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return Report{}, fmt.Errorf("decode scorecard report: trailing JSON value")
		}
		return Report{}, fmt.Errorf("decode scorecard report trailer: %w", err)
	}
	if strings.TrimSpace(report.Repository.Name) == "" {
		return Report{}, fmt.Errorf("repository.name is required")
	}
	if report.GeneratedAt.IsZero() {
		return Report{}, fmt.Errorf("generated_at is required")
	}
	return report, nil
}

// Digest returns a stable digest for the normalized report payload.
func Digest(report Report) (string, error) {
	raw, err := json.Marshal(report)
	if err != nil {
		return "", fmt.Errorf("marshal scorecard report: %w", err)
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// Collect converts a report into an SDK result for the supplied core-owned claim.
func Collect(claim sdk.Claim, report Report, options CollectOptions) (sdk.Result, error) {
	if err := validateClaimForPackage(claim); err != nil {
		return sdk.Result{}, err
	}
	if report.Repository.Name != claim.Scope.ID {
		return sdk.Result{}, fmt.Errorf("report repository %q does not match claim scope %q", report.Repository.Name, claim.Scope.ID)
	}
	observedAt := options.ObservedAt
	if observedAt.IsZero() {
		observedAt = report.GeneratedAt
	}
	sourceURI := strings.TrimSpace(options.SourceURI)
	if sourceURI == "" {
		sourceURI = "scorecard://" + claim.Scope.ID
	}
	warningThreshold := options.WarningThreshold
	if warningThreshold == 0 {
		warningThreshold = 5
	}
	digest, err := Digest(report)
	if err != nil {
		return sdk.Result{}, err
	}

	result := baseResult(claim, observedAt, "scorecard_digest="+digest)
	if options.PreviousDigest == digest {
		result.State = sdk.ResultUnchanged
		result.Statuses = []sdk.Status{{Class: sdk.StatusComplete, FactCount: 0}}
		return result, nil
	}

	facts := []sdk.Fact{snapshotFact(claim, report, observedAt, sourceURI, digest)}
	checks, warnings, conflictWarnings := normalizedChecks(report.Checks, warningThreshold)
	for _, check := range checks {
		facts = append(facts, checkFact(claim, check, observedAt, sourceURI))
	}
	for _, warning := range warnings {
		facts = append(facts, warningFact(claim, warning, observedAt, sourceURI))
	}
	for _, warning := range conflictWarnings {
		facts = append(facts, warningFact(claim, warning, observedAt, sourceURI))
	}

	result.Facts = facts
	result.State = sdk.ResultComplete
	result.Statuses = []sdk.Status{{Class: sdk.StatusComplete, FactCount: len(facts)}}
	if len(checks) == 0 || len(conflictWarnings) > 0 {
		result.State = sdk.ResultPartial
		result.Statuses = []sdk.Status{{
			Class:        sdk.StatusWarning,
			Partial:      true,
			WarningCount: len(warnings) + len(conflictWarnings),
			FactCount:    len(facts),
		}}
	}
	return result, nil
}

// RetryableSourceFailureResult creates a retryable result for source failures.
func RetryableSourceFailureResult(claim sdk.Claim, observedAt time.Time, failureClass string, retryAfterSeconds int) sdk.Result {
	failureClass = strings.TrimSpace(failureClass)
	if failureClass == "" {
		failureClass = "source_unavailable"
	}
	if retryAfterSeconds <= 0 {
		retryAfterSeconds = 60
	}
	result := baseResult(claim, observedAt, "source_unavailable")
	result.State = sdk.ResultRetryable
	result.Statuses = []sdk.Status{{
		Class:             sdk.StatusFailure,
		FailureClass:      failureClass,
		RetryAfterSeconds: retryAfterSeconds,
	}}
	return result
}

func validateClaimForPackage(claim sdk.Claim) error {
	if claim.ComponentID != ComponentID {
		return fmt.Errorf("claim component_id %q does not match %q", claim.ComponentID, ComponentID)
	}
	if claim.CollectorKind != CollectorKind {
		return fmt.Errorf("claim collector_kind %q does not match %q", claim.CollectorKind, CollectorKind)
	}
	if claim.SourceSystem != SourceSystem {
		return fmt.Errorf("claim source_system %q does not match %q", claim.SourceSystem, SourceSystem)
	}
	return nil
}

func baseResult(claim sdk.Claim, observedAt time.Time, freshnessHint string) sdk.Result {
	return sdk.Result{
		ProtocolVersion: sdk.ProtocolVersionV1Alpha1,
		Claim:           claim,
		Generation: sdk.Generation{
			ID:            claim.GenerationID,
			ObservedAt:    observedAt,
			FreshnessHint: freshnessHint,
		},
	}
}

func snapshotFact(claim sdk.Claim, report Report, observedAt time.Time, sourceURI string, digest string) sdk.Fact {
	key := "snapshot:" + claim.Scope.ID
	return sdk.Fact{
		Kind:             FactKindSnapshot,
		SchemaVersion:    "1.0.0",
		StableKey:        key,
		SourceConfidence: sdk.SourceConfidenceReported,
		ObservedAt:       observedAt,
		SourceRef:        sourceRef(claim, key, sourceURI, "snapshot"),
		Payload: map[string]any{
			"repository":          report.Repository.Name,
			"commit":              report.Repository.Commit,
			"score":               report.Score,
			"check_count":         len(report.Checks),
			"report_generated_at": report.GeneratedAt.Format(time.RFC3339),
			"report_digest":       digest,
		},
	}
}

func checkFact(claim sdk.Claim, check Check, observedAt time.Time, sourceURI string) sdk.Fact {
	slug := slugify(check.Name)
	key := "check:" + slug
	return sdk.Fact{
		Kind:             FactKindCheck,
		SchemaVersion:    "1.0.0",
		StableKey:        key,
		SourceConfidence: sdk.SourceConfidenceReported,
		ObservedAt:       observedAt,
		SourceRef:        sourceRef(claim, key, sourceURI, "check:"+slug),
		Payload: map[string]any{
			"name":    check.Name,
			"score":   check.Score,
			"reason":  check.Reason,
			"details": check.Details,
		},
	}
}

func warningFact(claim sdk.Claim, warning warningRecord, observedAt time.Time, sourceURI string) sdk.Fact {
	key := "warning:" + warning.Slug
	return sdk.Fact{
		Kind:             FactKindWarning,
		SchemaVersion:    "1.0.0",
		StableKey:        key,
		SourceConfidence: sdk.SourceConfidenceReported,
		ObservedAt:       observedAt,
		SourceRef:        sourceRef(claim, key, sourceURI, "warning:"+warning.Slug),
		Payload: map[string]any{
			"check":   warning.CheckName,
			"class":   warning.Class,
			"message": warning.Message,
		},
	}
}

func sourceRef(claim sdk.Claim, key string, sourceURI string, recordID string) sdk.SourceRef {
	return sdk.SourceRef{
		SourceSystem: claim.SourceSystem,
		ScopeID:      claim.Scope.ID,
		GenerationID: claim.GenerationID,
		FactKey:      key,
		URI:          sourceURI,
		RecordID:     recordID,
	}
}

type warningRecord struct {
	Slug      string
	CheckName string
	Class     string
	Message   string
}

func normalizedChecks(checks []Check, warningThreshold float64) ([]Check, []warningRecord, []warningRecord) {
	bySlug := map[string]Check{}
	conflicts := []warningRecord{}
	for _, check := range checks {
		slug := slugify(check.Name)
		if slug == "" {
			slug = "unnamed"
		}
		if existing, ok := bySlug[slug]; ok {
			if !reflectCheck(existing, check) {
				conflicts = append(conflicts, warningRecord{
					Slug:      "duplicate-" + slug,
					CheckName: check.Name,
					Class:     "duplicate_check_conflict",
					Message:   "scorecard source repeated a check name with different values",
				})
			}
			continue
		}
		bySlug[slug] = check
	}

	slugs := make([]string, 0, len(bySlug))
	for slug := range bySlug {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)
	normalized := make([]Check, 0, len(slugs))
	warnings := []warningRecord{}
	for _, slug := range slugs {
		check := bySlug[slug]
		normalized = append(normalized, check)
		if check.Score < warningThreshold {
			warnings = append(warnings, warningRecord{
				Slug:      slug,
				CheckName: check.Name,
				Class:     "low_score",
				Message:   "scorecard check is below the reference warning threshold",
			})
		}
	}
	slices.SortFunc(warnings, func(left, right warningRecord) int {
		return strings.Compare(left.Slug, right.Slug)
	})
	return normalized, warnings, conflicts
}

func reflectCheck(left Check, right Check) bool {
	return left.Name == right.Name &&
		left.Score == right.Score &&
		left.Reason == right.Reason &&
		slices.Equal(left.Details, right.Details)
}

func slugify(value string) string {
	var builder strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			builder.WriteRune(r)
			lastDash = false
		default:
			if !lastDash && builder.Len() > 0 {
				builder.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(builder.String(), "-")
}
