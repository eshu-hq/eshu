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
(no join reads an address today); names lose readability; an AWS service
principal hostname (`*.amazonaws.com`) has no gate allow row yet, so a
recording carrying one is refused until that row is reviewed.

## Runtime impact

Record mode is opt-in and one-shot. The claimed-live, fixture and cassette
paths do not construct the pseudonymizer: `buildClaimedService` is
unchanged, `recorder.Run` only wraps the source when `Options.Pseudonymize`
is set, and `recordpseudo.Verify` adds one linear scan over the canonical
bytes of a fixture-sized document at the end of a record run.

No-Regression Evidence: no live ingest, reducer, projector or query path changes. The touched runtime files are the record-mode branch in `cmd/collector-aws-cloud/main.go` (taken only for `-mode=record`, before Postgres is opened), the key loader in `config.go` (called only from `runRecord`), `awsruntime/record_source.go` (constructed only by `buildRecordSource`) and the recorder's `Verify` call, which runs once per record run over the canonical bytes (68-file corpus, 27 KB awscloud cassette: `go test ./internal/replay/recordpseudo -run TestAWSCorpus -count=1` completes in well under a second including two full record passes). `go test ./cmd/collector-aws-cloud ./internal/collector/awscloud/awsruntime -count=1` passes with the existing claimed-live and fixture tests untouched.

Observability Evidence: the record run logs `collector.record.started` (path, key_fingerprint), `collector.record.pseudonymized` (scopes, facts, tokens, learned_by_class, opaque_paths, unclassified_paths, ipv4_collisions, key_fingerprint) and `collector.record.completed`; `TestRecordCassetteIsGateCleanAndLogsNoValue` decodes the pseudonymized event and asserts every field is present and that neither the key nor any raw value appears in the log. The scans record mode drives keep emitting `eshu_dp_aws_scan_duration_seconds`, `eshu_dp_aws_resources_emitted_total` and `eshu_dp_aws_relationships_emitted_total` from their unmoved awsruntime call sites; no metric was added because a one-shot CLI run has no steady state to alert on.
