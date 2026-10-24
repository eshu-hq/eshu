# Producer Grant DRAFT (`#8865`, `#6709`)

**DRAFT — not issued, not stored, not enforced.** Records the intended
first-party delegation for the extracted `sbom_attestation` producer so review
can approve the exact kind/version/scope binding before cutover. Core retains
fact admission, canonical identity, projection, persistence, and retraction;
this grant authorizes emission only.

## Proposed grant identity

- Producer: `dev.eshu.collector.sbom-attestation`
- Version: `0.1.0` (collector artifact version at cutover)
- Scope: `sbom_attestation`
- Kinds (`1.1.0` each, `chain/sbom_attestation.go:50-59`):
  `sbom.document`, `sbom.component`, `sbom.dependency_relationship`,
  `sbom.external_reference`, `attestation.statement`,
  `attestation.slsa_provenance`, `attestation.signature_verification`,
  `sbom.warning`
- Lifetime: bounded `--expires-in` (rotation expected; no non-expiring grants)

The draft covers all 8 kinds the collector actually emits (proven by the
parity pack). This deliberately exceeds the 5 kinds the current producer
record binds to `collector-sbom-attestation`
([EXTRACTION_PREP](EXTRACTION_PREP.md) §2): the owner must reconcile the
record — either widen it to all 8 or split `sbom.document`,
`sbom.component`, `sbom.warning` authority with `scanner-worker` — before
issuance. No grant issues against an unreconciled record.

## Issuance (operator commands at cutover)

One grant per core-owned kind (the CLI binds one kind per grant):

```bash
for kind in document component dependency_relationship external_reference warning; do
  eshu component grant dev.eshu.collector.sbom-attestation \
    --component-home ~/.eshu/components \
    --version 0.1.0 \
    --kind "sbom.${kind}" \
    --schema-version 1.1.0 \
    --scope sbom_attestation \
    --expires-in 2160h
done
for kind in statement slsa_provenance signature_verification; do
  eshu component grant dev.eshu.collector.sbom-attestation \
    --component-home ~/.eshu/components \
    --version 0.1.0 \
    --kind "attestation.${kind}" \
    --schema-version 1.1.0 \
    --scope sbom_attestation \
    --expires-in 2160h
done
eshu component grants --component-home ~/.eshu/components \
  --producer dev.eshu.collector.sbom-attestation
```

Grant-first-then-install: authorize before activating the replacement, which
is the order cutover needs.

## Enforcement (already core-owned, `#6709`)

- Admit only when producer identity, version, kind, schema version, and scope
  all match a live, unexpired, unrevoked stored grant; anything else fails
  closed at install, activation, and every emission.
- The check must accept both document-path (`sbom_document`) and
  attestation-path (`sbom_attestation`) collector kinds as emissions of this
  one producer; keying on a single collector-kind string would falsely
  reject half the fact bundle (see [SHIM_MAP](SHIM_MAP.md) §2).
- Registry/document fetch failures deny core-owned kinds rather than emitting
  under unknown authorization.
- Revocation (`component revoke-grant …`) fails the next emission terminal;
  absent-grant revocation returns `grant_not_found`, never silent success.

## First-party note

First-party status changes packaging trust, not authority: the extracted
producer still emits through the grant like any external producer. Delegation
does not transfer canonical ownership — no graph edges, no identity choice,
no queue or storage access. Parser output stays `reported` confidence with
blank `verification_status`; only the core-owned attachment reducer promotes.

## Pre-issuance checklist

- [ ] Producer record reconciled to all 8 emitted kinds (or formally split
  with `scanner-worker`).
- [ ] `#6710` template repo exists; manifest version equals grant version.
- [ ] Parity proof green on the fixture pack (`#4047` gate input).
- [ ] Rollback rehearsed ([ROLLBACK_DRAFT](ROLLBACK_DRAFT.md)).
- [ ] Expiry/rotation owner and alert recorded before issuance.
