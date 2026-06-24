// Command skillgen generates and verifies the per-host skill-file
// roundtrip baseline that the Eshu skillgen epic ships.
//
// Two subcommands are wired:
//
//   - gen reads skill-fragments/, renders per host, writes expected/.
//   - check reads skill-fragments/, renders per host, byte-compares to
//     expected/. Exits non-zero on any drift.
//
// Both subcommands read the same inputs (a fragments directory and an
// expected root) so the gen output and the check baseline are always
// produced from the same source. The capability override file at
// `<fragments>/capabilities.local.yaml` is read when present; its
// absence means the default capability set (all collectors enabled).
//
// The command is a thin driver; the loader, render pipeline, and drift
// check live in github.com/eshu-hq/eshu/go/internal/extensions/skillgen.
// Run from the go module directory:
//
//	go run ./cmd/skillgen gen
//	go run ./cmd/skillgen check
package main
