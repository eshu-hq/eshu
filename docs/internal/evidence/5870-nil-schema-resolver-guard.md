# #5870 — an empty provider-schema bundle was a running mode, not a failure

## What was wrong

`LoadPackagedSchemaResolver` returned `(nil, nil)` when neither the operator's
schema directory nor the binary's embedded bundle produced a single attribute —
success carrying a nil resolver. Nothing upstream turned that into a failure, so
`cmd/collector-terraform-state` started with `SchemaResolver == nil`.

Root-Cause Evidence: `schemaTrust`
(`go/internal/collector/terraformstate/attributes.go`) answers
`redact.SchemaUnknown` for **every** `(resourceType, attributeKey)` pair against
a nil resolver, with no exemption, and under `SchemaUnknown` the redaction rules
fail closed and the scalar becomes a redaction map. Failing closed is correct
for a value and wrong for a join key: `arn` was redacted along with everything
else.

A redacted `arn` does not lose one attribute, it breaks the join.
`listActiveStateResourcesForAWSARNsQuery` matches state rows to cloud resources
with an **inner join** on `attributes->>'arn'` against ARNs already loaded from
the AWS generation, and a redaction marker renders as JSON text that equals no
real ARN. Every state row under that bundle is therefore dropped at the
database: each cloud resource finds no state to compare against and
reclassifies as `orphaned_cloud_resource`, with nothing downstream naming
redaction as the cause, because the rows never arrive.

