// Package resolutionparity holds the per-language call-resolution accuracy
// goldens and the CI parity gate for code-edge resolution provenance (issue
// #2226).
//
// Once code-call edges carry a resolution_method (ADR #2222, emitted by the
// reducer in #2223), the distribution of resolution tiers per language becomes
// measurable. This package parses the per-language fixture corpora with the real
// parser engine, runs the reducer's code-call extraction, and tallies the
// resolution_method of every emitted row. A checked-in golden pins the expected
// per-language distribution; the test fails when a parser or resolver change
// shifts the tiers, which is how a per-language resolution regression is caught
// in the normal `go test ./...` CI matrix.
//
// The golden is a regression snapshot of deterministic pipeline output, not a
// hand-authored target. Update it deliberately with the documented procedure in
// README.md when a tier shift is intended and explained.
package resolutionparity
