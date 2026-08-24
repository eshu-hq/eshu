// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package main implements the public documentation CLI and environment reference verifier.
// It applies a precision-first contract to concrete references in conservative
// shell fences and leaves excluded shell forms outside the gate's scope. A
// logical line that is a simple list -- segments separated by an unquoted pipe,
// AND, or semicolon -- is split so each segment is checked against its own
// command; every other shell form keeps the whole line out of scope rather than
// risk attributing one command's flags to another. An unquoted subshell or
// command substitution excludes the line whether or not it is a list, because
// its inner words are not arguments of the outer command. Every run reports how many
// command segments it attributed and how many Eshu command lines it skipped
// that way, and asserts both: the skipped population is pinned exactly in each
// direction and the attributed population has a floor, so a scanner whose
// coverage collapsed cannot report a clean run. If the baseline or frozen
// ceiling is malformed, the verifier fails closed. The mutable baseline may
// shrink only within that frozen initial-debt ceiling.
package main