**Correction (#6017 review).** An earlier draft of this note said the redacted
`arn` rendered through `coerceJSONString` into a non-empty garbage string that
passed the `arn == ""` guard and *became a key* in `stateByARN`. That path is
not reachable in production — the inner join drops the row first, so
`awsRuntimeStateRowFromPayload` never sees it and `state_resource_arn_redacted`
cannot fire from the bounded loader. The orphan outcome is real; the mechanism
is the join, not a garbage key. The distinction matters here because it removes
the last downstream place the failure could have been noticed, which is the
argument for failing at startup.

The loop that populates the attribute set swallows per-file failures — `os.Open`
errors `continue`, and `parseSchemaInto` returns nothing checkable — so a
corrupt or unparseable bundle produced the nil silently.

## What changed

`newPackagedSchemaResolver` rejects an empty attribute set, so
`LoadPackagedSchemaResolver` returns an error naming the schema directory and
the consequence instead of `(nil, nil)`.
`cmd/collector-terraform-state/config.go` already wraps and returns that error,
so the collector fails to start rather than degrading every attribute of every
resource.

This is not a policy trade. Reaching the empty case means the binary's **own
embedded bundle** produced nothing, since the loader falls back to it whenever
the operator directory yields nothing. That is a broken build or a broken
deployment — a condition to fail on, not to run in.

The guard is split into `newPackagedSchemaResolver` purely so the empty case is
testable: in a correctly built binary the embedded bundle always loads, so the
branch could otherwise only be exercised by breaking the build.

## What is deliberately NOT changed

The issue offers a second option: exempting identity anchors (`arn`, `id`) from
`SchemaUnknown` fail-closure. That is not done here, and the reason is not
scope-trimming:

- It weakens fail-closed on a field that can legitimately carry account
  identifiers, which is a redaction-policy decision for the repo owner rather
  than a code change.
- It is the only fix that reaches the **per-provider gap** — a resolver that
  loads successfully but lacks one provider still answers `SchemaUnknown` for
  that provider's attributes, which no nil check can catch.

So the per-provider gap remains open after this change. It is named here rather
than left implicit, and asked on the issue. The two options are not mutually
exclusive; this one closes the deployment-wide case, and only that case.

## What the guard does not catch

It fires only when the attribute set is **entirely** empty. `parseSchemaInto`
still swallows per-file failures, so a bundle where most files fail to parse and
one succeeds produces a small non-empty map, passes this guard, and leaves every
resource type it does not cover classifying as `SchemaUnknown` — the same
redaction outcome as the nil case, just narrower.

That is the per-provider gap named above wearing a different hat, and it has the
same fix (option 2) rather than a second nil check. Saying so here because the
guard reads like it closes the whole class and it does not: it closes the case
where the collector has no usable schema at all.

## No-Regression Evidence

No-Regression Evidence: the changed code runs once at collector startup, not on
any per-attribute or per-resource path. `newPackagedSchemaResolver` adds one
`len()` check to a function that previously performed the same check inline, so
the executed work at startup is unchanged and there is nothing on a hot path to
measure. The behavioural claim is proven by the tests below, not by timing.

Focused verification, run after the final edit:

```bash
cd go && go test ./internal/collector/terraformstate/ \
  ./cmd/collector-terraform-state/ -count=1
```

Both packages `ok`.

New tests, the first two failing before the change:

- `TestNewPackagedSchemaResolverRejectsAnEmptyBundle` — an empty set is an
  error, and the message names the directory an operator would check.
- `TestNewPackagedSchemaResolverAcceptsANonEmptyBundle` — the guard does not
  reject a bundle that did load.
- `TestLoadPackagedSchemaResolverNeverReturnsANilResolverWithoutAnError` —
  asserts the contract at the public boundary with the worst input an operator
  can supply (a schema directory that does not exist): the embedded bundle
  carries the load and the function never hands back `(nil, nil)`.

## Observability Evidence

No-Observability-Change: this change removes a silent degraded mode rather than
adding a new runtime path, so there is no new stage to instrument. The condition
the issue asked to make observable — "this deployment has no usable schema
bundle at all", previously indistinguishable from "a few attributes were
redacted" in `eshu_dp_tfstate_redactions_applied_total{reason}` — is now
observable in the strongest available form: the collector refuses to start and
reports the reason on the startup error path
(`runtime.startup.failed`), so it cannot be mistaken for ordinary redaction
volume. The existing `eshu_dp_tfstate_redactions_applied_total` counter and the
`SchemaResolverEntryCounter` startup log
(`cmd/collector-terraform-state/service.go`) are unchanged and still report
per-attribute redaction and loaded-schema coverage.

## Acceptance criteria: what this PR meets, and what it does not

Stated criterion by criterion so the merge does not imply the issue is fully
satisfied (#6017 review).

| # | Criterion | Status |
| --- | --- | --- |
| 1 | A nil resolver either cannot reach the parser, **or** cannot redact an identity anchor | **Met**, by the first disjunct. Option 1: the constructor errors, so a nil resolver never reaches the parser. |
| 2 | The per-provider gap (resolver present, one provider missing) addressed or documented out of scope with the reason | **Documented out of scope.** A nil check cannot reach it: a bundle can load successfully and still lack one provider, whose attributes then come back `SchemaUnknown` per-attribute. Closing it needs the option-2 identity-anchor exemption, not a constructor guard. |
| 3 | A test failing before the fix, asserting a declared ARN **survives** a `SchemaUnknown` classification | **NOT met.** This asserts option-2 behaviour — that an identity anchor is exempt from fail-closure. Nothing here exempts `arn`; it prevents the blanket-unknown *state* instead. The test added, `TestLoadPackagedSchemaResolverNeverReturnsANilResolverWithoutAnError`, fails before the fix but asserts the constructor contract, not anchor survival. |
| 4 | The decision between the options stated explicitly, with its redaction-policy implication | **Met.** Option 1 only. Option 2 is a redaction-policy call — whether an identity anchor may ever be redacted, on a field that can legitimately carry account identifiers — and is left to the owner. Nothing here presumes an answer. |
| 5 | The condition is observable | **Partially met.** "No usable schema bundle at all" is now maximally observable: the collector fails to start with an error naming the schema directory. What is still not distinguished is the per-provider case in row 2 — a bundle that loads but covers one provider thinly still redacts silently, and `eshu_dp_tfstate_redactions_applied_total{reason}` does not separate it. |

Rows 3 and 5 close together with option 2, not separately. Both are the same
missing thing: an identity-anchor exemption that survives `SchemaUnknown`
regardless of why the schema is unknown.
