// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
)

// collectorSDKProtocolV1Alpha3 is the paged collector extension wire
// contract. It mirrors collector.ProtocolVersionV1Alpha3 in the
// sdk/go/collector module: the host decodes the page envelope at the JSON
// level (see ResultPage) so this package keeps working against the pinned
// eshu-sdk/collector module, which predates pagination. The string values
// must stay identical; TestPagedDecodeAcceptsLegacyAndPaged pins the mirror
// against the SDK module's own paged fixture.
const collectorSDKProtocolV1Alpha3 = "collector-sdk/v1alpha3"

// DefaultMaxResultPages bounds how many satellite calls one claim may
// reassemble. A hostile 93 MB result needs about 24 pages at the 4 MiB
// stdout bound, so 64 leaves headroom while keeping a runaway satellite from
// holding a claim past its deadline one page at a time. The bound is
// explicit and documented (never a silent fact ceiling): facts that do not
// fit one page move to the next, and breaching the page bound fails the claim
// terminal naming the bound, with the satellite's cursor protocol left intact
// so a narrower page bound resumes cleanly on retry.
const DefaultMaxResultPages = 64

// ResultPage is the host-side mirror of the collector-sdk/v1alpha3 page
// continuation (collector.ResultPage in sdk/go/collector). JSON tags must
// stay field-for-field identical with the SDK type; the cross-module fixture
// test pins them.
type ResultPage struct {
	// Cursor is the opaque satellite-minted resume token, required while
	// More is true and empty on the final page.
	Cursor string `json:"cursor,omitempty"`
	// More reports whether the host must re-invoke the satellite with
	// Cursor to fetch the next page.
	More bool `json:"more,omitempty"`
	// PageIndex is the zero-based page sequence the host verifies gapless.
	PageIndex int `json:"page_index,omitempty"`
	// TotalFacts is the satellite-declared fact total across all pages.
	// Zero means undeclared.
	TotalFacts int `json:"total_facts,omitempty"`
}

// PageContinuation carries the resume cursor back to the satellite. It
// travels only on continuation calls (never on the first call, which stays
// byte-identical to the legacy request), so satellites that never paginate
// never observe it.
type PageContinuation struct {
	// Cursor is the opaque token the satellite minted on the previous page.
	Cursor string `json:"cursor,omitempty"`
}

// CallResult is one decoded satellite call: the SDK result plus the optional
// v1alpha3 page continuation. A nil Page means legacy single-shot output.
type CallResult struct {
	Result sdkcollector.Result
	Page   *ResultPage
}

// PagedRunner is an optional Runner extension for satellites that speak the
// v1alpha3 pagination contract. Runners that do not implement it keep the
// exact legacy single-call path through RunCollector.
type PagedRunner interface {
	Runner
	RunPagedCollector(context.Context, Request) (CallResult, error)
}

// pagedWireResult decodes one stdout document with the page envelope
// promoted alongside the SDK result fields, so strict decoding still rejects
// unknown fields and trailing JSON while admitting both legacy and paged
// output. Numbers stay exact through UseNumber exactly as decodeSDKResult
// guarantees.
type pagedWireResult struct {
	sdkcollector.Result
	Page *ResultPage `json:"page,omitempty"`
}

// decodePagedSDKResult decodes exactly one SDK result with its optional page
// envelope from either bounded runner stdout buffer.
func decodePagedSDKResult(payload []byte) (CallResult, error) {
	var wire pagedWireResult
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	if err := decoder.Decode(&wire); err != nil {
		return CallResult{}, fmt.Errorf("decode extension result: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return CallResult{}, errors.New("decode extension result: trailing JSON value")
		}
		return CallResult{}, fmt.Errorf("decode extension result trailer: %w", err)
	}
	return CallResult{Result: wire.Result, Page: wire.Page}, nil
}

