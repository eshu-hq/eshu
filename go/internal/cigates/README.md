# cigates

`cigates` is the typed core of the CI gate registry ([#4213](https://github.com/eshu-hq/eshu/issues/4213)). It provides the loader, selector, validator, and glob matcher that back the `cmd/ci-gates` CLI and the `scripts/dev/select-gates.sh` / `scripts/dev/run-selected-gates.sh` wrappers.

It answers one question: **given a set of changed paths and a tier ceiling, which credential-free CI verifiers should run locally — and which are registered but CI-only or out of scope?**

## Files

| File | Purpose |
| --- | --- |
| `registry.go` | Types (`Registry`, `Gate`, `Tier`, `Category`, `Requirement`, `Local`, `CI`) and `Load` |
| `select.go` | `(*Registry).Select` — pure path-trigger matcher |
| `validate.go` | `(*Registry).Validate` — script + workflow existence checks |
| `glob.go` | `MatchGlob` — doublestar matcher, no external deps |

## Registry format

The registry lives at `specs/ci-gates.v1.yaml`. Each gate entry has a stable kebab-case id, a tier, a set of path-glob triggers, an optional local command, and a CI workflow reference. Gates whose `local` field is absent are CI-only and always require a non-empty `ci_only_reason`.

## Selector semantics

`Select(changed, tier)` returns one `Selection` per gate in registry order. A gate is selected when:

1. Its tier is ≤ the requested tier ceiling.
2. At least one of its triggers matches at least one changed path.
3. Its `local` field is non-nil (CI-only gates are reported but never selected).

`ci-heavy` and `manual` tiers are never selected locally, regardless of the requested ceiling.

## Glob semantics

`MatchGlob` supports:
- `**` — matches zero or more path segments.
- `*` — matches any characters within a single segment (no `/` crossing).
- All other characters are literal.

Patterns with a leading `/` or trailing `/` never match.

## Tests

```bash
cd go && go test ./internal/cigates/ -count=1
```
