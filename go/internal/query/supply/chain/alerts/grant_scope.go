// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package alerts

import "github.com/eshu-hq/eshu/go/internal/scope/org/shard"

// A security-alert org target with more than 100 same-org repositories is
// collected as shard scopes "<base>#shard-<bits>" (#9019), so its
// reconciliation rows carry the shard scope id. An operator grants the
// configured target, the base scope id, never a generated shard id. The
// shard grant disjuncts below let a granted id g also cover every row whose
// scope_id carries the marker and whose text before the first marker equals
// g. That is exact: a base never contains the marker (configuration
// validation refuses it), so the text before the first marker of a shard id
// is its base. An ungranted base and an id that only shares a prefix with the
// base fail the equality, and a granted id that carries the marker can never
// equal a split_part result, which has none. The comparison is an equality
// against the same grant array the exact disjuncts use, not a LIKE, so no
// character of g is a pattern, and the fragments hold no "%" because the
// aggregate templates go through fmt.Sprintf. Measured cost: see the
// supply/chain alerts README (#9019).

// shardGrantMatch8 is the shard grant disjunct for the aggregate queries,
// whose grant set is $8.
const shardGrantMatch8 = `
      OR (strpos(fact.scope_id, '` + shard.Marker + `') > 0
          AND split_part(fact.scope_id, '` + shard.Marker + `', 1) = ANY($8::text[]))`

// shardGrantMatch11 is the shard grant disjunct for the list query, whose
// grant set is $11.
const shardGrantMatch11 = `
      OR (strpos(fact.scope_id, '` + shard.Marker + `') > 0
          AND split_part(fact.scope_id, '` + shard.Marker + `', 1) = ANY($11::text[]))`
