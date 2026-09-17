// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strings"
	"time"

	sdk "github.com/eshu-hq/eshu/sdk/go/collector"
)

const (
	// ComponentID is the placeholder manifest identity. Rename it to the new
	// repository's identity (dev.<owner>.<source>) and keep it in sync with
	// manifest.yaml before the first release.
	ComponentID = "dev.eshu.template.collector"
	// CollectorKind is the placeholder collector family. Rename it once.
	CollectorKind = "template"
	// SourceSystem names the observed source. Rename it once.
	SourceSystem = "dev.eshu.template.source"
	// MetricsPrefix is the component-owned metric prefix from the manifest.
	MetricsPrefix = "eshu_dp_template_collector_"
)

const (
	// FactKindSnapshot summarizes one source report as source evidence.
	FactKindSnapshot = "dev.eshu.template.collector.snapshot"
	// FactKindRecord records one source record as source evidence.
	FactKindRecord = "dev.eshu.template.collector.record"
	// FactKindWarning records degraded-evidence warnings.
	FactKindWarning = "dev.eshu.template.collector.warning"
)

// Report is the placeholder source document this template reads. Replace it
// with the real source artifact; keep the redaction-safe shape (no secrets in
// struct fields that land in payloads).
type Report struct {
	GeneratedAt time.Time `json:"generated_at"`
	Source      string    `json:"source"`
	Records     []Record  `json:"records"`
}

// Record is one placeholder source row.
type Record struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Detail string `json:"detail,omitempty"`
	// Secret is intentionally never emitted: Collect redacts it and records
	// the redaction instead. Keep a field like this so the redaction path
	// stays exercised after the template is customized.
	Secret string `json:"secret,omitempty"`
}

// CollectOptions controls emission for one claimed scope.
type CollectOptions struct {
	ObservedAt     time.Time
	SourceURI      string
	PreviousDigest string
	// Limits bounds one claim's emission. The zero value selects
	// DefaultResourceUse so an unconfigured copy still fails terminal
	// instead of allocating without bound.
	Limits ResourceUse
}

// Contract returns the SDK fact families this template may emit.
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
				Kind:             FactKindRecord,
				SchemaVersions:   []string{"1.0.0"},
				SourceConfidence: []sdk.SourceConfidence{sdk.SourceConfidenceObserved},
			},
			{
				Kind:             FactKindWarning,
				SchemaVersions:   []string{"1.0.0"},
				SourceConfidence: []sdk.SourceConfidence{sdk.SourceConfidenceReported},
			},
		},
	}
}

// maxReportBytes caps one source document. Reads past the cap fail instead
// of decoding unbounded input; emission bounds below still gate every fact.
const maxReportBytes = 32 << 20

// LoadReport decodes one placeholder source document.
func LoadReport(r io.Reader) (Report, error) {
	raw, err := io.ReadAll(io.LimitReader(r, maxReportBytes+1))
	if err != nil {
		return Report{}, fmt.Errorf("read report: %w", err)
	}
	if len(raw) > maxReportBytes {
		return Report{}, fmt.Errorf("source document exceeds %d bytes", maxReportBytes)
	}
	var report Report
	if err := json.Unmarshal(raw, &report); err != nil {
		return Report{}, fmt.Errorf("decode report: %w", err)
	}
	return report, nil
}

// Digest returns the stable identity of one report for freshness/stale proof.
// It covers every field that changes emitted facts: the source (snapshot
// payload), each record's emitted fields, and each record's redaction
// presence, so a stale digest never preserves stale evidence.
func Digest(report Report) string {
	ids := make([]string, 0, len(report.Records)+1)
	ids = append(ids, "source\x00"+report.Source)
	for _, record := range report.Records {
		secretPresence := "secret:0"
		if record.Secret != "" {
			secretPresence = "secret:1"
		}
		ids = append(ids, record.ID+"\x00"+record.Name+"\x00"+record.Detail+"\x00"+secretPresence)
	}
	sort.Strings(ids)
	sum := sha256.Sum256([]byte(strings.Join(ids, "\x01")))
	return hex.EncodeToString(sum[:])
}

