// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package eval measures how well a router.Ranker picks the right MCP tool for
// a real question (epic #8752, issue #8756).
//
// The package holds an embedded corpus of questions, each with the tool an
// agent should be offered and optional acceptable alternates. Validate checks
// the corpus shape and that every named tool is registered. Run drives any
// router.Ranker over the corpus and reports top-1 and top-5 accuracy per
// family, plus every question whose expected tool did not rank first.
//
// The corpus is written from tool descriptions, the public starter prompts,
// and catalog sweep arguments, plus hand-written questions for gaps. It is
// never written from the curated intent phrases
// in internal/ask/catalog. A corpus built from the intents would only score the
// intents against themselves.
//
// The package is offline measurement tooling. It reads no configuration,
// serves no request, and emits no runtime telemetry; its report is the signal.
// It must not import the mcp root, which injects the ranker at runtime.
package eval
