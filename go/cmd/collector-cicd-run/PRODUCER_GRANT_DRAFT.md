# Producer Grant DRAFT (`#8854`, `#6709`)

**DRAFT — not issued, not stored, not enforced.** Records the intended
first-party delegation for the extracted `cicd-run` producer so review
can approve the exact kind/version/scope binding before cutover. Core
retains fact admission, canonical identity, projection, persistence,
and retraction; this grant authorizes emission only.

## Proposed grant identity

- Producer: `dev.eshu.collector.cicd-run`
- Version: `0.1.0` (collector artifact version at cutover; NOT a
  published SDK pin — `go/go.mod` holds `sdk/go/*` at `v0.0.0` with
  local replaces, so no published version is claimed here)
- Scope: the `ci_cd_run` collector-instance scope
- Kinds (`1.0.0` each, `go/internal/facts/ci_cd_run.go:45-69`): the
  nine `ci.*` kinds in [SHIM_MAP](SHIM_MAP.md) §2
- Lifetime: bounded `--expires-in` (rotation expected; no non-expiring
  grants)

## Issuance (operator commands at cutover)

One grant per core-owned kind (the CLI binds one kind per grant):

```bash
for kind in pipeline_definition run job step artifact trigger_edge environment_observation deployment_event warning; do
  eshu component grant dev.eshu.collector.cicd-run \
    --component-home ~/.eshu/components \
    --version 0.1.0 \
    --kind "ci.${kind}" \
    --schema-version 1.0.0 \
    --scope ci_cd_run \
    --expires-in 2160h
done
eshu component grants --component-home ~/.eshu/components \
  --producer dev.eshu.collector.cicd-run
```

Grant-first-then-install: authorize before activating the replacement,
which is the order cutover needs.

Explicit exclusion: `ci.workflow_image_evidence` is family-declared
but git-collector-owned
(`go/internal/collector/repo/git/workflow/image/facts.go`). It is NOT
in this grant; the extracted `cicd-run` producer must never declare or
emit it. A trial emission of an excluded kind fails closed under core
admission.

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

First-party status changes packaging trust, not authority: the
extracted producer still emits through the grant like any external
producer. Delegation does not transfer canonical ownership — no graph
edges, no identity choice, no queue or storage access.

## Pre-issuance checklist

- [ ] `#6710` template repo exists; manifest version equals grant
  version.
- [ ] Parity proof green on the fixture pack (`#4047` gate input).
- [ ] Rollback rehearsed ([ROLLBACK_DRAFT](ROLLBACK_DRAFT.md)).
- [ ] Expiry/rotation owner and alert recorded before issuance.
