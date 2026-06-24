// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package answernarration validates optional narrated answer text against an
// existing query answer packet.
//
// The package is a pure guard layer for governed answer narration. It does not
// build prompts, call providers, read graph or content stores, or change the
// canonical ResponseEnvelope or AnswerPacket. Callers pass a source packet, an
// explicit citation allowlist, and candidate narrated sentences; Validate
// returns low-cardinality findings that future status, audit, and metrics paths
// can reuse.
package answernarration
