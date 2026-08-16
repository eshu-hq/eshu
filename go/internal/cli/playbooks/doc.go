// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package playbooks holds the logic behind the two `eshu playbooks`
// subcommands: list and resolve. RunList fetches GET /api/v0/query-playbooks
// and RunResolve posts to /api/v0/query-playbooks/resolve; both decode the
// canonical Eshu response envelope through an EnvelopeClient the caller
// supplies and print it to an io.Writer as two-space-indented JSON with HTML
// escaping off. An envelope-level error is printed in-band as part of that
// JSON, not mapped to an exit code — reporting a capability failure is part of
// the commands' output contract.
//
// ParseInputs converts the repeatable --input key=value flag values into the
// resolve request's input map, rejecting entries with no separator or an
// empty key so an operator's input is never silently dropped. RunResolve
// trims the playbook ID before the request.
//
// Both Run functions fail on a nil EnvelopeClient instead of succeeding
// silently or panicking, so a wrapper that forgets to wire the client cannot
// look like a healthy command that printed nothing.
//
// The package reads no cobra flags, no process environment, and no command
// line, and never calls os.Exit or touches os.Stdout. go/cmd/eshu's
// playbooks.go is the thin cobra wrapper that resolves the flags, the output
// stream, and the concrete API client. The split is mechanical: go/cmd/eshu
// is package main, so nothing can import it, and any symbol that reads a flag
// or builds the client has to stay there.
package playbooks