// Collect builds one SDK result for the claimed scope and generation.
// Behavior contract, covered by conformance_test.go:
//   - empty input emits an authoritative complete snapshot with zero records
//     (retires stale evidence; distinct from unchanged freshness);
//   - unchanged digest emits ResultUnchanged (stale/freshness proof);
//   - duplicate record IDs emit one fact (idempotent re-emission);
//   - secrets never enter payloads; each redaction is recorded;
//   - source_ref carries claim scope, generation, and a credential-free URI;
//   - record, payload, and deadline bounds fail terminal, never unbounded.
func Collect(claim sdk.Claim, report Report, opts CollectOptions) (sdk.Result, error) {
	observedAt := opts.ObservedAt.UTC()
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}
	limits := opts.Limits
	if limits.MaxRecordsPerClaim <= 0 || limits.MaxPayloadBytes <= 0 || limits.ClaimTimeoutSeconds <= 0 {
		limits = DefaultResourceUse()
	}
	if !claim.Deadline.IsZero() && observedAt.After(claim.Deadline) {
		return terminalResult(claim, observedAt, "claim-deadline-exceeded"), nil
	}
	if err := sdk.ValidateShareSafeKeys(map[string]any{"source": report.Source}); err != nil {
		return sdk.Result{}, err
	}
	if strings.TrimSpace(opts.SourceURI) == "" {
		return sdk.Result{}, fmt.Errorf("source URI must not be blank")
	}
	if err := checkSourceURI(opts.SourceURI); err != nil {
		return sdk.Result{}, err
	}
	if len(report.Records) > limits.MaxRecordsPerClaim {
		return terminalResult(claim, observedAt, "record-budget-exceeded"), nil
	}
	// PreviousDigest accepts the last emitted snapshot stable key (the
	// LastDigest the Monitor reports) or the raw report digest; either
	// match means the source has not moved since the last emission.
	digest := Digest(report)
	if opts.PreviousDigest != "" && len(report.Records) > 0 &&
		(opts.PreviousDigest == digest || opts.PreviousDigest == "snapshot:"+digest) {
		return sdk.Result{
			ProtocolVersion: sdk.ProtocolVersionV1Alpha1,
			State:           sdk.ResultUnchanged,
			Claim:           claim,
			Generation:      sdk.Generation{ID: claim.GenerationID, ObservedAt: observedAt, FreshnessHint: "unchanged-digest"},
			Statuses:        []sdk.Status{{Class: sdk.StatusComplete, FactCount: 0}},
		}, nil
	}

	seen := map[string]bool{}
	facts := make([]sdk.Fact, 0, len(report.Records)+1)
	for _, record := range report.Records {
		key := "record:" + record.ID
		if seen[key] {
			continue
		}
		seen[key] = true
		payload := map[string]any{
			"id":   record.ID,
			"name": record.Name,
		}
		if record.Detail != "" {
			payload["detail"] = record.Detail
		}
		factRedactions := []sdk.Redaction{}
		if record.Secret != "" {
			factRedactions = append(factRedactions, sdk.Redaction{Field: "secret", Reason: "credential-redacted"})
		}
		encoded, err := json.Marshal(payload)
		if err != nil {
			return sdk.Result{}, fmt.Errorf("encode record payload: %w", err)
		}
		if len(encoded) > limits.MaxPayloadBytes {
			return terminalResult(claim, observedAt, "payload-budget-exceeded"), nil
		}
		facts = append(facts, sdk.Fact{
			Kind:             FactKindRecord,
			SchemaVersion:    "1.0.0",
			StableKey:        key,
			SourceConfidence: sdk.SourceConfidenceObserved,
			ObservedAt:       observedAt,
			SourceRef: sdk.SourceRef{
				SourceSystem: SourceSystem,
				ScopeID:      claim.Scope.ID,
				GenerationID: claim.GenerationID,
				FactKey:      key,
				URI:          opts.SourceURI + "#" + record.ID,
				RecordID:     record.ID,
			},
			Payload:    payload,
			Redactions: factRedactions,
		})
	}
	snapshotPayload := map[string]any{"source": report.Source, "record_count": len(seen)}
	if encoded, err := json.Marshal(snapshotPayload); err != nil || len(encoded) > limits.MaxPayloadBytes {
		return terminalResult(claim, observedAt, "payload-budget-exceeded"), nil
	}
	snapshotKey := "snapshot:" + Digest(report)
	facts = append(facts, sdk.Fact{
		Kind:             FactKindSnapshot,
		SchemaVersion:    "1.0.0",
		StableKey:        snapshotKey,
		SourceConfidence: sdk.SourceConfidenceReported,
		ObservedAt:       observedAt,
		SourceRef: sdk.SourceRef{
			SourceSystem: SourceSystem,
			ScopeID:      claim.Scope.ID,
			GenerationID: claim.GenerationID,
			FactKey:      snapshotKey,
			URI:          opts.SourceURI,
			RecordID:     "snapshot",
		},
		Payload: snapshotPayload,
	})
	return sdk.Result{
		ProtocolVersion: sdk.ProtocolVersionV1Alpha1,
		State:           sdk.ResultComplete,
		Claim:           claim,
		Generation:      sdk.Generation{ID: claim.GenerationID, ObservedAt: observedAt},
		Facts:           facts,
		Statuses:        []sdk.Status{{Class: sdk.StatusComplete, FactCount: len(facts)}},
	}, nil
}

