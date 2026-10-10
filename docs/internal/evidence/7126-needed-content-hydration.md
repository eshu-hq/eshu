# Needed-content hydration: proof (#7126, PR 1)

The repository story's `deployment_artifacts` stage read the content of up to
50 config candidates per related repository, one `GetFileContent` statement per
file. This change reads content only for files an extractor consumes, and keeps
the extractor output identical. Batching, a parse cache, ingest-time extraction
and request budgets are separate changes and are not in this PR.

## What is read now

For one repository, in order:

1. Select the hydration set as before: the first 50 candidates in
   `relative_path` order that carry no content yet (`selectHydrationIndexes`).
2. Fetch content for the files `configArtifactReadsContent` accepts:
   kustomization files, Docker Compose artifacts, `terragrunt.hcl`/`.tf`/`.hcl`,
   and a conventional Ansible playbook name that the path rules leave to the
   content heuristic. Each selector is the function the matching extractor
   branches on.
3. For the other selected files, issue one `NonBlankRepoFilePaths` statement
   per repository. The loader drops a blank file before building, and every
   path-only row (var files, Ansible inventory, vars, role and task paths) needs
   a non-blank file, so non-blankness is all the extractors need from them.
4. Fetch the resource files the kustomizations reference. They are named by
   `collectKustomizeResourceFiles`, the walk's own function, run over the same
   index the extractor builds.

## Non-blank statement

```sql
SELECT relative_path FROM content_files
WHERE repo_id = $1 AND relative_path = ANY($2::text[])
  AND btrim(content, $3::text) <> ''
```

`$3` is every rune with `unicode.IsSpace`, the set `strings.TrimSpace` trims
(25 code points), built by enumeration so it follows the Go toolchain. What
PostgreSQL does differently on the default paths, measured on PostgreSQL 18.6
(UTF8, `en_US.UTF-8`, and a `C` locale database):

| Way to test blank | Whitespace it recognises | Differs from Go |
|---|---|---|
| `btrim(content)` | U+0020 only | tab, newline, NBSP and the rest |
| regex `\s` / `[[:space:]]`, `en_US.UTF-8` | U+0009-000D, U+0020, U+00A0, U+1680, U+2000-200A, U+2028, U+2029, U+202F, U+205F, U+3000 | misses U+0085 (NEL) |
| regex `[[:space:]]`, `C` locale | U+0009-000D, U+0020 | misses U+00A0, U+0085 and every non-ASCII space |
| `btrim(content, <Go set>)` | the 25 code points of Unicode White_Space | none |

U+FEFF (byte-order mark) and U+200B (zero-width space) are not whitespace in Go
and are not in the set: a file holding only one is non-blank in both. The set
holds multibyte characters, so the statement assumes a UTF8 database (a
SQL_ASCII database trims byte-wise; LATIN1 errors on non-Latin content).

`ListRepoFiles` metadata cannot decide non-blankness. `line_count` is 1 for
both `"x"` and `"  "`, and `content_hash` of a whitespace-only value is
unknown to the reader. Only `line_count = 0` implies an empty body, which is one
class of the blank set, so a statement is still needed.

Plan, `content_files` seeded with the bench data (20 paths, median repository;
9 paths, 2.2 MB repository): index scan on `content_files_repo_path_pattern_idx`
with the `btrim` filter, 0.20 ms and 5.5 ms server time (the larger one is the
first, cold read including detoast).

## Equality proof

Package `repositoryartifacts`, run through the real loader
(`loadRepositoryConfigArtifactsForSources`) with the pre-change pass as the
oracle (`HydrateRepositoryCandidateFiles` over `isConfigArtifactCandidate`, then
the `TrimSpace` filter). The result is compared by SHA-256 of the marshalled
`config_paths`.

| Test | What it covers |
|---|---|
| `TestNeededContentHydrationMatchesFullHydrationOnGeneratedTrees` | 400 seeded trees: path classes of every extractor, kustomizations by file, by directory, missing, remote, padded and `./` paths, and content drawn from a poison pool that moves each extractor (so a class read in a new way meets content that matters) |
| `TestNeededContentHydrationBlankClassesMatchFullHydration` | 18 content classes (empty, space, tab, newline, CRLF, VT/FF, NBSP, NEL, U+2028, U+2029, U+3000, U+2000-200A, mixed, BOM, zero-width, BOM plus space, text, padded text) times 16 path classes |
| `TestNeededContentHydrationKustomizeShapesMatchFullHydration` | chain depth 3, cycle, missing and remote refs, JSON resource, blank resource, shared resource, non-resource extension, bases/patches/components, bare name, padded path, special characters, blank duplicate of a cleaned path |
| `TestNeededContentHydrationReferenceOutsideCapIsNotRead` | a referenced resource beyond the 50-file cap is not fetched |
| `TestNeededContentHydrationPreloadedContentIsKept` | anchor files with content already loaded |
| `TestNeededContentHydrationRowVanished...` | a row gone after the list, and gone between the blank check and the read |
| `TestNeededContentHydrationReadErrorsFailTheRepository` | a failed read of a consumed file, of a referenced resource, and of the blank check fails the load |
| `TestNonBlankRepoFilePathsMatchesTrimSpaceLive` | the statement against `TrimSpace` on every White_Space code point, runs, padded text, BOM, values of about 2 MB stored out of line (needs a disposable PostgreSQL) |
| `TestNonBlankSpaceSetMatchesGoOverEveryRuneLive` | the trim set against `unicode.IsSpace` over every code point except NUL and the surrogates |

