// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"time"
)

// SSMInventoryPage is one ListInventoryEntries response for one host and
// inventory type, reduced to the completeness evidence the assessment needs:
// the stream identity (instance, source type, schema version, capture time),
// the pagination chain (the token that fetched this page and the token for
// the next), and the volume accounting (entry count, largest entry, page
// bytes). Raw rows, hostnames, and IPs never enter this shape.
type SSMInventoryPage struct {
	// InstanceID is the managed-node instance identifier this page belongs
	// to. Every page in one assessed stream must agree.
	InstanceID string

	// SourceType is the inventory type this page belongs to (for example
	// AWS:Application). Every page in one assessed stream must agree:
	// platform and application evidence pair by skew rule, never by page
	// mixing.
	SourceType string

	// SchemaVersion is the inventory schema version this page was read
	// under. Every page in one assessed stream must agree.
	SchemaVersion string

	// CaptureTime is the inventory capture time (RFC 3339) this page
	// reports. Every page in one assessed stream must agree; a missing or
	// invalid capture time makes the stream unknown.
	CaptureTime string

	// FetchToken is the pagination token that fetched this page: empty for
	// the first page, otherwise the previous page's NextToken.
	FetchToken string

	// NextToken is the pagination token for the following page: empty on
	// the final page. A non-empty NextToken on the last assessed page
	// means the read stopped early (truncation).
	NextToken string

	// EntryCount is the number of inventory entries on this page.
	EntryCount int

	// MaxEntryBytes is the largest single entry bytes on this page.
	MaxEntryBytes int64

	// PageBytes is the compact-JSON response bytes of this page.
	PageBytes int64
}

// Closed assessment reasons. Reasons are static strings: pagination tokens,
// capture times, and instance identities never flow into operator-visible
// text through the verdict path.
const (
	ssmCaptureReasonComplete        = "complete coherent capture within budgets"
	ssmCaptureReasonTruncated       = "pagination stopped with a pending next token"
	ssmCaptureReasonTokenRepeat     = "repeating pagination token"
	ssmCaptureReasonTokenChain      = "broken pagination token chain"
	ssmCaptureReasonIncoherent      = "incoherent stream identity or capture time"
	ssmCaptureReasonMissingCapture  = "missing or invalid capture time"
	ssmCaptureReasonNoPages         = "no pages read"
	ssmCaptureReasonEntryCeiling    = "entry volume exceeds the per-host capture ceiling"
	ssmCaptureReasonPageCeiling     = "page volume exceeds the per-host per-type poll ceiling"
	ssmCaptureReasonEntryByteBudget = "entry bytes exceed the per-entry planning budget"
	ssmCaptureReasonPageByteBudget  = "page bytes exceed the per-page planning budget"
	ssmCaptureReasonStale           = "capture past the stale-age bound"
	ssmCaptureReasonSourceNotAllowlisted = "source type outside the poll allowlist"
)

// SSMHostCaptureAssessment is the verdict for one host and inventory type:
// whether the read is complete, whether it may supersede the prior
// generation, the operator outcome, and the static reason. Only a fresh
// complete capture within budgets supersedes; every other verdict preserves
// the prior generation with visible status.
type SSMHostCaptureAssessment struct {
	// Complete reports a fully paginated, coherent, within-budget read.
	Complete bool

	// Supersede reports the read may supersede the prior generation for
	// that host and type. False preserves the prior generation.
	Supersede bool

	// Outcome is the operator verdict: successful, partial, or stale.
	Outcome SSMHostInventoryOutcome

	// Reason is a closed static string naming the verdict class.
	Reason string
}

