// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package loki collects bounded live Loki metadata as observability source
// facts.
//
// The package reads configured Loki API targets and emits metadata-only
// observability.source_instance, observability.observed_log_signal,
// observability.observed_rule, and observability.coverage_warning facts. It
// never persists log lines, raw LogQL, private URLs, label values, tenant IDs,
// tenant secrets, credentials, or provider response bodies. Provider failures
// expose bounded workflow failure classes while omitting credential-bearing
// request details. Reducers and query surfaces own declared/applied/observed
// comparison and user-facing observability truth.
package loki
