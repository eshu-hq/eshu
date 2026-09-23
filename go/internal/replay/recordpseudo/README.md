# recordpseudo

Record-mode identifier pseudonymization for replay cassettes (#6965 Phase 3).
The engine sits between a live collector and `replay/recorder` and rewrites
every identifier into a keyed, structure-preserving pseudonym so a recording
of a real estate can be committed as a cassette.

## Purpose

A cassette carries synthetic values only, and the private-data gate
(`scripts/verify-cassette-author.sh`) enforces it. A recording is full of
account ids, ARNs, names, hostnames and addresses. Rather than scrubbing the
output, this package rewrites the facts before the recorder sees them, in a
way the reducers cannot tell apart from a synthetic corpus: the same reducer
extractors produce the same node, edge and decision counts on the
pseudonymized cassette as on the raw one (`TestAWSCorpusShapePreserved`).

## Ownership boundary

- Owns: the `Key`, the `Class` shapes, the learning and substitution engine,
  `Wrap` (the `collector.Source` wrapper), `Report`, and `Verify`.
- Does not own: which field has which class. That table is
  collector-owned (`go/internal/collector/awscloud/recordpolicy` for AWS) so
  the engine stays collector-neutral.
- Does not own: canonical serialization or the cassette format
  (`replay`, `replay/cassette`), nor when to record (`replay/recorder`, the
  collector commands).

## How it works

`Wrap(inner, key, policy)` returns a `Source` whose first `Next` drains the
inner source completely, then:

1. **learn** -- walks scope metadata and every payload with the policy and
   records a pseudonym for each classified string (pass 1 over all
   generations, so a token first seen in a late scope still rewrites an
   early one);
2. **rewrite** -- substitutes every learned token, longest first on
   alphanumeric boundaries, in every payload string, scope metadata value
   and the composite envelope fields; unclassified and opaque payload and
   metadata fields are replaced wholesale and their paths reported.

Learning visits map keys in sorted order and the dictionary gives classes
an explicit precedence (structured shapes beat tag values), so the output
never depends on map iteration and a token that is both a name and a tag
value is a name everywhere.

Payload fields and scope metadata are classified per key. The composite
envelope fields -- `scope_id`, `partition_key`, `stable_fact_key`,
`source_record_id`, `source_uri` -- are substitution-only **by design**:
the collector builds them from tokens that also appear in classified
fields (account, region, service, resource ids), from one-way hashes
(`facts.StableID`) or from structural URI text, and a table keyed by field
name has nothing to classify them by. `Verify`'s shape scan over the
canonical bytes is the belt for them, and a composite that carried an
unlearned name would be a collector-side contract to fix, not a policy
entry.

Pseudonym material is `HMAC-SHA256(key, "recordpseudo/v1\0" + raw)`. The
class is not in the input, only in the output shape, so an account seen as
`account_id` by the AWS collector and as an ECR host label by the OCI
collector gets one pseudonym and their join survives.

| class | shape |
| --- | --- |
| account | `0000` + 8 decimal digits (stays 12 numeric digits) |
| ARN | partition, service, region, leading type token and numeric qualifiers kept; account and every other component pseudonymized; `arn:aws:iam::aws:policy/...` kept whole |
| resource name | `n` + 11 hex |
| AWS-issued id | prefix kept, hex run replaced with HMAC hex of equal length; 32-hex ids stay 32 hex |
| ECR host / image ref | `<pseudo-account>.dkr.ecr.<region>.amazonaws.com/<pseudo-repo>[:tag\|@digest]` |
| other hostname | each customer label `h` + 10 hex, public suffix collapsed to `.example`; AWS-owned suffixes (`amazonaws.com`, `on.aws`, `cloudfront.net`) keep their structural tail and region; `*` and a trailing dot kept |
| IPv4 | HMAC slot in RFC 5737 (762 slots), linear probing on collision, collisions counted; loopback, unspecified and documentation addresses kept |
| IPv6 | `2001:db8:` + 96 bits of HMAC |
| CIDR | network address as above, prefix length kept; `0.0.0.0/0` and `::/0` kept |
| tag value | `t` + 11 hex (well-known tag keys such as `Name` kept, other keys pseudonymized) |
| email | `<11 hex>@example.com` |
| opaque / unclassified | `o` + 11 hex, path reported |

Declared limits:

- two private CIDRs lose their overlap relation;
- IPv4 pseudonyms of the linear-probed minority depend on learning order
  across separate recordings (no join reads an address today);
- names lose readability;
- a customer host under an AWS-owned suffix (an ELB DNS name, an RDS or
  OpenSearch endpoint, a Route53 alias target) becomes
  `h<hex>.<region>.<service>.amazonaws.com`, and the private-data gate has
  no allow row for that form yet, so `Verify` refuses the whole recording
  -- fail closed, not a leak -- until a reviewed row exists;
- Keep-class free text (`environment`, `tag`, `version`, `engine`,
  `status`, `device_name`, ...) is written verbatim when the value is not
  also learned from a classified field, and `Verify` carries no
  organisation-identifier list, so an org string in an environment name or
  an image tag reaches the file. Run the gate with
  `ESHU_PRIVATE_IDENTIFIERS_FILE` set before committing a recording; that
  alternative is the check for this residual.

## Verify

`Verify(canonical, produced)` is the record-time membership belt. It scans
the canonical bytes with the gate's alternatives (ipv4, nodeip, ipv6,
account12, arn, hostname) and refuses any candidate that is neither a
documented safe form nor a pseudonym the run produced. The reserved
`0000xxxxxxxx` form is admitted only when this run minted it, which closes
the residual of a raw account that happens to start with `0000`. The gate's
identifier alternative has no list here. The allow forms mirror
`scripts/lib/cassette_private_data_pattern.sh`; `TestVerifyAgreesWithGateOnCommittedCorpus`
holds the two together.

## Exported surface

- `Key`, `NewKey`, `Key.Fingerprint`, `MinKeyBytes`
- `Class` and its constants, `Policy`, `Policy.Validate`, `Policy.Clone`, `Config`
- `Wrap`, `Source.Next`, `Report`, `Report.LogAttrs`, `Set`
- `Verify`

## Telemetry

No metrics: recording is a one-shot CLI run. The `Report` is what the
collector command logs as `collector.record.pseudonymized` (key fingerprint,
scope/fact/token counts, learned tokens per class, opaque and unclassified
paths, IPv4 collision count). No value -- raw or pseudonymized -- is ever
logged, returned in an error, or formatted by this package.

## Validation

```bash
cd go && go test ./internal/replay/recordpseudo ./internal/replay/recorder -count=1
bash scripts/test-verify-cassette-author.sh   # the gate the allow forms mirror
```
