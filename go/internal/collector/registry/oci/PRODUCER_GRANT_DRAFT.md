# Producer Grant DRAFT (`#8861`, `#6709`)

**DRAFT — not issued, not stored, not enforced.** Records the intended
first-party delegation for the extracted `oci_registry` producer so review
can approve the exact kind/version/scope binding before cutover. Core retains
fact admission, canonical identity, projection, persistence, and retraction;
this grant authorizes emission only.

## Proposed grant identity

- Producer: `dev.eshu.collector.oci-registry`
- Version: `0.1.0` (collector artifact version at cutover)
- Scope: `oci_registry`
- Kinds (`1.0.0` each, `chain/oci_registry.go:30-43`):
  `oci_registry.repository`, `oci_registry.image_tag_observation`,
  `oci_registry.image_manifest`, `oci_registry.image_index`,
  `oci_registry.image_descriptor`, `oci_registry.image_referrer`,
  `oci_registry.warning`
- Lifetime: bounded `--expires-in` (rotation expected; no non-expiring grants)

## Issuance (operator commands at cutover)

One grant per core-owned kind (the CLI binds one kind per grant):

```bash
for kind in repository image_tag_observation image_manifest image_index \
    image_descriptor image_referrer warning; do
  eshu component grant dev.eshu.collector.oci-registry \
    --component-home ~/.eshu/components \
    --version 0.1.0 \
    --kind "oci_registry.${kind}" \
    --schema-version 1.0.0 \
    --scope oci_registry \
    --expires-in 2160h
done
eshu component grants --component-home ~/.eshu/components \
  --producer dev.eshu.collector.oci-registry
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
- [ ] Parity proof green on the fixture pack (`#4047` gate input).
- [ ] Rollback rehearsed ([ROLLBACK_DRAFT](ROLLBACK_DRAFT.md)).
- [ ] Expiry/rotation owner and alert recorded before issuance.
