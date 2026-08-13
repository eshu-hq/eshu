// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package hookpreflight classifies an `eshu assistant hook preflight`
// request into the assistant_fast_path_hook.v1 advise-or-skip decision (see
// docs/public/reference/assistant-fast-path-hooks.md for the contract).
//
// Evaluate fails open on every ineligible condition -- expired budget,
// unsupported host, disabled hook, disallowed trigger, denied permission,
// missing or unsafe scope, unavailable index freshness -- and never returns
// an error: a fast-path hook that cannot safely advise must let the
// original host action continue, not block it. It advises only once a
// single narrow, share-safe scope resolves from the request's repo path,
// entity ID, service, workload, environment, or resource fields.
//
// The package also merges Claude Code's PreToolUse hook payload
// (ClaudePreToolUseInput) into a request and converts an advise decision
// into that hook's response JSON (ClaudePreToolUseOutputForPreflight),
// since the CLI's first shipped host integration is Claude Code-style.
//
// The package reads no cobra flags, no process environment, and prints
// nothing -- go/cmd/eshu's assistant_hook_preflight.go is the thin cobra
// wrapper that resolves process state (flags, stdin) and calls in as plain
// values, per the extraction shape settled for issue #6053/#6059.
package hookpreflight
