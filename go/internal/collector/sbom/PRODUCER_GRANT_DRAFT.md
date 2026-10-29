# Producer Grant — ISSUED (`#8865`, `#6709`)

**ISSUED — stored and enforced.** Records the first-party delegation for the
extracted `sbom_attestation` producer with the exact kind/version/scope
binding review approved before cutover. Core retains fact admission,
canonical identity, projection, persistence, and retraction; this grant
authorizes emission only.

## Grant identity (as issued)

- Producer: `dev.eshu.collector.sbom-attestation`
- Version: `0.1.1` (deployed collector artifact version)
- Scope: `sbom_attestation`
- Expires: `2027-10-29T16:05:37Z` (observed read-only from the QA
  component-home listing; task shorthand `2027-10-28` was one day early —
  issuance instant was 2026-10-29)
- Kinds (`1.1.0` each, `chain/sbom_attestation.go:50-59`):
  `sbom.document`, `sbom.component`, `sbom.dependency_relationship`,
  `sbom.external_reference`, `attestation.statement`,
  `attestation.slsa_provenance`, `attestation.signature_verification`,
  `sbom.warning`
- Lifetime: bounded `--expires-in` (rotation expected; no non-expiring grants)

The grant covers all 8 kinds the collector actually emits (proven by the
parity pack). It was issued against the reconciled producer record, which the
producer-record lane (`#8865`) widened from 5 kinds to all 8
([EXTRACTION_PREP](EXTRACTION_PREP.md) §2); the same three document-path
kinds (`sbom.document`, `sbom.component`, `sbom.warning`) remain additionally
bound to `scanner-worker` as `core_owned` until the scanner-worker cutover.

## Issuance (as issued)

One grant per core-owned kind (the CLI binds one kind per grant):

```bash
for kind in document component dependency_relationship external_reference warning; do
  eshu component grant dev.eshu.collector.sbom-attestation \
    --component-home ~/.eshu/components \
    --version 0.1.1 \
    --kind "sbom.${kind}" \
    --schema-version 1.1.0 \
    --scope sbom_attestation \
    --expires-in 2160h
done
for kind in statement slsa_provenance signature_verification; do
  eshu component grant dev.eshu.collector.sbom-attestation \
    --component-home ~/.eshu/components \
    --version 0.1.1 \
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

- [x] Producer record reconciled to all 8 emitted kinds (widened in the
  producer-record lane, `#8865`; `scanner-worker` dual-emits 3 kinds until
  its cutover).
- [ ] `#6710` template repo exists; manifest version equals grant version.
- [ ] Parity proof green on the fixture pack (`#4047` gate input).
- [ ] Rollback rehearsed ([ROLLBACK_DRAFT](ROLLBACK_DRAFT.md)).
- [ ] Expiry/rotation owner and alert recorded before issuance.
