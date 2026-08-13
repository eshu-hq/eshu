// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package evidpacket holds the logic behind the `eshu evidence-packet-dogfood`
// command: fetching the captured benchmark artifact, rendering the scored
// verdict as an operator-facing text report, and summarizing the failed
// criteria for an error message. A second command calls in here too:
// `eshu competitive-parity validate` scores a committed fixture benchmark and
// puts FailureSummary's line in its own error, so a change to that function's
// output shows up in two commands. That command uses neither ReadBenchmark
// nor RenderVerdict.
//
// The scoring itself is not here. Parsing and grading a benchmark belong to
// internal/packetdogfood; this package depends on that one for the Verdict,
// Criterion, and CriterionStatus types and their constants, and makes no call
// into it.
//
// Despite the family's name, nothing in this package produces an evidence
// packet. The benchmark it reads is a captured JSON artifact that measures
// portable v2 evidence packets against raw-search and tool-drilldown baselines.
//
// The two paragraphs that follow describe dogfood.go, which holds every
// non-test declaration in this package. The file you are reading carries this
// comment and the package clause, nothing more.
//
// dogfood.go's filesystem surface is a single read: ReadBenchmark calls
// os.ReadFile on the operator's --from path. No file or directory is created,
// truncated, renamed, or chmod'd, and there is no temporary file.
// ReadBenchmark's other branch calls io.ReadAll on the io.Reader it was handed;
// it never reaches for os.Stdin itself. The scoping matters because
// dogfood_test.go shares this package and does write files: os.WriteFile under
// a t.TempDir. t.TempDir calls os.MkdirTemp(os.Getenv("GOTMPDIR"), ...), so
// GOTMPDIR is the variable it reads first; os.MkdirTemp falls back to
// os.TempDir, and therefore TMPDIR, only when GOTMPDIR is unset.
//
// dogfood.go reads no process environment variable, and every construct that
// would read one behind a helper is absent: it makes no os.Getenv or
// os.LookupEnv call; no os.UserHomeDir or os.UserConfigDir call, which would
// read HOME or USERPROFILE; no os.CreateTemp, os.MkdirTemp, or os.TempDir
// call, which would read TMPDIR; no exec.Command, which would resolve a binary
// through PATH; and no HTTP client, whose default transport would honour
// HTTP_PROXY, HTTPS_PROXY, and NO_PROXY. It starts no subprocess and opens no
// network connection. It writes to no output stream either: its fmt.Fprintf and
// fmt.Fprintln calls all target the local strings.Builder in RenderVerdict,
// which returns the text so the caller owns the stream.
//
// go/cmd/eshu/evidence_packet_dogfood_cmd.go is the thin cobra wrapper for the
// dogfood command. It owns what this package deliberately does not:
// registering on rootCmd, declaring and reading the --from and --json flags,
// resolving cobra's input and output streams, calling
// packetdogfood.ParseBenchmark and packetdogfood.Score, encoding the JSON
// form, and turning a failing verdict into a non-nil error so the process
// exits non-zero. That split is mechanical rather than a judgement about this
// package: go/cmd/eshu is `package main`, so nothing can import it, and any
// symbol that reads cobra flags, the process environment, or the exit-code
// contract has to stay there.
package evidpacket
