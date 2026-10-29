// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// ssmCaptureTime is the dated capture both measured cohort nodes share.
const ssmCaptureTime = "2026-10-29T22:31:19Z"

// buildSSMCapturePages synthesizes a complete multi-page
// ListInventoryEntries read for one host and type: pageCount pages, entries
// split across them, chained fetch/next tokens, one coherent dated capture.
// It mirrors the measured cohort shape (target A: 1310 entries over 27
// pages) without carrying any real row, hostname, or identifier.
func buildSSMCapturePages(pageCount, entries int) []SSMInventoryPage {
	pages := make([]SSMInventoryPage, 0, pageCount)
	base := entries / pageCount
	extra := entries % pageCount
	fetchToken := ""
	for i := 0; i < pageCount; i++ {
		count := base
		if i < extra {
			count++
		}
		nextToken := ""
		if i+1 < pageCount {
			nextToken = fmt.Sprintf("ssm-test-token-%d", i+1)
		}
		pages = append(pages, SSMInventoryPage{
			InstanceID:    "i-test",
			SourceType:    "AWS:Application",
			SchemaVersion: "1.0.0",
			CaptureTime:   ssmCaptureTime,
			FetchToken:    fetchToken,
			NextToken:     nextToken,
			EntryCount:    count,
			MaxEntryBytes: 459,
			PageBytes:     22800,
		})
		fetchToken = nextToken
	}
	return pages
}

func assessSSMCapture(t *testing.T, pages []SSMInventoryPage) SSMHostCaptureAssessment {
	t.Helper()
	now := time.Date(2026, 10, 29, 22, 35, 0, 0, time.UTC)
	return AssessSSMHostCapture(pages, exampleSSMHostInventoryLimits(), now)
}

// TestAssessSSMHostCaptureAdmitsComplete27PageSnapshot proves the
// no-page-1-truncation regression from #9556: a populated host reads ~27
// pages, so the assessment must require every one of them. All 27 chained
// pages with a coherent dated capture assess complete and may supersede.
func TestAssessSSMHostCaptureAdmitsComplete27PageSnapshot(t *testing.T) {
	t.Parallel()

	assessment := assessSSMCapture(t, buildSSMCapturePages(27, 1310))
	if !assessment.Complete {
		t.Fatalf("27-page coherent capture: Complete = false, reason %q", assessment.Reason)
	}
	if !assessment.Supersede {
		t.Fatalf("27-page coherent capture: Supersede = false, reason %q", assessment.Reason)
	}
	if assessment.Outcome != SSMHostInventorySuccessful {
		t.Fatalf("27-page coherent capture: Outcome = %q, want successful", assessment.Outcome)
	}
}

// TestAssessSSMHostCaptureRejectsPageOneTruncation proves a single-page read
// of a multi-page stream is partial, never superseding: stopping after page
// 1 while its NextToken names more pages preserves the prior generation.
func TestAssessSSMHostCaptureRejectsPageOneTruncation(t *testing.T) {
	t.Parallel()

	pages := buildSSMCapturePages(27, 1310)
	assessment := assessSSMCapture(t, pages[:1])
	if assessment.Complete {
		t.Fatal("page-1-of-27 read: Complete = true, want false")
	}
	if assessment.Supersede {
		t.Fatal("page-1-of-27 read: Supersede = true, want false (partial preserves prior)")
	}
	if assessment.Outcome != SSMHostInventoryPartial {
		t.Fatalf("page-1-of-27 read: Outcome = %q, want partial", assessment.Outcome)
	}
}

// TestAssessSSMHostCaptureCeilingBreachIsPartialNotSuperseding proves a
// capture exceeding any arbiter ceiling reads partial/unknown, never
// truncated-and-superseding: the prior generation stays until a bounded
// complete capture replaces it.
func TestAssessSSMHostCaptureCeilingBreachIsPartialNotSuperseding(t *testing.T) {
	t.Parallel()

	t.Run("page_ceiling", func(t *testing.T) {
		t.Parallel()
		assessment := assessSSMCapture(t, buildSSMCapturePages(101, 5050))
		if assessment.Complete || assessment.Supersede {
			t.Fatalf("101-page capture: Complete = %v Supersede = %v, want both false",
				assessment.Complete, assessment.Supersede)
		}
		if assessment.Outcome != SSMHostInventoryPartial {
			t.Fatalf("101-page capture: Outcome = %q, want partial", assessment.Outcome)
		}
	})

	t.Run("entry_ceiling", func(t *testing.T) {
		t.Parallel()
		pages := buildSSMCapturePages(27, 1310)
		pages[0].EntryCount = SSMHostInventoryMaxEntriesPerHostPerCapture + 1
		assessment := assessSSMCapture(t, pages)
		if assessment.Complete || assessment.Supersede {
			t.Fatalf("over-ceiling entry count: Complete = %v Supersede = %v, want both false",
				assessment.Complete, assessment.Supersede)
		}
	})
}

