// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package semanticdocs builds provenance-only semantic documentation
// observation facts from bounded documentation sections.
//
// The package accepts existing doctruth section inputs and mocked, already
// redacted observation output, then returns validated fact envelopes. It does
// not call providers, persist facts, write graph state, expose API routes, or
// admit observations as canonical truth. Unsafe redaction state fails closed by
// forcing provenance-only output and dropping observation text.
//
// When a caller sets SectionInput.SourceACLState from the owning
// source/document fact, the emitter propagates that bounded source access
// posture verbatim onto the observation payload's acl_summary, so the
// docs-evidence projection and readbacks carry the posture end-to-end. It is
// omitted when no bounded ACL claim was observed; this is factual propagation
// only and never a disclosure or enforcement decision.
package semanticdocs
