# internal/semanticcode

Builds `semantic.code_hint` fact envelopes from already-parsed,
already-redacted provider output.

## Why it exists

`semantic.code_hint` had a read contract, a payload schema, an MCP tool
(`list_semantic_code_hints`), a capability row, and a retention policy — and no
producer anywhere in the runtime. Every deployed
`GET /api/v0/semantic/code-hints` returned an empty list. Correctly formed, and
indistinguishable from "this repository has no hints". The #5552 deployed proof
found it; issue #5693 filed it.

This package is the missing producer. It is the code-hint twin of
[`internal/semanticdocs`](../semanticdocs/README.md), which does the same job
for documentation observations.

## What it is, and is not

It is **not** an LLM integration. It calls no provider and holds no
credentials. Its input boundary (`HintInput`) carries only output that has
already been produced, parsed, and redacted somewhere else.

What it owns is the part that must not vary between providers:

- the provenance envelope — which repository, file, span, and content hash a
  hint came from, so a reader can follow it back;
- the stable fact identity, so replaying the same provider output supersedes
  the previous fact instead of doubling it;
- policy, redaction, and freshness state;
- the non-canonical promotion boundary.

## The promotion boundary

A code hint is evidence. It is never a relationship.

Every payload carries `promotion_policy = requires_deterministic_evidence` and
an explicit `corroboration_state`, and neither is something a caller can talk
the emitter out of. A provider that returns `confidence: high` still produces
an uncorroborated hint, because confidence is a claim about the model's
certainty and corroboration is a claim about the code. Only deterministic
evidence moves the second one.

That boundary is enforced, not documented: `facts.ValidateSemanticCodeHintPayload`
runs on every payload before it becomes an envelope, and a hint that cannot
state its corroboration state does not ship.

## Staleness

`CodeSpanInput.ContentHash` is the hash of the source the provider actually
saw, and it is part of the fact identity. When the file changes, the hash
changes, and the new hint gets a new identity rather than quietly overwriting
the old one under the same key. A hint about code that no longer exists should
not be able to masquerade as current.

## Layout

| File | Holds |
| --- | --- |
| `emitter.go` | `Emitter`, its inputs, and `Emit` |
| `config.go` | config normalization, config validation, span validation |
| `payload.go` | one hint to one `SemanticCodeHintPayload` |

## Testing

```bash
cd go && go test ./internal/semanticcode -count=1
```