// CollectPartial is the partial-failure shape: reachable records commit as
// ResultPartial with a warning status, never as an ambiguous complete.
func CollectPartial(claim sdk.Claim, report Report, opts CollectOptions, failure string) (sdk.Result, error) {
	result, err := Collect(claim, report, opts)
	if err != nil {
		return sdk.Result{}, err
	}
	if result.State != sdk.ResultComplete {
		return result, nil
	}
	// The cause travels as a FactKindWarning fact so operators see why the
	// claim is partial. The reason is share-safe validated first; an
	// unshareable cause is withheld and the redaction recorded instead.
	reason := strings.TrimSpace(failure)
	if reason == "" {
		reason = "source-degraded"
	}
	warningPayload := map[string]any{"reason": reason}
	warningRedactions := []sdk.Redaction{}
	// ValidateShareSafeKeys guards key shape; the reason travels as a value,
	// so credential-marker text is scanned separately. Either trip withholds
	// the cause and records the redaction instead of leaking it.
	if err := sdk.ValidateShareSafeKeys(map[string]any{"reason": reason}); err != nil || containsCredentialText(reason) {
		warningPayload = map[string]any{"reason": "withheld-unshareable-cause"}
		warningRedactions = append(warningRedactions, sdk.Redaction{Field: "reason", Reason: "unshareable-cause-redacted"})
	}
	observedAt := result.Generation.ObservedAt
	warningKey := "warning:partial:" + claim.GenerationID
	result.Facts = append(result.Facts, sdk.Fact{
		Kind:             FactKindWarning,
		SchemaVersion:    "1.0.0",
		StableKey:        warningKey,
		SourceConfidence: sdk.SourceConfidenceReported,
		ObservedAt:       observedAt,
		SourceRef: sdk.SourceRef{
			SourceSystem: SourceSystem,
			ScopeID:      claim.Scope.ID,
			GenerationID: claim.GenerationID,
			FactKey:      warningKey,
			URI:          opts.SourceURI + "#partial",
			RecordID:     "partial",
		},
		Payload:    warningPayload,
		Redactions: warningRedactions,
	})
	result.State = sdk.ResultPartial
	result.Statuses = append(result.Statuses, sdk.Status{Class: sdk.StatusWarning, Partial: true, WarningCount: 1, FactCount: len(result.Facts)})
	return result, nil
}

// terminalResult is the bounded-failure shape for exhausted budgets and
// expired claims: explicit terminal state with a failure class, never an
// ambiguous complete and never unbounded allocation.
func terminalResult(claim sdk.Claim, observedAt time.Time, failureClass string) sdk.Result {
	return sdk.Result{
		ProtocolVersion: sdk.ProtocolVersionV1Alpha1,
		State:           sdk.ResultTerminal,
		Claim:           claim,
		Generation:      sdk.Generation{ID: claim.GenerationID, ObservedAt: observedAt},
		Statuses: []sdk.Status{{
			Class: sdk.StatusFailure, FailureClass: failureClass,
		}},
	}
}

// CollectRetryable is the bounded-retry shape for transient source errors.
func CollectRetryable(claim sdk.Claim, observedAt time.Time, retryAfterSeconds int) sdk.Result {
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}
	return sdk.Result{
		ProtocolVersion: sdk.ProtocolVersionV1Alpha1,
		State:           sdk.ResultRetryable,
		Claim:           claim,
		Generation:      sdk.Generation{ID: claim.GenerationID, ObservedAt: observedAt.UTC()},
		Statuses: []sdk.Status{{
			Class: sdk.StatusFailure, FailureClass: "transient", RetryAfterSeconds: retryAfterSeconds,
		}},
	}
}

func checkSourceURI(raw string) error {
	trimmed := strings.TrimSpace(raw)
	lower := strings.ToLower(trimmed)
	// url.Parse only surfaces userinfo after "//", so check "@" first: a
	// hierarchical URI with userinfo (https://user:secret@host) and an
	// opaque credential body (svc:SECRET@host/x) are both refused. Error
	// text never echoes the URI, which may carry the credential.
	if strings.Contains(trimmed, "@") {
		if u, err := url.Parse(trimmed); err != nil || u.User != nil {
			return fmt.Errorf("source URI must not embed credentials")
		}
		if !strings.Contains(lower, "://") {
			return fmt.Errorf("source URI must not embed credentials")
		}
	}
	for _, key := range []string{"password", "secret", "token"} {
		if strings.Contains(lower, key+"=") {
			return fmt.Errorf("source URI must not embed credentials")
		}
	}
	return nil
}

// credentialValueMarkers are substrings that mark free text as unsafe to
// emit. Matching is deliberately broad: a withheld reason stays diagnosable
// through its redaction record, while a leaked credential does not.
var credentialValueMarkers = []string{
	"password", "passwd", "secret", "token", "bearer",
	"api_key", "apikey", "private_key", "client_secret", "credentials",
}

// containsCredentialText reports whether free text may carry a credential.
func containsCredentialText(text string) bool {
	lower := strings.ToLower(text)
	for _, marker := range credentialValueMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
