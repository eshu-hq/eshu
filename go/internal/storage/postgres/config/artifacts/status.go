// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

// Read-model states for the /admin/status field config_artifacts (decision
// record section 3.5). PR3b-1a defines the values; the status field itself
// lands with the reader (PR3c).
const (
	// StatusNotInstalled means stored rows are never trusted, with a reason
	// such as ReasonEncodingUnsupported.
	StatusNotInstalled = "not_installed"
	// StatusBackfilling means the first full walk has not recorded its
	// backfill marker yet.
	StatusBackfilling = "backfilling"
	// StatusReady means the backfill marker exists.
	StatusReady = "ready"
)

// Reasons the read model is StatusNotInstalled.
const (
	// ReasonEncodingUnsupported: server_encoding is not UTF8 (re-ruling 3).
	ReasonEncodingUnsupported = "encoding_unsupported"
	// ReasonEncodingCheckFailed: SHOW server_encoding failed at start, so the
	// process cannot vouch for the digest and keeps the read model off.
	ReasonEncodingCheckFailed = "encoding_check_failed"
)

// Trigger names what started a derive. It is the trigger label of the
// derives_total counter and the derive duration histogram.
type Trigger string

const (
	// TriggerWrite is the derive at the end of ContentWriter.Write.
	TriggerWrite Trigger = "write"
	// TriggerBackfill is a walker derive during the first full walk.
	TriggerBackfill Trigger = "backfill"
	// TriggerRepair is a walker derive of a repository the dirty join named.
	TriggerRepair Trigger = "repair"
)

// Outcome is the result of one derive, the outcome label of derives_total.
type Outcome string

const (
	// OutcomeOK: the derive wrote rows and a state row for its version.
	OutcomeOK Outcome = "ok"
	// OutcomeSuperseded: a newer version or extractor already owns the state
	// row, so the derive rolled back without writing.
	OutcomeSuperseded Outcome = "superseded"
	// OutcomeError: the derive failed; the state row records the failure.
	OutcomeError Outcome = "error"
	// OutcomePanic: the derive recovered a panic.
	OutcomePanic Outcome = "panic"
	// OutcomeTimeout: the derive ran past its deadline.
	OutcomeTimeout Outcome = "timeout"
	// OutcomeEncodingUnsupported: the start-up encoding check failed, so no
	// derive runs in this process.
	OutcomeEncodingUnsupported Outcome = "encoding_unsupported"
)
