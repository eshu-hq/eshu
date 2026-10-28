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

This measures the first signed-query guard commit, before the follow-up shared
classifier and variable-only URL change. The broader #9303
parser contribution still has the separately recorded +16–20% five-fixture
and +95–101% repeated deep-parent costs. Neither measurement establishes an
end-to-end or production-corpus budget acceptance.
