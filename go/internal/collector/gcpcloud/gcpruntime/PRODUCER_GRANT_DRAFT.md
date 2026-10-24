# Producer Grant DRAFT (`#8856`, `#6709`)

**DRAFT — not issued, not stored, not enforced.** Records the intended
first-party delegation for the extracted `gcp-cloud` producer so review
can approve the exact kind/version/scope binding before cutover. Core retains
fact admission, canonical identity, projection, persistence, and retraction;
this grant authorizes emission only.

## Proposed grant identity

- Producer: `dev.eshu.collector.gcp-cloud`
- Version: `0.1.0` (collector artifact version at cutover)
- Scope: `gcp` (plus `secrets_iam` for the 3 mirror kinds — see open item)
- Kinds (schemas per `cloud/gcp.go:73-81` and the secrets/IAM registry):
  `gcp_cloud_resource` (`1.1.0`), `gcp_cloud_relationship`,
  `gcp_tag_observation`, `gcp_iam_policy_observation`, `gcp_dns_record`,
  `gcp_image_reference`, `gcp_collection_warning` (all `1.0.0`),
  plus mirror `gcp_iam_principal`, `gcp_iam_permission_policy`,
  `gcp_iam_trust_policy` (all `1.0.0`)
- Lifetime: bounded `--expires-in` (rotation expected; no non-expiring grants)

## Open item: mirror-kind authority

Today the 3 mirror kinds' producer record names `secrets-iam-posture`
(`go/internal/facts/identity/iam.go:216`) and their envelopes stamp
`collector_kind = "secrets_iam_posture"`, while this collector emits them.
Either the grant binds all 10 kinds to `dev.eshu.collector.gcp-cloud`
(re-stamp at extraction), or the extracted component carries a second
`secrets-iam-posture` delegation. Decide with `#6709` before issuance;
the parity pack (`source_parity_8856_test.go`) pins the current stamping so
either choice is verifiable.

## Issuance (operator commands at cutover)

One grant per core-owned kind (the CLI binds one kind per grant):

```bash
for kind in cloud_resource cloud_relationship tag_observation \
    iam_policy_observation dns_record image_reference collection_warning; do
  eshu component grant dev.eshu.collector.gcp-cloud \
    --component-home ~/.eshu/components \
    --version 0.1.0 \
    --kind "gcp_${kind}" \
    --schema-version 1.0.0 \
    --scope gcp \
    --expires-in 2160h
done
eshu component grant dev.eshu.collector.gcp-cloud \
  --component-home ~/.eshu/components \
  --version 0.1.0 \
  --kind gcp_cloud_resource \
  --schema-version 1.1.0 \
  --scope gcp \
  --expires-in 2160h
# Plus one grant per mirror kind above once the §open-item decision lands.
eshu component grants --component-home ~/.eshu/components \
  --producer dev.eshu.collector.gcp-cloud
```

Grant-first-then-install: authorize before activating the replacement, which
is the order cutover needs.

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
- [ ] Mirror-kind authority decision recorded (open item above).
- [ ] Parity proof green on the fixture pack (`#4047` gate input).
- [ ] Rollback rehearsed ([ROLLBACK_DRAFT](ROLLBACK_DRAFT.md)).
- [ ] Expiry/rotation owner and alert recorded before issuance.