One intended difference: a read failure on a file no extractor reads no longer
fails the repository, because that read is not issued. A load that succeeds
returns the same rows.

## Guard against a new content-reading branch

`TestConfigArtifactExtractorsIgnoreContentOfUnreadClasses` fails when the
extractors give different rows for different non-blank content on any path that
`configArtifactReadsContent` rejects. It fires only for content in
`poisonContents`: a new extractor branch must add content that moves it, or the
guard cannot see the read.

`TestConfigArtifactExtractorsIgnoreContentOfUnreadClassesSeededPair` is its
seeded-violation pair. The guard function takes the extractor as an argument.
GREEN: the real extractors pass on the clean tree. RED: a planted branch that
reads `.json` content marked `SEEDED_BRANCH` (the marker is in `poisonContents`)
goes through the same guard and is reported. The same plant was also written
into the real `BuildRepositoryConfigArtifacts` once, by hand and not committed:
both `TestConfigArtifactExtractorsIgnoreContentOfUnreadClasses` and
`TestNeededContentHydrationMatchesFullHydrationOnGeneratedTrees` went RED, and
both returned to GREEN when the plant was removed.
`TestNeededContentHydrationGuardCatchesUnplannedContentRead` is the same kind of
pair for the equality harness on one fixture.

## Mutation sweep

One edit at a time to the production files, the focused package tests run, the
file restored. Script: `mutate.py` in the proof scratch directory; results at
the PR HEAD.

| Mutant | Result | Killed by |
|---|---|---|
| nothing read by itself | killed | `...FetchesOnlyConsumedFiles`, `...PreloadedContentIsKept` |
| everything read (superset) | killed | `...FetchesOnlyConsumedFiles`, `...GuardCatches...` |
| deferred files all non-blank | killed | `...PreloadedContentIsKept`, generated trees |
| deferred files all blank | killed | `...RowVanishedBetweenBlankCheckAndRead`, generated trees |
| visible blank rule is `Content != ""` | killed | `...KustomizeShapes/blank_duplicate_of_a_cleaned_path` |
| unread non-blank file dropped at the end | killed | `...PreloadedContentIsKept`, `...FetchesOnlyConsumedFiles` |
| phase 2 result not re-checked | killed | `...RowVanishedBetweenBlankCheckAndReadIsDropped` |
| phase 2 skipped | killed | `...RowVanishedBetweenBlankCheckAndRead`, `...ReadErrors...` |
| phase 2 plans by cleaned path | killed | generated trees |
| read error swallowed | killed | `...ReadErrorsFailTheRepository` |
| blank-check error swallowed | killed | `...ReadErrorsFailTheRepository/blank_check` |
| cap off by one | killed | `TestSelectHydrationIndexesCapsAtTheHydrationLimitInListOrder` |
| Ansible content rule never reads | killed | `...IgnoreContentOfUnreadClasses`, `...FetchesOnlyConsumedFiles` |
| kustomization selector ignores `.yml` | killed | `...KustomizeShapes/bases_patches_components` |
| Compose selector ignored | killed | `...IgnoreContentOfUnreadClasses` |
| HCL selector ignored | killed | `...IgnoreContentOfUnreadClasses`, generated trees |
| SQL trims only U+0020 | killed | both live tests |
| SQL predicate inverted | killed | `...MatchesTrimSpaceLive` |
| SQL ignores the repository | killed | `...MatchesTrimSpaceLive` |
| story drops the degraded reason | killed | `...ReadFailureIsNamedNotSwallowed` |
| story drops the failure log | killed | `...ReadFailureIsNamedNotSwallowed` |
| context drops the `partial_reasons` append (`context.go`) | killed | `TestRepositoryContextDeploymentArtifactsReadFailureIsNamedNotSwallowed` |
| context drops the stage-line `failure_class` (`context.go`) | killed | `TestRepositoryContextDeploymentArtifactsReadFailureIsNamedNotSwallowed` |
| context drops the WARN report (`context.go`) | killed | `TestRepositoryContextDeploymentArtifactsReadFailureIsNamedNotSwallowed` |
| planted `SEEDED_BRANCH` branch in the real extractor | killed | `...IgnoreContentOfUnreadClasses`, generated trees |

In-process policy mutants (`TestNeededContentHydrationMutantsAreKilled`, ten
cases: each predicate branch dropped, phase 2 dropped or first-kustomization
only, and four blank-handling variants) run with the package tests.

## Performance, laptop bench

