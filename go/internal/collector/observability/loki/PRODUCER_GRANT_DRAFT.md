# Producer Grant DRAFT (`#8860`, `#6709`)

**DRAFT — not issued, not stored, not enforced.** Records the intended
first-party delegation for the extracted `loki` producer so review can
approve the exact kind/version/scope binding before cutover. Core
retains fact admission, canonical identity, projection, persistence, and
retraction; this grant authorizes emission only.

## Proposed grant identity

- Producer: `dev.eshu.collector.loki`
- Version: `0.1.0` (collector artifact version at cutover)
- Scope: `loki`
- Kinds (`1.0.0` each, `facts/observability.go:88-110`):
  `observability.source_instance`, `observability.observed_log_signal`,
  `observability.observed_rule`, `observability.coverage_warning`
- Lifetime: bounded `--expires-in` (rotation expected; no non-expiring grants)

## Issuance (operator commands at cutover)

One grant per core-owned kind (the CLI binds one kind per grant):

```bash
for kind in source_instance observed_log_signal observed_rule coverage_warning; do
  eshu component grant dev.eshu.collector.loki \
    --component-home ~/.eshu/components \
    --version 0.1.0 \
    --kind "observability.${kind}" \
    --schema-version 1.0.0 \
    --scope loki \
    --expires-in 2160h
done
eshu component grants --component-home ~/.eshu/components \
  --producer dev.eshu.collector.loki
```

Grant-first-then-install: authorize before activating the replacement, which
is the order cutover needs.

Shared-kind guard: `observability.source_instance`,
`observability.observed_rule`, and `observability.coverage_warning` are
also emitted by the grafana, prometheus-mimir, and tempo collectors for
their own providers. The grant binds producer identity + kind + schema
version + scope, so the extracted producer emits only under `scope loki`
and can never write under a sibling's authority.
`observability.observed_log_signal` is emitted only by this producer.

## Enforcement (already core-owned, `#6709`)

- Admit only when producer identity, version, kind, schema version, and scope
  all match a live, unexpired, unrevoked stored grant; anything else fails
  closed at install, activation, and every emission.
- Registry read failures deny core-owned kinds rather than emitting under
  unknown authorization.
- Revocation (`component revoke-grant …`) fails the next emission terminal;
  absent-grant revocation returns `grant_not_found`, never silent success.

## First-party note

First-party status changes packaging trust, not authority: the extracted
producer still emits through the grant like any external producer. Delegation
does not transfer canonical ownership — no graph edges, no identity choice,
no queue or storage access.

## Pre-issuance checklist

- [ ] `#6710` template repo exists; manifest version equals grant version.
- [ ] Parity proof green on the fixture pack (`#4047` gate input).
- [ ] Rollback rehearsed ([ROLLBACK_DRAFT](ROLLBACK_DRAFT.md)).
- [ ] Expiry/rotation owner and alert recorded before issuance.
