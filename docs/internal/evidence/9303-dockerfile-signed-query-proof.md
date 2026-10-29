# Signed Dockerfile download query proof (#9303)

The Dockerfile parser records URL `ADD`, `curl`, and `wget` declarations as
install evidence. A signed URL can grant access through its query string, even
when the query is omitted from the retained install name. AWS uses
`X-Amz-Signature`, Google Cloud uses `X-Goog-Signature`, and Azure Storage SAS
uses `sig` ([AWS](https://docs.aws.amazon.com/prescriptive-guidance/latest/presigned-url-best-practices/logging-interactions.html),
[Google Cloud](https://docs.cloud.google.com/storage/docs/access-control/signed-urls),
[Azure](https://learn.microsoft.com/en-us/rest/api/storageservices/create-service-sas)).

## Accuracy and sink proof

Before the fix, `go test ./internal/parser/dockerfile -run
'^TestInstallEvidenceDropsSignedDownloadQueries$' -count=1` failed for ten
AWS, Google Cloud, and Azure query variants: each emitted one install and
counted zero credential drops. The collector regression also failed because
signed downloads reached `dockerfile_install_names`. After the fix, both tests
passed. The parser test covers case variants, single and double percent
encoding, malformed escapes, and ordinary `mirror` and `design` keys. The
collector test checks persisted `DockerfileInstall` entities and content
reference names. Unparsed diagnostics keep only bounded reasons, never query
values. Source content facts still contain the original Dockerfile body by
their existing contract.

A follow-up parser regression failed for a signed pip index option, a direct
pip URL, and signed ARG/ENV aliases: each counted zero drops. The same URL
credential classifier now serves download rows, option values, package words,
and variable taint. A variable-only `curl $DOWNLOAD_URL` passes through the
bounded expansion path; the test keeps a safe URL and reports an unresolved
name without creating a row. These tests passed after the shared guard change.

## Fixed-scope performance verification

The quiet-host window was 2026-10-28 23:04:23–23:04:33 UTC, within the
declared 23:11:14 deadline. Two baseline/head rounds each exited zero with
five samples per case. Baseline was
`90e9b7c7d7da6e51789e832e40b8a9d21216c31f`; candidate was
`dc4845418fca98c4ee405bc29d78a65acea18c5a`. The temporary benchmark
harness was byte-identical in both worktrees (SHA-256
`dc939ef82fe2ab64ee950a4f14e7c5fee561ac839643bdd8e1694236607c0890`).
The changed `install.go` SHA-256 values were
`3c8fcd3a0de12ba16f2ec79f9c27de6f8e8f81f1bb95d2f986b763970ef02b60`
and `3bb2d593e48be6706e07f3b7c94afa888751fb1b27a61fa0f18cc4634dd23b39`.
The five ecosystem fixture files were unchanged between these commits.

The command in each worktree was
`GOMAXPROCS=4 go test ./internal/parser/dockerfile -run '^$' -bench
'BenchmarkInstallQueryBoundary|BenchmarkInstallEvidenceEcosystemsDockerfiles'
-benchtime=10000x -count=5 -benchmem`. The temporary harness called
`safeDownload` and `RuntimeMetadata` on ordinary, existing-token, AWS
signature, encoded Google Cloud signature, and Azure SAS URL cases. Its
source and raw output are retained in `/tmp/eshu-9303-query-bench_test.go`
and `/tmp/eshu-9303-query-{base1,head1,base2,head2}.log` on the validation
host.

| Same-outcome case | Baseline round 1 → candidate | Baseline round 2 → candidate |
| --- | ---: | ---: |
| Ordinary query, full parser | 2,644 → 2,549 ns/op | 2,695 → 2,510 ns/op |
| Existing `token` query, full parser | 2,592 → 2,516 ns/op | 2,604 → 2,562 ns/op |
| Five ecosystem Dockerfiles, full parser | 23,860 → 24,278 ns/op | 24,061 → 23,808 ns/op |

The five-file differences were +418 ns (+1.8%) and −253 ns (−1.1%) per
pass. The provisional diagnostic threshold declared before timing was a
same-outcome increase greater than 10% or 1 microsecond per Dockerfile;
neither round reached it. Signed cases intentionally change outcome from an
install row to a counted credential drop, so their before/after times are not
interpreted as a speedup. Candidate `safeDownload` medians were 346–351 ns
for AWS, 553–555 ns for encoded Google Cloud, and 287–288 ns for Azure.

The first comparison measured the signed-query guard before the shared
classifier and variable-only URL change. A second quiet-host comparison from
2026-10-28 23:38:13–23:38:24 UTC measured that final delta, before its
declared 23:45:02 deadline. Baseline was
`96708ac14cad0f38e98f1e2c85a50fcaf2cfa57d`; candidate was
`116ac19e9ebf8f79a1512502d4edd789f63fd9d5`. The identical temporary
option benchmark harness had SHA-256
`6f14132344ade81fd14914f415e53e8b91b1f2697f621db0d316d5334ed31497`.
The changed `install.go` hashes were
`3bb2d593e48be6706e07f3b7c94afa888751fb1b27a61fa0f18cc4634dd23b39`
and `48bce0f200578e584c90afd67dbec9cdd2b0475ed43d548f96f3049832b7563a`;
`install_security.go` hashes were
`38fe5f0420e91ceea4f03a7434c6719750ac91e5c1e926eec344990e78c09496`
and `1140398784ef519af2d6d23527cef18ac1e4606099be5b794a1f6565accbfae9`.

Both base/head rounds used five samples per case with
`GOMAXPROCS=4 go test ./internal/parser/dockerfile -run '^$' -bench
'BenchmarkInstallOptionBoundary|BenchmarkInstallEvidenceEcosystemsDockerfiles'
-benchtime=10000x -count=5 -benchmem`. The identical temporary harness and
raw output are retained on the validation host as
`/tmp/eshu-9303-option-bench_test.go` and
`/tmp/eshu-9303-option-{base1,head1,base2,head2}.log`.

| Same-outcome case | Baseline round 1 → candidate | Baseline round 2 → candidate |
| --- | ---: | ---: |
| Ordinary pip index URL | 5,089 → 5,304 ns/op | 5,120 → 5,532 ns/op |
| Safe ADD URL alias | 5,731 → 5,828 ns/op | 5,590 → 5,877 ns/op |
| Five ecosystem Dockerfiles | 24,071 → 24,285 ns/op | 24,165 → 24,349 ns/op |

The ordinary option increases were 215–412 ns (+4.2–8.0%) and one
allocation. The safe alias increases were 97–287 ns (+1.7–5.1%) and one
allocation. The five-file increases were 184–214 ns (+0.8–0.9%) per pass,
with allocation count unchanged at 374. The provisional diagnostic flag was
a same-outcome increase above 10% or 1 microsecond per Dockerfile; no case
crossed it. Signed option and variable-only signed `curl` cases change
credential counts, so their times are reported only as candidate absolute
costs (about 6.74–6.75 and 5.99–6.00 microseconds respectively).

The broader #9303 parser contribution still has the separately recorded
+16–20% five-fixture and +95–101% repeated deep-parent costs. These local
measurements do not establish an end-to-end or production-corpus budget
acceptance.