// AssessSSMHostCapture verdicts one host-and-type inventory read against
// the configured limits at read time now (eshu-hq/eshu#9556, rules from the
// accepted #9302 record). A complete snapshot needs every page (valid
// nonrepeating pagination tokens), a consistent instance ID, type, schema
// version, and capture time within the stream, volume within the ceilings,
// and a fresh capture — before superseding prior inventory. Missing pages,
// denial-shaped gaps, inconsistent capture times, incoherent type pairs,
// ceiling breaches, and stale captures mean partial, stale, or unknown, not
// an empty host or a deletion.
func AssessSSMHostCapture(pages []SSMInventoryPage, limits SSMHostInventoryLimits, now time.Time) SSMHostCaptureAssessment {
	preserve := func(outcome SSMHostInventoryOutcome, reason string) SSMHostCaptureAssessment {
		return SSMHostCaptureAssessment{Outcome: outcome, Reason: reason}
	}
	if len(pages) == 0 {
		return preserve(SSMHostInventoryPartial, ssmCaptureReasonNoPages)
	}
	first := pages[0]
	// The poll allowlist is enforced before any coherence or budget verdict:
	// a stream outside it is never admissible, whatever else it carries.
	allowlisted := false
	for _, allowed := range limits.AllowedSourceTypes {
		if first.SourceType == allowed {
			allowlisted = true
			break
		}
	}
	if !allowlisted {
		return preserve(SSMHostInventoryPartial, ssmCaptureReasonSourceNotAllowlisted)
	}
	if first.CaptureTime == "" {
		return preserve(SSMHostInventoryPartial, ssmCaptureReasonMissingCapture)
	}
	captureTime, err := time.Parse(time.RFC3339, first.CaptureTime)
	if err != nil {
		return preserve(SSMHostInventoryPartial, ssmCaptureReasonMissingCapture)
	}
	if first.FetchToken != "" {
		return preserve(SSMHostInventoryPartial, ssmCaptureReasonTokenChain)
	}
	// seenNextTokens holds the non-empty NextTokens emitted so far. A chained
	// token (page N's NextToken, page N+1's FetchToken) appears twice by
	// construction and is expected; a repeated NextToken means the pager
	// re-emitted a page.
	seenNextTokens := make(map[string]struct{}, len(pages))
	totalEntries := 0
	for i, page := range pages {
		if page.InstanceID != first.InstanceID ||
			page.SourceType != first.SourceType ||
			page.SchemaVersion != first.SchemaVersion ||
			page.CaptureTime != first.CaptureTime {
			return preserve(SSMHostInventoryPartial, ssmCaptureReasonIncoherent)
		}
		if i > 0 && page.FetchToken != pages[i-1].NextToken {
			return preserve(SSMHostInventoryPartial, ssmCaptureReasonTokenChain)
		}
		if page.NextToken != "" {
			if _, seen := seenNextTokens[page.NextToken]; seen {
				return preserve(SSMHostInventoryPartial, ssmCaptureReasonTokenRepeat)
			}
			seenNextTokens[page.NextToken] = struct{}{}
		}
		if page.EntryCount < 0 {
			return preserve(SSMHostInventoryPartial, ssmCaptureReasonIncoherent)
		}
		totalEntries += page.EntryCount
		if page.MaxEntryBytes > int64(limits.MaxEntryBytes) {
			return preserve(SSMHostInventoryPartial, ssmCaptureReasonEntryByteBudget)
		}
		if page.PageBytes > int64(limits.MaxPageBytes) {
			return preserve(SSMHostInventoryPartial, ssmCaptureReasonPageByteBudget)
		}
	}
	if last := pages[len(pages)-1]; last.NextToken != "" {
		return preserve(SSMHostInventoryPartial, ssmCaptureReasonTruncated)
	}
	if len(pages) > limits.MaxPagesPerHostPerTypePerPoll {
		return preserve(SSMHostInventoryPartial, ssmCaptureReasonPageCeiling)
	}
	if totalEntries > limits.MaxEntriesPerHostPerCapture {
		return preserve(SSMHostInventoryPartial, ssmCaptureReasonEntryCeiling)
	}
	if limits.CaptureIsStale(captureTime, now) {
		return preserve(SSMHostInventoryStale, ssmCaptureReasonStale)
	}
	return SSMHostCaptureAssessment{
		Complete:  true,
		Supersede: true,
		Outcome:   SSMHostInventorySuccessful,
		Reason:    ssmCaptureReasonComplete,
	}
}
