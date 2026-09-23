# #6965 Phase 3: record-mode identifier pseudonymization, aws-cloud pilot

## What it guards

`collector-aws-cloud -mode=record` runs the claimed-live credential and
scanner path once over every configured `(account, region, service)` tuple
and writes a canonical cassette. A recording of a real estate carries account
ids, ARNs, names, hostnames and addresses that must never be committed, and
the Phase 2 gate (`scripts/verify-cassette-author.sh`) only catches them
after the fact. This change makes the recorder unable to write them:
`go/internal/replay/recordpseudo` rewrites every identifier into a keyed,
structure-preserving pseudonym before the recorder sees it, and
`recordpseudo.Verify` refuses the canonical bytes the gate would reject
before any file exists.

## Proof (all offline; no cloud or cluster contact)

- Design theory shim, run before the change: the committed awscloud cassette
  wrapped with the prototype layer and recorded by the real recorder passed
  the gate under the one reviewed `doc_account` extension and drove the six
  reducer extractors to the same shape as the raw cassette (design section 3).
- `go test ./internal/replay/recorder ./internal/replay/recordpseudo
  ./internal/collector/awscloud/recordpolicy ./cmd/collector-aws-cloud
  ./internal/collector/awscloud/awsruntime ./internal/envregistry -count=1`:
  all pass. `TestAWSCorpusShapePreserved` asserts the design's exact numbers
  on raw and pseudonymized envelopes -- 10 CloudResource rows, the
  resource_type multiset, 10 uids, 1 `ec2_instance_uses_ami` bare_id edge,
  1 `lambda_function_uses_image` edge, 2 USES rows (prod 1, stage 1),
  1 CAN_PERFORM exact_arn, 2 container-image identity decisions, a 22-fact /
  7-scope bijection; `TestAWSCorpusSiblingsJoin` finds the lambda image's
  oci-descriptor uid and the ec2 instance ARN in the same-dictionary rewrite
  of the ociregistry and terraformstate cassettes.
- Seeded RED/GREEN for the two guards: `TestVerifySeededRedGreen` plants one
  raw value per alternative into a clean recording and each is refused
  naming offset and alternative, never the value; the reserved `0000` form
  is admitted only when the run minted it. The gate mirror
  (`scripts/test-verify-cassette-author.sh`, exit 0) plants a `0001`
  account, an ARN and an ECR host on it (all red) and passes the reserved
  form (green); deleting the reserved allowed sample turns the hand count
  red (28 vs 29).
- RED runs found three real defects before the code was committed: a
  learnIdent/learnImageRef recursion on bare ECR hosts, a hostname
  alternative that lost `.corp` candidates because Go's RE2 has no
  lookahead, and a fail-open walker that let typed `[]string` anchors
  through untouched (fixed by JSON-normalizing payloads).
- `TestRecordSourceIsClaimedLiveWiringMinusStores`: the record source is the
  live `awsruntime.ClaimedSource` literal with the three Postgres-backed
  stores nil; `git diff` of `service.go` is empty.

## Stated limits

Two private CIDRs lose their overlap relation; IPv4 pseudonyms of the
linear-probed minority depend on learning order across separate recordings
(no join reads an address today); names lose readability. Any customer host
under an AWS-owned suffix (ELB DNS names, RDS/OpenSearch endpoints, Route53
alias targets, service principals) is pseudonymized to
`h<hex>.<region>.<service>.amazonaws.com`, a form the gate has no allow row
for, so `Verify` refuses such a recording -- fail closed -- until a reviewed
row exists. Keep-class free text (environment, version, engine, status) is
written verbatim when not learned elsewhere and `Verify` has no
organisation-identifier list; operators run the gate with
`ESHU_PRIVATE_IDENTIFIERS_FILE` before committing a recording. Customer
image tags are pseudonymized (only `latest`, pure semver and digests are
kept). One recording holds at most 762 distinct IPv4 addresses and CIDR
networks; the 763rd is `ErrIPv4Exhausted` from `Next`, and
`Report.IPv4Addresses` shows the count. A name or tag value shorter than
four characters, or a purely numeric tag value, is rewritten only as a
whole classified value or a whole ARN component, so one inside a non-ARN
composite stays raw. The composite envelope fields (`scope_id`,
`partition_key`, `stable_fact_key`, `source_record_id`, `source_uri`) are
substitution-only by design; scope metadata is classified per key like a
payload.

## Review round 2 (verdict-p3.md on 40a4ac36e)

