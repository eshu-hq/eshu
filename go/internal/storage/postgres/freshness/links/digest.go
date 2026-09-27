// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linksfreshnessstore

// PayloadDigestInput is the value every changed-since payload digest hashes.
// It is the fact payload, except that content_entity rows drop indexed_at: the
// git collector stamps every content_entity payload with the snapshot time
// (go/internal/collector/git/content/envelopes.go, the "indexed_at" field of
// ContentEntityFactEnvelope), so an unchanged entity re-indexed on a new run
// would otherwise differ from its prior copy and report updated. Nothing reads
// the field back. Existing generations keep it forever, so the diff normalizes
// it at read time; only content_entity is normalized because no other kind
// carries a per-run timestamp of this shape. This is data normalization of a
// known collector field, not an allowlist. The key removal applies only to
// object payloads: jsonb "payload - key" raises "cannot delete from scalar" on
// a scalar payload, which would fail the whole statement instead of one key,
// so a non-object payload digests as itself.
//
// This constant is the single source of the digest input. The parent postgres
// package's changed-since read statement (changedSincePayloadDigestInput),
// the golden-corpus oracle fragments generated from it, and the link writer in
// this package all use these bytes. Changing them changes every persisted
// state digest, so a change must bump DigestVersion.
const PayloadDigestInput = `CASE WHEN fact_kind = 'content_entity' AND jsonb_typeof(payload) = 'object' THEN payload - 'indexed_at' ELSE payload END`

// ReducerDerivedFactKindLikePattern matches every reducer-derived fact kind:
// the reducer writes its materialized output into the source generation after
// that generation activates, under a "reducer_" kind prefix. The underscore is
// escaped so a kind that merely starts with "reducer" does not match.
const ReducerDerivedFactKindLikePattern = `'reducer\_%'`

// ExcludeReducerDerivedKinds keeps reducer-derived rows out of a changed-since
// scan. They exist only in generations the reducer has processed, so their
// presence tracks reducer scheduling, not repository change.
const ExcludeReducerDerivedKinds = `fact_kind NOT LIKE ` + ReducerDerivedFactKindLikePattern

// DigestVersion pins the pair (PayloadDigestInput, ExcludeReducerDerivedKinds)
// and the state-digest construction of the link writer. A scope cursor whose
// digest_version differs from it has no usable state: the next full
// generation re-roots the scope.
const DigestVersion = 1
