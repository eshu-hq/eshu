// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package evbundle builds and checks the portable evidence bundles behind
// `eshu evidence bundle export` and `eshu evidence bundle validate`: the
// fixture demo bundle, the live bundle composed from a running stack's three
// status routes, the rendered JSON, and the validation verdict.
//
// The package reads no cobra flags, resolves no Eshu config or credential
// from the process environment, calls no clock, and never calls os.Exit. It
// takes a StatusFetcher, a scope handle, and a caller-owned clock function as
// parameters -- a function rather than an already-read timestamp, so the
// export decides when the clock is read (see ExportLive) --
// and it reads and writes local files only behind explicit path parameters
// (ReadBundleInput, WriteBundle) -- mechanical input and output handling, not
// process wiring. go/cmd/eshu's evidence_bundle_cmd.go is the thin cobra
// wrapper that resolves the flags (--scope, --out, --live, --from, plus the
// shared remote flags), builds the API client, supplies time.Now, and maps
// errors to the CLI's exit-code contract.
//
// Share-safety contract. Nothing here composes a string that lands in a
// bundle field: LiveSnapshotFromStatus copies decoded status values through
// verbatim, and the only caller-supplied string that becomes bundle content
// is the scope handle, which evidencebundle stamps into Identity.ScopeID and
// every reproduce call's repo_id. ExportDemo and ExportLive both run
// evidencebundle.Validate -- a whole-document scan over the marshalled
// bundle, not a field-name-keyed redactor -- before returning any bytes, and
// return no bytes at all when it rejects. What that scan does and does not
// screen is enumerated in README.md; it is a screen, not a guarantee.
package evbundle
