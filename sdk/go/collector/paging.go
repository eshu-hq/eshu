// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"encoding/json"
	"fmt"
	"strings"
)

// pageEnvelopeHeadroomBytes reserves wire space for everything around one
// page's facts on a bounded stdout write: the repeated claim and generation,
// the page envelope, statuses, and JSON framing. It is deliberately generous:
// measured claim+generation+status framing stays under 4 KiB, so a page sized
// by PageBoundForStdout always fits the host's stdout limit with room to
// spare, and the same headroom applies on every protocol version.
const pageEnvelopeHeadroomBytes = int64(64 * 1024)

// PageBoundForStdout derives the explicit per-call page byte bound from the
// host's stdout limit: the limit minus fixed envelope headroom. A satellite
// sizes every page with SplitFacts against this bound, so the bound is
// explicit and versioned (part of the collector-sdk/v1alpha3 pagination
// contract), and every page stays resumable through its cursor. It never
// reduces a fact ceiling silently: facts that do not fit one page move to
// the next, and only a single fact larger than the whole bound fails, naming
// itself.
func PageBoundForStdout(stdoutLimitBytes int64) (int64, error) {
	if stdoutLimitBytes <= 0 {
		return 0, fmt.Errorf("stdout limit must be positive")
	}
	bound := stdoutLimitBytes - pageEnvelopeHeadroomBytes
	if bound <= 0 {
		return 0, fmt.Errorf("stdout limit %d leaves no page bytes after %d headroom", stdoutLimitBytes, pageEnvelopeHeadroomBytes)
	}
	return bound, nil
}

// FactWireBytes returns the exact JSON wire size of one SDK fact: the bytes
// a page carries for it. Satellites use it (through SplitFacts) to account
// every fact against the per-call bound before writing stdout.
func FactWireBytes(fact Fact) (int, error) {
	raw, err := json.Marshal(fact)
	if err != nil {
		return 0, fmt.Errorf("marshal fact %q: %w", fact.Kind, err)
	}
	return len(raw), nil
}

// validateResultPage enforces the versioned pagination contract: page fields
// exist only on ProtocolVersionV1Alpha3, a continuation page must name its
// resume cursor, a final page must not carry one, and only a completable
// state (complete or partial) may ask for continuation. Cross-page rules —
// sequence continuity, generation and claim stability, total-fact agreement —
// need every page at once and are enforced by the reassembling host.
func (v Validator) validateResultPage(result Result) error {
	page := result.Page
	if page == nil {
		return nil
	}
	if v.protocolVersion != ProtocolVersionV1Alpha3 {
		return fmt.Errorf("page requires protocol_version %q", ProtocolVersionV1Alpha3)
	}
	if page.PageIndex < 0 {
		return fmt.Errorf("page.page_index must be >= 0")
	}
	if page.TotalFacts < 0 {
		return fmt.Errorf("page.total_facts must be >= 0")
	}
	if page.More {
		if strings.TrimSpace(page.Cursor) == "" {
			return fmt.Errorf("page.cursor is required for continuation")
		}
		switch result.State {
		case ResultComplete, ResultPartial:
		default:
			return fmt.Errorf("%s result must not request continuation", result.State)
		}
		return nil
	}
	if strings.TrimSpace(page.Cursor) != "" {
		return fmt.Errorf("page.cursor must be empty on the final page")
	}
	return nil
}

// SplitFacts partitions facts into pages whose summed wire bytes each fit
// maxPageBytes. Order is preserved, no fact is lost or duplicated, and every
// page carries at least one fact. A single fact larger than the whole bound
// fails closed naming its kind, stable key, and size: callers must fix the
// producer, never silently drop the fact. An empty input or a non-positive
// bound fails closed as well.
func SplitFacts(facts []Fact, maxPageBytes int64) ([][]Fact, error) {
	if len(facts) == 0 {
		return nil, fmt.Errorf("at least one fact is required to page")
	}
	if maxPageBytes <= 0 {
		return nil, fmt.Errorf("page bound must be positive")
	}
	pages := make([][]Fact, 0, 1)
	current := make([]Fact, 0, len(facts))
	var currentBytes int64
	flush := func() {
		if len(current) > 0 {
			pages = append(pages, current)
			current = make([]Fact, 0, len(facts))
			currentBytes = 0
		}
	}
	for _, fact := range facts {
		size, err := FactWireBytes(fact)
		if err != nil {
			return nil, err
		}
		if int64(size) > maxPageBytes {
			return nil, fmt.Errorf(
				"fact %q stable key %q is %d bytes above page bound %d",
				fact.Kind, fact.StableKey, size, maxPageBytes,
			)
		}
		if len(current) > 0 && currentBytes+int64(size) > maxPageBytes {
			flush()
		}
		current = append(current, fact)
		currentBytes += int64(size)
	}
	flush()
	return pages, nil
}
