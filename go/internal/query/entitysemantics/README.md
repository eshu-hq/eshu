# entitysemantics

Renders an entity's promoted metadata into the prose summary and semantic
profile that entity-shaped responses carry.

## What lives here

`AttachSemanticSummary` writes a one-sentence, entity-type-specific summary
onto a result map. Behind it sit the per-language interpreters that produce the
fields the summary reads: Python metadata promotion, TypeScript declaration
merging, and JavaScript framework shapes, plus the semantic-profile builder
that assembles them.

## Why it is not in querycontract

`querycontract` is the dependency-neutral contract leaf: wire and port types
that several query families need without inheriting a runtime. Its `AGENTS.md`
bars family-specific response models, and prose rendering is exactly that — it
decides how one family's answers read, not what any family's contract is.

This code was briefly placed in `querycontract` during #6060 and moved here
once that boundary was checked against the rule. `querycontract` is also
already above the 40-file directory cap under an exemption tracked by #6597,
so growing it with family shaping made a known problem worse.

`codeshaping` is the same shape one family over, and is the precedent this
package follows.

## Boundaries

Depends only on `querycontract`, for row-value decoding. It does not reach the
graph, the content store, or the network, and it holds no handler.

Root keeps lowercase forwarders for its own callers, so the move changed no
existing call site outside this package.
