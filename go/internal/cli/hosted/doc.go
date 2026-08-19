// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package hosted holds the decision logic behind `eshu hosted-setup` and `eshu
// hosted-onboard`: the ordered connection checks against a deployed Eshu
// service, the narrow-versus-broad repository rule classification that refuses
// accidental org-wide ingestion, and the redacted onboarding artifact a team is
// handed.
//
// ExecuteSetup runs six ordered stages — endpoint/auth resolution, /healthz,
// /readyz, index readiness, MCP tool visibility, and one bounded query — and
// records each with its own FailCategory so an operator sees which stage failed
// and why. The contract that matters: Result.Connected reports true only when
// the final bounded query returned. Health or readiness alone is never success.
//
// ExecuteOnboard wraps those stages with rule validation. ClassifyRepoRules
// treats an empty rule set and any whole-org selector as broad, and ExecuteOnboard
// refuses a broad set BEFORE any connection check runs unless OnboardOptions
// .ConfirmBroad is set. A refusal still returns a populated, shareable Artifact
// so the team gets guidance either way.
//
// # Redaction contract
//
// The Artifact is written to disk and handed to a project team, so it carries
// only a token SOURCE NAME (the ESHU_API_KEY env var name), never a token value.
// APIURL, MCPURL, and SetupSnippet are all rendered through the CLI's shared
// endpoint redaction, evidredact.Endpoint, which removes userinfo
// (user:password@), credential-named query values, and the whole fragment while
// keeping scheme, host, and path so the target stays recognizable. Result —
// hosted-setup's own output,
// read by the operator who owns the credential — is redacted differently: it
// carries a redacted TokenRef but its ServiceURL, SetupHint, and Stage details
// are verbatim. See README.md for what redaction does and does not cover.
//
// # Boundary
//
// This package reads no cobra flag, resolves no endpoint or credential from the
// process environment, opens no network connection, and never calls os.Exit. All
// hosted-service traffic runs through the Deps seams that go/cmd/eshu wires; the
// only syscall this package makes on its own is the os.WriteFile inside
// WriteArtifact, against the operator-supplied --out path. go/cmd/eshu's
// hosted.go holds the thin cobra wrappers that read flags, resolve streams,
// build the HTTP seams, and map a result to an exit code.
package hosted