// collectPages reassembles one claim's pages by re-invoking a paged runner
// until a final page closes the generation. It returns the assembled result
// (all pages' facts and statuses, first page's generation) for the existing
// validate/record/resolve path, so downstream stages observe exactly one
// result. Every error is already an extensionFailure with its final class:
// runner failures stay launch failures, claim or generation drift is an
// identity mismatch, and sequence, version, total, or bound violations are
// invalid results. All are terminal except the launch failure, matching the
// legacy single-call mapping.
func (s *Source) collectPages(ctx context.Context, runner PagedRunner, request Request) (sdkcollector.Result, error) {
	maxPages := s.maxResultPages()
	var facts []sdkcollector.Fact
	var statuses []sdkcollector.Status
	var assembled sdkcollector.Result
	var pinnedGeneration sdkcollector.Generation
	pinned := false
	declaredTotal := 0
	pending := request
	for index := 0; ; index++ {
		if index >= maxPages {
			return sdkcollector.Result{}, extensionFailure{
				class:    FailureClassInvalidResult,
				terminal: true,
				cause: fmt.Errorf(
					"extension result exceeded %d pages for one claim",
					maxPages,
				),
			}
		}
		call, err := runner.RunPagedCollector(ctx, pending)
		if err != nil {
			// Matches the legacy single-call mapping: every runner error,
			// including a spent claim deadline surfacing through the
			// runner, is a launch failure.
			return sdkcollector.Result{}, extensionFailure{
				class: FailureClassLaunchFailure,
				cause: err,
			}
		}
		if err := s.checkPageEnvelope(call, index, index > 0); err != nil {
			return sdkcollector.Result{}, err
		}
		if err := validateReturnedClaim(pending.Claim, call.Result.Claim); err != nil {
			return sdkcollector.Result{}, extensionFailure{
				class:    FailureClassIdentityMismatch,
				terminal: true,
				cause:    err,
			}
		}
		if _, err := s.validator.ValidateResult(call.Result); err != nil {
			return sdkcollector.Result{}, extensionFailure{
				class:    FailureClassInvalidResult,
				terminal: true,
				cause:    err,
			}
		}
		if !pinned {
			pinnedGeneration = call.Result.Generation
			declaredTotal = pageTotal(call.Page)
			pinned = true
		} else if !sameGeneration(pinnedGeneration, call.Result.Generation) {
			return sdkcollector.Result{}, extensionFailure{
				class:    FailureClassIdentityMismatch,
				terminal: true,
				cause:    errors.New("extension result generation changed mid-stream"),
			}
		}
		if declaredTotal != pageTotal(call.Page) {
			return sdkcollector.Result{}, extensionFailure{
				class:    FailureClassInvalidResult,
				terminal: true,
				cause:    errors.New("extension result total_facts changed mid-stream"),
			}
		}
		facts = append(facts, call.Result.Facts...)
		statuses = append(statuses, call.Result.Statuses...)
		assembled = call.Result
		if call.Page == nil || !call.Page.More {
			break
		}
		pending.Continuation = &PageContinuation{Cursor: call.Page.Cursor}
	}
	assembled.Facts = facts
	assembled.Statuses = statuses
	if declaredTotal > 0 && len(facts) != declaredTotal {
		return sdkcollector.Result{}, extensionFailure{
			class:    FailureClassInvalidResult,
			terminal: true,
			cause: fmt.Errorf(
				"extension result reassembled %d facts, want declared total %d",
				len(facts), declaredTotal,
			),
		}
	}
	assembled.Generation = pinnedGeneration
	return assembled, nil
}

// checkPageEnvelope enforces the stream framing before any content
// validation: pages must be versioned v1alpha3, indexed gaplessly from zero,
// and present on every call once a continuation opens the stream.
func (s *Source) checkPageEnvelope(call CallResult, index int, streamOpen bool) error {
	invalid := func(cause error) error {
		return extensionFailure{
			class:    FailureClassInvalidResult,
			terminal: true,
			cause:    cause,
		}
	}
	if call.Page == nil {
		if streamOpen {
			return invalid(errors.New("extension result dropped its page envelope mid-stream"))
		}
		return nil
	}
	if call.Result.ProtocolVersion != collectorSDKProtocolV1Alpha3 {
		return invalid(fmt.Errorf(
			"extension result page requires protocol_version %q",
			collectorSDKProtocolV1Alpha3,
		))
	}
	if call.Page.PageIndex != index {
		return invalid(fmt.Errorf(
			"extension result page_index %d breaks the sequence at page %d",
			call.Page.PageIndex, index,
		))
	}
	// The pinned eshu-sdk/collector validator cannot see the page envelope,
	// so the SDK module's page rules are mirrored here: a continuation page
	// must name its resume cursor and carry a completable state, a final
	// page must not carry a cursor, and the declared total cannot be
	// negative.
	if call.Page.TotalFacts < 0 {
		return invalid(errors.New("extension result total_facts must be >= 0"))
	}
	if call.Page.More {
		if strings.TrimSpace(call.Page.Cursor) == "" {
			return invalid(errors.New("extension result continuation is missing its cursor"))
		}
		switch call.Result.State {
		case sdkcollector.ResultComplete, sdkcollector.ResultPartial:
		default:
			return invalid(fmt.Errorf(
				"%s extension result must not request continuation",
				call.Result.State,
			))
		}
		return nil
	}
	if strings.TrimSpace(call.Page.Cursor) != "" {
		return invalid(errors.New("extension result final page must not carry a cursor"))
	}
	return nil
}

// pageTotal reads the declared cross-page fact total; a nil page is the
// legacy single-shot result and declares nothing.
func pageTotal(page *ResultPage) int {
	if page == nil {
		return 0
	}
	return page.TotalFacts
}

// sameGeneration compares the generation fields a satellite repeats on every
// page. The per-page SDK validator already pins the generation ID to the
// claim; this pins the rest, so a page cannot smuggle a different
// observation time, freshness hint, or deletion attestation into an otherwise
// identical generation.
func sameGeneration(first, next sdkcollector.Generation) bool {
	return first.ID == next.ID &&
		first.ObservedAt.Equal(next.ObservedAt) &&
		strings.TrimSpace(first.FreshnessHint) == strings.TrimSpace(next.FreshnessHint) &&
		first.TrustedDeletion == next.TrustedDeletion
}

func (s *Source) maxResultPages() int {
	if s.maxPages > 0 {
		return s.maxPages
	}
	return DefaultMaxResultPages
}