Real fenced reader, `ContentReader`, hydration and `BuildRepositoryConfigArtifacts`
against a private native PostgreSQL 18 primary and hot standby (no Docker).
CPU is `getrusage` user plus system around the story, `GOMAXPROCS=2`, seven
ABBA rounds with a discarded warm-up, host load1 15-32. The bench anchor
failed (the shipped path scaled to the pod's statement count measured 0.54 of
the pod's CPU), so only arm-against-arm comparisons hold. Arm A is the
pre-change pass. Arm F is the needed-content pass in a test-local loop; it is
not the shipped code. Arm FN is the production loader
(`LoadSharedRepositoryConfigArtifacts`) and is the "after" figure.

| Data | Arm | Statements (list + get + blank) | Content bytes | CPU-s median (min-max) | Digest |
|---|---|---|---|---|---|
| Original bench mix, 8,000 files, 82.7 MB | A | 300 + 8,000 + 0 | 82,712,468 | 3.647 (3.309-4.465) | `c4cd9f6b` |
| | F (test-local loop) | 300 + 3,042 + 276 | 29,589,516 | 3.170 (3.051-4.023) | `c4cd9f6b` |
| | FN (shipped loader) | 300 + 3,042 + 276 | 29,589,516 | 3.314 (3.071-3.620) | `c4cd9f6b` |
| Observed mix, 8,000 files, 57.4 MB | A | 300 + 8,000 + 0 | 57,410,054 | 3.023 (2.804-3.449) | `06b952e9` |
| | F (test-local loop) | 300 + 3,753 + 272 | 15,845,536 | 2.684 (2.227-2.975) | `06b952e9` |
| | FN (shipped loader) | 300 + 3,753 + 272 | 15,845,536 | 2.816 (2.384-3.072) | `06b952e9` |

Against A on medians, the shipped loader (FN) is -9.1 percent CPU on the
original mix (3.647 to 3.314) and -6.8 percent on the observed mix (3.023 to
2.816). The test-local loop (F) reads -13.1 and -11.2 percent; it is shown for
reference only. The CPU ranges overlap (observed mix: A 2.804-3.449 against FN
2.384-3.072) and the host ran at load1 15-32, so the CPU change is a small
arm-to-arm gain and not a precise figure. Statements fall 56 percent and 48
percent. Bytes fall 64 percent and 72 percent. All 21 digests per data set are
equal. The observed mix is the 141-repository response
set's class mix and size fits, drawn at the bench's 300 repositories and 8,000
files; it is not the response set. The earlier CPU bench attributed 76-79 percent of CPU to parse; parse
is unchanged by this PR, which is why the CPU gain is smaller than the
statement and byte gain. Pod CPU, the deployed API p95 and ops-qa stage seconds
are NOT_CHECKED.

## Evidence markers

Performance Evidence: baseline is the pre-change pass (arm A) on a private
native PostgreSQL 18.6 primary plus hot standby, the real fenced reader and
`ContentReader`, 300 related repositories and 8,000 hydrated files (observed
class mix, 57.4 MB). After, with the shipped loader (arm FN): 4,325
statements against 8,300 (300 lists, 3,753 reads, 272 non-blank checks), 15.8
MB against 57.4 MB, CPU median 2.816 against 3.023 CPU-s (-6.8 percent; the
original mix is 3.314 against 3.647, -9.1 percent) over seven ABBA rounds on a
host at load1 15-32 with overlapping ranges, and the same `config_paths` digest
in 21 of 21 runs (33,972 rows at the original mix). The non-blank statement is an index
scan on `content_files_repo_path_pattern_idx` with a `btrim` filter: 0.20 ms
for 20 paths and 5.5 ms for the 2.2 MB repository. The change is safe because
the plan never fetches less than an extractor reads (the differential and
guard tests), the extra statement is bounded to one per repository, and a read
the extractors need still fails the load.

Observability Evidence: the `deployment_artifacts` stage line of
`repository_query.stage_completed` carries `related_repository_count`,
`files_listed`, `files_hydrated`, `files_skipped`, `hydrated_bytes`,
`statements`, `blank_check_statements`, `list_seconds`, `hydrate_seconds`,
`parse_seconds` and `error`; the context route's
`content_infrastructure_overview` line carries the same counts. A failed load
logs `repository_query.deployment_artifacts_degraded` (WARN) with the error
text, sets an error status on the request span, and adds
`deployment_artifacts_read_degraded` to story `limitations` and context
`partial_reasons`. The reason covers a failed content or graph read. The
response keeps the artifacts from the loads that succeeded (the runtime and
workflow loaders still run after a config-load failure) and lacks the artifacts
of the load that failed. The context route has its own tests for the reason,
its absence on success, and the stage-line attributes. The new statement appears as a `postgres.query` span with
`db.operation=non_blank_repo_file_paths`. No metric instrument was added: no
existing query-stage metric family fits and the counts are log fields.

## Not in this PR

Batched reads, a parse cache keyed by `content_hash`, ingest-time extraction,
request budgets and worker changes. A response that is complete for the
consumed set but still pays 300 list statements and the HCL parse is the
state this PR leaves.
