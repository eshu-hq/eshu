// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package recorder records a live collector run as a canonical replay cassette.
//
// Run drives any collector.Source for one batch, captures every emitted fact
// envelope, and writes the batch as a canonical cassette (sorted keys, volatile
// fields collapsed, generation_id derived, configured secrets redacted) that
// replays credential-free through replay/cassette. It performs no durable
// commit, so recording needs only the collector's live credentials, not a
// database.
//
// Because the real collector produces the envelopes, every derived field — most
// importantly each fact's object_id, computed by the collector's own
// facts.StableID derivation — is captured with full fidelity. That is the
// structural fix for cassette object_id drift (#3928): a hand-authored cassette
// could carry a stand-in object_id that the real collector never emits, but a
// recorded cassette cannot, because the recorder copies exactly what the
// collector produced.
//
// Recording is deterministic: re-recording the same input yields byte-identical
// output, so refreshing a fixture produces a reviewable diff instead of churn.
package recorder