F1 (P1): ARN type tokens are now a per-service vocabulary; unknown first
components (SNS topics, SQS queues) are learned. F2 (P1): dictionary
entries carry a class with explicit precedence and the walker learns in
sorted key order; tag values are shape-sniffed. F3 (P2): scope metadata
goes through the policy. F4: IPv6 in an ident field reaches the
documentation block; limits widened above. F6: `Key` renders as its
fingerprint under `%v`, `%+v`, `%#v` and slog. F7: `arn:aws:iam::A:root`
keeps `root`. Each fix landed RED-first (arn_vocabulary_test.go,
precedence_test.go, metadata_test.go, ident_ipv6_test.go,
key_format_test.go); `TestAWSCorpusShapePreserved` numbers unchanged.

## Review round 3 (PR #6987 at 2c467b6, 1 P0 + 4 P1 + 3 P2)

Each fix landed RED-first in its own commit with the reviewer's exact
scenario as the test; `TestAWSCorpusShapePreserved` and
`TestAWSCorpusSiblingsJoin` are unchanged throughout.

- P0, S3 key paths (`s3_key_path_test.go`): RED had
  `arn:aws:s3:::bucket/home/user/*` written with `home` and `user` raw in
  the ARN field, `resources` and the stable key; every `/` component is
  now learned, `*` and empty components stay.
- P1, short tokens (`short_token_test.go`): RED had a tag value `1`, a
  tag value `us` and a name `east` rewrite `us-east-1` in scope ids, the
  region Keep field, scope metadata, an ARN and an ECR host. Root-cause
  fix: substitution is structure-aware (ARNs by position with partition,
  service and region untouched; regions and availability zones refused by
  learn and protected in free text; free-text tokens under four characters
  or purely numeric are exact-only). Declared trade-off: such a token
  inside a non-ARN composite stays raw.
- P1, IPv4 ceiling (`ipv4_limit_test.go`): RED had no address count and a
  panic on the 763rd address; `ipv4Slot` now records `ErrIPv4Exhausted`,
  `Next` returns it wrapped with the limit, `Report.IPv4Addresses` counts.
- P1, image tags (`image_tag_test.go`): RED kept `repo:user-hotfix`
  verbatim in the reference and the `tag` field; a `ClassImageTag` keeps
  `latest`, pure semver and digests and learns every other tag to the name
  form, plus the `path:tag` composite so a tag below the length floor is
  still rewritten inside the reference.
- P1, ripgrep skips: the two gate-library tests no longer skip without
  `rg` (RED reproduced locally with rg hidden: FAIL naming exit 127, not
  SKIP); `test.yml` go-race and `macos.yml` install ripgrep (macos also a
  bash >= 4.3), the lanes that actually run these packages.
- P2, account collisions (`account_probe_internal_test.go`): the reserved
  `0000` space is linear-probed and `Report.AccountCollisions` counts,
  cached per raw account so a second mint never double-counts.
- P2, public hosts (`safe_host_internal_test.go`, `public_host_test.go`):
  `safeHost` consults the one gate-mirrored `publicHostsList`.
- P2, unlisted ARN types (`arn_unlisted_type_test.go`):
  `Report.UnlistedARNTypes` names each `service:token` miss for a service
  with a vocabulary; a service without one is never reported.

## Runtime impact

Record mode is opt-in and one-shot. The claimed-live, fixture and cassette
paths do not construct the pseudonymizer: `buildClaimedService` is
unchanged, `recorder.Run` only wraps the source when `Options.Pseudonymize`
is set, and `recordpseudo.Verify` adds one linear scan over the canonical
bytes of a fixture-sized document at the end of a record run.

No-Regression Evidence: no live ingest, reducer, projector or query path changes. The touched runtime files are the record-mode branch in `cmd/collector-aws-cloud/main.go` (taken only for `-mode=record`, before Postgres is opened), the key loader in `config.go` (called only from `runRecord`), `awsruntime/record_source.go` (constructed only by `buildRecordSource`) and the recorder's `Verify` call, which runs once per record run over the canonical bytes (68-file corpus, 27 KB awscloud cassette: `go test ./internal/replay/recordpseudo -run TestAWSCorpus -count=1` completes in well under a second including two full record passes). `go test ./cmd/collector-aws-cloud ./internal/collector/awscloud/awsruntime -count=1` passes with the existing claimed-live and fixture tests untouched.

Observability Evidence: the record run logs `collector.record.started` (path, key_fingerprint), `collector.record.pseudonymized` (scopes, facts, tokens, learned_by_class, opaque_paths, unclassified_paths, ipv4_collisions, ipv4_addresses, account_collisions, unlisted_arn_types, unlisted_arn_type_count, key_fingerprint) and `collector.record.completed`; `TestRecordCassetteIsGateCleanAndLogsNoValue` decodes the pseudonymized event and asserts every field is present and that neither the key nor any raw value appears in the log. The scans record mode drives keep emitting `eshu_dp_aws_scan_duration_seconds`, `eshu_dp_aws_resources_emitted_total` and `eshu_dp_aws_relationships_emitted_total` from their unmoved awsruntime call sites; no metric was added because a one-shot CLI run has no steady state to alert on.
