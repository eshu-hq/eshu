# Producer Grant DRAFT (`#8868`, `#6709`)

**DRAFT — not issued, not stored, not enforced.** Records the intended
first-party delegation for the extracted `terraform-state` producer so
review can approve the exact kind/version/scope binding before cutover.
Core retains fact admission, canonical identity, projection,
persistence, claim mutation, fencing, and retraction; this grant
authorizes emission only.

## Proposed grant identity

- Producer: `dev.eshu.collector.terraform-state`
- Version: `0.1.0` (collector artifact version at cutover; NOT a
  published SDK pin — `go/go.mod` holds
  `github.com/eshu-hq/eshu-sdk/collector v0.1.0-rc.3` and
  `github.com/eshu-hq/eshu-sdk/factschema v0.1.0-rc.5`, so no published
  version is claimed here)
- Scope: the `terraform_state` collector-instance scope
- Kinds (`1.0.0` each): the seven `terraform_state_*` kinds in
  [SHIM_MAP](SHIM_MAP.md) §2 (the `TerraformStateFactKinds()` list in
  `facts/cloud/terraform_state.go:54-74` minus the git-owned
  candidate) **plus** the 3 co-emitted `incident_routing` kinds at
  `1.0.0` (`applied_pagerduty_resource`, `applied_alert_route`,
  `coverage_warning` — arbiter ruling on #9195: extend, no remaining
  owner for two of the three)
- Lifetime: bounded `--expires-in` (rotation expected; no non-expiring
  grants)

## Issuance (operator commands at cutover)

One grant per core-owned kind (the CLI binds one kind per grant):

```bash
for kind in snapshot resource output module provider_binding tag_observation warning; do
  eshu component grant dev.eshu.collector.terraform-state \
    --component-home ~/.eshu/components \
    --version 0.1.0 \
    --kind "terraform_state.${kind}" \
    --schema-version 1.0.0 \
    --scope terraform_state \
    --expires-in 2160h
done
for kind in applied_pagerduty_resource applied_alert_route coverage_warning; do
  eshu component grant dev.eshu.collector.terraform-state \
    --component-home ~/.eshu/components \
    --version 0.1.0 \
    --kind "incident_routing.${kind}" \
    --schema-version 1.0.0 \
    --scope terraform_state \
    --expires-in 2160h
done
eshu component grants --component-home ~/.eshu/components \
  --producer dev.eshu.collector.terraform-state
```

Grant-first-then-install: authorize before activating the replacement,
which is the order cutover needs.

Explicit exclusion: `terraform_state_candidate` is family-declared but
git-collector-owned (`terraform_state.go:117` in `facts/cloud`,
authority `core_owned`). It is NOT in this grant; the extracted
`terraform-state` producer must never declare or emit it. A trial
emission of an excluded kind fails closed under core admission.

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
- [ ] Redaction bundle provisioned to the external producer: redaction
  key, versioned `redact.RuleSet` (`ESHU_TFSTATE_REDACTION_RULESET_VERSION`),
  and packaged provider-schema bundle — all three are startup-fatal when
  absent (`config.go:229-264`), and without the schema resolver every
  scalar redacts and state rows orphan from their cloud resources.
- [ ] `allow_empty_successor` attestation path agreed: the operator flag
  (config or `ESHU_TFSTATE_ALLOW_EMPTY_SUCCESSOR`) carries over, and
  only the legitimate-empty shape bypasses the successor-emptiness
  backstop.
