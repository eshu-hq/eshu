// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package gitcontent builds the durable content, content-entity, repository,
// and file fact envelopes for the git collector.
//
// It is a leaf: gitrepo calls the envelope constructors during emission, and
// this package never imports gitrepo back. Ref selection and ref-payload
// shaping stay on the caller side for the same reason — the GitRef type lives
// in gitrepo, so callers pass the precomputed default branch and ref payload.
// Anything both sides need lives in
// go/internal/collector/repo/git/model.
//
// ApplyContentStore applies a parser definition's content-store policy to a
// source body before any fact exists: store keeps it, metadata empties it, and
// redact runs the marker redactor (closed-schema files, #8812) or the
// allowlist-skeleton redactor (open-schema files, #8833). Every failure path
// returns an empty body, never the source.
package gitcontent
