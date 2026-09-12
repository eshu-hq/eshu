// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package deadcode implements dead-code analysis for the code-family
// queries: candidate scanning, cross-repo consumer evidence, investigation
// packets, downgraded-root verdicts, and the default reachability policy.
// It split out of package codequery (#6060 lane A) so the analysis can be
// read, tested, and changed without pulling in the rest of the code
// surface, and so it can one day move into its own repo.
//
// The split keeps two queryplan-pinned row readers in codequery --
// (*CodeHandler).deadCodeCandidateRows and
// (*CodeHandler).deadCodeResultsWithGraphIncomingEdges stay byte-identical
// there with their source_sha256 pins (see
// go/internal/queryplan/testdata/query-source-coverage.yaml). Everything
// else dead-code-specific lives here behind Analyzer, which carries its
// few staying codequery dependencies as explicit func fields (see
// deadcode/AGENTS.md). CodeHandler delegates to a per-call Analyzer;
// there is no shared mutable state.
//
// Import discipline: this package may import the same dependency-neutral
// leaves codequery uses (codemodel, querycontract, queryauth, contentread,
// entitysemantics, codeshaping, codeprovenance, rows, telemetry)
// but NEVER package codequery itself and NEVER root package query -- both
// would create an import cycle (codequery delegates to Analyzer, and root
// aliases codequery).
package deadcode