// TestAssessSSMHostCaptureUnknownOnIncoherence proves the unknown-not-empty
// rule: missing pages, repeating tokens, inconsistent capture times, and
// incoherent type pairs preserve the prior generation instead of reading an
// empty host or a deletion.
func TestAssessSSMHostCaptureUnknownOnIncoherence(t *testing.T) {
	t.Parallel()

	t.Run("no_pages", func(t *testing.T) {
		t.Parallel()
		assessment := assessSSMCapture(t, nil)
		if assessment.Complete || assessment.Supersede {
			t.Fatal("zero pages: superseding verdict, want preserve-prior unknown")
		}
	})

	t.Run("repeating_token", func(t *testing.T) {
		t.Parallel()
		pages := buildSSMCapturePages(3, 120)
		pages[2].NextToken = pages[0].NextToken
		assessment := assessSSMCapture(t, pages)
		if assessment.Complete || assessment.Supersede {
			t.Fatal("repeating pagination token: superseding verdict, want preserve-prior unknown")
		}
		if assessment.Reason != "repeating pagination token" {
			t.Fatalf("repeating pagination token: Reason = %q, want the repeat verdict", assessment.Reason)
		}
	})

	t.Run("capture_time_split", func(t *testing.T) {
		t.Parallel()
		pages := buildSSMCapturePages(3, 120)
		pages[2].CaptureTime = "2026-10-28T22:31:19Z"
		assessment := assessSSMCapture(t, pages)
		if assessment.Complete || assessment.Supersede {
			t.Fatal("split capture times: superseding verdict, want preserve-prior unknown")
		}
	})

	t.Run("missing_capture_time", func(t *testing.T) {
		t.Parallel()
		pages := buildSSMCapturePages(2, 0)
		for i := range pages {
			pages[i].CaptureTime = ""
			pages[i].EntryCount = 0
		}
		assessment := assessSSMCapture(t, pages)
		if assessment.Complete || assessment.Supersede {
			t.Fatal("undated zero-row response: superseding verdict, want preserve-prior unknown")
		}
	})

	t.Run("cross_type_pair", func(t *testing.T) {
		t.Parallel()
		pages := buildSSMCapturePages(2, 80)
		pages[1].SourceType = "AWS:Network"
		assessment := assessSSMCapture(t, pages)
		if assessment.Complete || assessment.Supersede {
			t.Fatal("mixed-type page pair: superseding verdict, want preserve-prior unknown")
		}
	})
}

// TestAssessSSMHostCaptureStalePreservesPrior proves a complete-but-aged
// capture reads stale, never superseding: the display marks staleness and
// the prior generation stands.
func TestAssessSSMHostCaptureStalePreservesPrior(t *testing.T) {
	t.Parallel()

	pages := buildSSMCapturePages(27, 1310)
	for i := range pages {
		pages[i].CaptureTime = "2026-10-20T22:31:19Z"
	}
	assessment := assessSSMCapture(t, pages)
	if assessment.Complete || assessment.Supersede {
		t.Fatal("9-day-old capture: superseding verdict, want stale preserve-prior")
	}
	if assessment.Outcome != SSMHostInventoryStale {
		t.Fatalf("9-day-old capture: Outcome = %q, want stale", assessment.Outcome)
	}
}

// TestAssessSSMHostCaptureReasonCarriesNoIdentifiers proves every assessment
// reason is a closed static string: pagination tokens, capture times, and
// instance identities can never flow into operator-visible text through the
// verdict path.
func TestAssessSSMHostCaptureReasonCarriesNoIdentifiers(t *testing.T) {
	t.Parallel()

	pages := buildSSMCapturePages(3, 120)
	pages[2].FetchToken = "ssm-sensitive-resume-token"
	pages[1].InstanceID = "i-sensitive"
	assessment := assessSSMCapture(t, pages)
	for _, secret := range []string{"ssm-sensitive-resume-token", "i-sensitive", ssmCaptureTime} {
		if strings.Contains(assessment.Reason, secret) {
			t.Fatalf("assessment reason %q leaks %q", assessment.Reason, secret)
		}
	}
	if assessment.Reason == "" {
		t.Fatal("assessment reason is empty, want a static verdict string")
	}
}
