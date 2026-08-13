// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package opdigest builds the operator_digest.v1 model and its shareable
// operator_digest_artifact.v1 wrapper for `eshu report`.
//
// BuildDigest renders a deterministic digest for a validated, share-safe
// Options value: a scope (repo:owner/name, service:name, workload:name,
// environment:name, or project:name), a runtime profile, and a suggested-
// question limit. This first CLI implementation is an offline presentation
// path -- it validates the contract, emits deterministic unsupported
// sections, and points operators to bounded follow-up routes, without
// reading graph state, writing graph state, claiming reducer work, or
// calling providers. BuildArtifact and WriteArtifact wrap a Digest in the
// operator_digest_artifact.v1 shape, validate it against the artifact
// contract, and persist it as mode-0600 JSON.
//
// The package is process-neutral in the sense that matters here: it reads
// no cobra flags, no process environment, and no command-line arguments, and
// it never touches os.Stdout or os.Stderr. RenderText formats the report into
// an io.Writer the caller supplies; every other function returns data and
// errors. OptionsFromFlags takes the plain --scope/--profile/--question-limit
// values already extracted from cobra flags, not a *cobra.Command, so the
// validation and scope-parsing rules stay unit-testable without cobra.
// go/cmd/eshu's operator_digest_cmd.go is the thin cobra wrapper that
// resolves process state -- flags and output streams -- and passes it in as
// plain values, then prints the resulting Digest/Artifact and maps errors to
// the exit-code contract. This split is mechanical, not a design choice
// specific to this package: cmd/eshu is `package main`, so nothing can import
// it, and any symbol reading cobra flags, process env, or the exit-code
// contract has to stay there.
package opdigest
