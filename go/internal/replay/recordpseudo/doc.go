// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package recordpseudo pseudonymizes identifiers while a collector run is
// recorded into a replay cassette (#6965 Phase 3).
//
// A cassette must carry synthetic values only, but a recording of a real
// estate is full of account ids, ARNs, names, hostnames and addresses. Wrap
// puts a collector.Source between the live collector and the recorder: it
// drains the run, learns every identifier from the fields a collector-owned
// Policy classifies, and rewrites scope, envelope and payload strings by
// structure-aware token substitution (ARNs by position, regions protected,
// free text longest-first on alphanumeric boundaries) with keyed,
// structure-preserving pseudonyms. Equal raw tokens under one Key give equal
// pseudonyms, so joins between facts and between sibling cassettes survive by
// construction; the class of a token is deliberately not part of the HMAC
// input, only of the output shape, so two collectors that classify the same
// token differently still agree.
//
// The engine fails closed: a string field whose key the Policy does not
// list is replaced by an opaque pseudonym and its path is reported. Verify
// is the second belt: it scans the canonical output with the private-data
// gate's own alternatives and refuses any candidate that is neither a
// documented safe form nor a pseudonym the run produced, so the recorder
// never writes a file the gate would reject -- and never writes a raw
// account that merely looks like a pseudonym.
//
// Nothing in this package logs, returns or formats a raw value: Report and
// every error carry counts, field paths, offsets and alternatives only.
package recordpseudo
