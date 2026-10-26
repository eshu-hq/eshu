# Producer Grant DRAFT (`#8863`, `#6709`)

**DRAFT — not issued, not stored, not enforced.** Records the intended
first-party delegation for the extracted `pagerduty` producer so review
can approve the exact kind/version/scope binding before cutover. Core
retains fact admission, canonical identity, projection, persistence, and
retraction; this grant authorizes emission only.

## Proposed grant identity

- Producer: `dev.eshu.collector.pagerduty`
- Version: `0.1.0` (collector artifact version at cutover)
- Scope: `pagerduty`
- Kinds (`1.0.0` each; producer records in
  `go/internal/facts/incident/context.go` and `routing.go`):
  `incident.record`, `incident.lifecycle_event`, `change.record`,
  `incident_routing.coverage_warning`,
  `incident_routing.observed_pagerduty_service`,
  `incident_routing.observed_pagerduty_integration`
- Lifetime: bounded `--expires-in` (rotation expected; no non-expiring grants)

## Issuance (operator commands at cutover)

One grant per core-owned kind (the CLI binds one kind per grant):

```bash
for kind in incident.record incident.lifecycle_event change.record; do
  eshu component grant dev.eshu.collector.pagerduty \
    --component-home ~/.eshu/components \
    --version 0.1.0 \
    --kind "${kind}" \
    --schema-version 1.0.0 \
    --scope pagerduty \
    --expires-in 2160h
done
for kind in coverage_warning observed_pagerduty_service observed_pagerduty_integration; do
  eshu component grant dev.eshu.collector.pagerduty \
    --component-home ~/.eshu/components \
    --version 0.1.0 \
    --kind "incident_routing.${kind}" \
    --schema-version 1.0.0 \
    --scope pagerduty \
    --expires-in 2160h
done
eshu component grants --component-home ~/.eshu/components \
  --producer dev.eshu.collector.pagerduty
```

Grant-first-then-install: authorize before activating the replacement,
which is the order cutover needs.

Shared-kind guard: `change.record` is also emitted by other provider
collectors for their own providers. The grant binds producer identity +
kind + schema version + scope, so the extracted producer emits only
under `scope pagerduty` and can never write under a sibling's
authority.

## Enforcement (already core-owned, `#6709`)

- Admit only when producer identity, version, kind, schema version, and
  scope all match a live, unexpired, unrevoked stored grant; anything
  else fails closed at install, activation, and every emission.
- Registry read failures deny core-owned kinds rather than emitting
  under unknown authorization.
- Revocation (`component revoke-grant …`) fails the next emission
  terminal; absent-grant revocation returns `grant_not_found`, never
  silent success.

## First-party note

First-party status changes packaging trust, not authority: the extracted
producer still emits through the grant like any external producer.
Delegation does not transfer canonical ownership — no graph edges, no
identity choice, no queue or storage access.

## Pre-issuance checklist

- [ ] Satellite `go.mod` reconciled to the supported SDK path at the
  exact pins (`collector v0.1.0-rc.3`, `factschema v0.1.0-rc.5`).
- [ ] Parity proof green on the fixture pack (`#4047` gate input).
- [ ] Rollback rehearsed ([ROLLBACK_DRAFT](ROLLBACK_DRAFT.md)).
- [ ] Jira (`#8858`) cutover landed first; PagerDuty activation uses the
  new instance id with the same scope id.
- [ ] Expiry/rotation owner and alert recorded before issuance.
