// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package demo owns the Docker Compose lifecycle behind `eshu demo up`,
// `eshu demo status`, and `eshu demo down`, plus the TTFA scorecard behind
// `eshu demo-benchmark`.
//
// Runtime.Up brings a stack up under its own Compose project (DefaultProject,
// "eshu-demo"), waits for indexing completeness rather than process health,
// asks the first question in specs/demo-first-answers.v1.yaml, and returns a
// Result with per-phase wall times. Runtime.Down removes the stack, its
// volumes, and its networks -- but only after ownsProject confirms Compose's
// own com.docker.compose.project.config_files label names ComposeFileName, so
// a --project pointing at an operator's real stack is refused rather than
// destroyed. Runtime.Status reports running and indexed separately.
//
// This package shells out and talks to the network. That surface is:
//
// Nine docker invocations, all through the ExecFunc seam and all with
// "docker" as argv[0]. Compose calls carry -p <project> -f <compose-file>:
// `compose ps --quiet`, `compose config --images`, `compose build`,
// `compose up -d --wait`, `compose down -v --remove-orphans`, and
// `compose exec -T eshu printenv ESHU_API_KEY`. Three are not compose:
// `version --format {{.Server.Version}}`, `image inspect <names...>`, and
// `ps --all --filter label=com.docker.compose.project=<project> --format
// {{.Label "com.docker.compose.project.config_files"}}`. The build and up
// calls add ESHU_DEMO_API_KEY to the child environment; it holds a
// per-invocation credential minted by newEphemeralKey and never persisted.
//
// Three HTTP requests, all through one 30-second-timeout client and all
// sending Authorization: Bearer when a key is known: GET
// <apiBase>/api/v0/status/index, POST <mcpBase>/mcp/message carrying a
// JSON-RPC tools/call, and whatever "<METHOD> <path>" a manifest question
// declares against <apiBase>.
//
// Two file reads and no writes: os.Stat while ResolveComposeFile walks up
// from a directory, and os.ReadFile in LoadManifest.
//
// The package reads no cobra flag and calls os.Getenv nowhere. APIBase,
// MCPBase, and ResolveComposeFile take the caller's lookup function as a
// parameter, so ESHU_DEMO_BIND_ADDR, ESHU_DEMO_API_PORT, ESHU_DEMO_MCP_PORT,
// and ESHU_DEMO_COMPOSE_FILE are resolved by go/cmd/eshu's demo.go passing
// os.Getenv in. Two stdlib boundaries still consult the environment, which is
// why "reads no environment" is a statement about decisions and not about the
// process: runCommand builds the child environment from os.Environ(), and
// exec.CommandContext resolves "docker" through PATH; the HTTP client leaves
// Transport nil, so http.DefaultTransport honours HTTP_PROXY, HTTPS_PROXY,
// and NO_PROXY.
//
// go/cmd/eshu's demo.go holds the thin cobra wrappers.
// They read the flags, resolve os.Getwd and os.Getenv, resolve stdin and
// stdout through cmd.InOrStdin/cmd.OutOrStdout, and map a returned error to
// the CLI's exit code. That split is mechanical: go/cmd/eshu is `package
// main`, so nothing can import it, and any symbol reading a flag or mapping to
// an exit code has to stay there.
//
// The scorecard vocabulary (Criterion, CriterionName, CriterionStatus and
// their constants) and the status marker are imported from
// go/internal/cli/firstrunbench, and the envelope error object from
// go/internal/cli/firstrun, so a harness reading either scorecard sees one
// shape by construction rather than by parallel copies. criteria.go declares
// only the two criterion names scored solely by the demo lane
// (CriterionPhaseTimings, CriterionModeObserved). quoteIfEmpty is a
// deliberate local adaptation rather than a call to the importable
// firstrun.QuoteIfEmpty: the original fills a repo-argument slot, while this
// package's only caller renders the benchmark mode, so the placeholder here is
// mode-neutral and a test pins it.
//
// Outside the standard library the package depends only on gopkg.in/yaml.v3,
// for the manifest. Its Eshu imports are go/internal/cli/firstrunbench and
// go/internal/cli/firstrun, and it emits no telemetry.
package demo
