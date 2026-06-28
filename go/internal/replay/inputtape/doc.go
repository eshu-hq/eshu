// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package inputtape records and replays HTTP traffic at the http.RoundTripper
// boundary so HTTP-backed collectors can be exercised credential-free and with
// no network access.
//
// It is a replay flavor of the deterministic-replay framework (see
// go/internal/replay): where the cassette flavor records collector output (fact
// envelopes), the input tape records collector input (the raw HTTP responses a
// collector receives). Recording at the transport boundary means the collector
// code under test runs unchanged; only its HTTP client is swapped.
//
// # Modes
//
// A RoundTripper runs in one of two modes:
//
//   - ModeRecord proxies each request to a real transport, returns the live
//     response to the caller, and persists a redacted, canonical request->response
//     pair. Construct with New.
//   - ModeReplay serves responses from a loaded tape keyed by the request, and
//     treats any unmatched request as a hard error. It performs no network I/O.
//     Construct with NewReplayer. An unmatched request is load-bearing: replay
//     never falls through to the network, so a collector that drifts off the
//     recorded request set fails loudly (ErrUnmatchedRequest) rather than
//     silently issuing a live call.
//
// # Fault-injection tape (R-11)
//
// An Interaction may carry an optional Fault directive that turns a plain
// recorded tape into a scenario: a scripted sequence of transport faults
// (timeout, connection reset), body faults (truncated partial response), status
// overrides (4xx/5xx injection), or retry-then-succeed sequences. Faults are
// part of the tape, not runtime configuration, so the same tape produces the
// same fault sequence on every replay run — deterministic by construction, no
// wall-clock reads and no random elements.
//
// See the Fault type, FaultKind constants, ErrFaultTimeout, and ErrFaultReset
// for the full API. The R-11 acceptance gate (TestFaultRetryThenSucceed,
// TestFaultRoundTripDeterminism) verifies that a 5xx-then-200 sequence yields
// byte-identical output across independent replays and that a collector retry
// loop emits each fact exactly once.
//
// # Request matching
//
// Each request is reduced to a deterministic key: a SHA-256 over the method, URL
// path, the sorted query, and (when present) the canonicalized request body. The
// key does not depend on header order, query order, or — for JSON bodies — object
// key order, so a collector that builds an equivalent request resolves to the
// same recorded interaction across runs. Two configurable parameter classes
// adjust the key:
//
//   - Secret parameters (Authorization and friends, token-style query params)
//     are redacted before the key is computed, so a credential-free replay
//     request still matches and the recorded tape never carries a live secret.
//   - Volatile query parameters (per-run timestamps, nonces) are normalized to a
//     fixed sentinel in the key, so a replay request signed/stamped at a
//     different instant still matches. The same set must be supplied at record
//     and replay time.
//
// # Tape format
//
// A tape is a versioned JSON document (schema_version "1") holding an ordered
// list of interactions. The recorder writes it canonically (sorted object keys,
// interactions sorted by request_key, redacted secrets) via the shared
// replay.Canonicalize core, so a committed tape is stable, reviewable in diffs,
// and byte-identical when re-recorded from equivalent traffic. See format.go for
// the Tape, Interaction, RecordedRequest, and RecordedResponse types.
//
// # Wiring
//
// The RoundTripper satisfies http.RoundTripper, so it installs as the Transport
// of an *http.Client. Collectors that accept an HTTP client through a config
// seam (loki, prometheusmimir, tempo, grafana via HTTPClientConfig.Client;
// pagerduty/jira/confluence via *http.Client) wire it directly. SDK-based
// collectors (AWS/GCP/Azure) accept a custom *http.Client on their SDK config
// (for AWS, aws.Config.HTTPClient), so the same RoundTripper records and replays
// through an SDK client seam too.
package inputtape
