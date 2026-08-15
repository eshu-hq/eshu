// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package answerqualityscorecard holds the offline logic behind `eshu
// answer-quality-scorecard`: reading a captured, redacted answer-quality
// evidence artifact, rendering the scored verdict for a terminal, and
// summarizing the failed criteria into one line for the command's error.
//
// Scoring itself belongs to internal/answerquality. This package reads the
// bytes that go in (ReadEvidence) and formats the Verdict that comes out
// (RenderVerdict, FailureSummary); it never scores, and it holds no scoring
// rule of its own.
//
// The package reads no cobra flags, no Eshu config, and no process
// environment, and it never calls os.Exit -- go/cmd/eshu is package main, so
// nothing can import it, and any symbol that resolves a flag or maps a result
// to an exit code has to stay in answer_quality_scorecard_cmd.go. That wrapper
// resolves --from and --json, passes cmd.InOrStdin() and cmd.OutOrStdout() in
// as plain io.Reader/io.Writer values, and turns a failed verdict into a
// non-zero exit. ReadEvidence does call os.ReadFile directly on the path
// parameter it is given, which is acting on an explicit argument rather than
// process wiring, matching internal/cli/servicereport's ReadInput.
//
// # Redaction
//
// This package performs no redaction. RenderVerdict prints the verdict's run
// id and each criterion's detail text verbatim, and both can carry values
// copied out of the captured evidence artifact. Whatever publish safety the
// output has comes entirely from the publish_safety criterion that
// internal/answerquality evaluates before the Verdict is built: a value that
// criterion accepts is printed here as-is. Do not add a redaction step to this
// package to compensate -- a second, weaker screen downstream of the real one
// hides which of the two is authoritative. Fix the screen.
package answerqualityscorecard
