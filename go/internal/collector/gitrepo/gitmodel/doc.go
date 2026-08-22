// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package gitmodel holds the types and helpers every gitrepo subpackage needs,
// and nothing else.
//
// It exists to break an import cycle rather than to be a home for stray code.
// The leaf emitters (gitdocs, gitobs, gitcodeowners, gitsubmodule,
// gitsvccatalog, gittfstate, workflowimage) all need the fact-stream writer and
// the content-file records, while gitrepo's streamFacts calls into every one of
// those leaves. Whatever both sides touch has to sit below both, which is this
// package. Keep it that way: anything only one caller needs belongs with that
// caller.
package gitmodel
