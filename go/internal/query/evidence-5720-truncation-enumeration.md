# Evidence: what belongs on the consumer-truncation enumeration (#5720 round 10)

Continues `evidence-5720-provisioning-candidate-bound.md`, which carries rounds
1 through 9 of the same issue. Read that file first for how
`queryProvisioningRepositoryCandidates` got its bound and how the three
`*_truncated` flags reached the wire.

## Round 10: the enumeration had no membership rule

Four rounds closed this enumeration and four rounds were wrong. The cause was
never a bound that hid especially well. The list never said what made something
a member, so each round redrew the line and found one more thing on the far side
of it.

Round 8 is where that came apart. It admitted the hostname affinity narrowing as
"source 2b" even though that narrowing selects hostnames rather than capping how
many survive, which quietly widened the criterion to "any narrowing that loses a
reachable consumer". Under the wider reading three more steps qualified and were
absent, all of them the same shape as 2b and all upstream of it:

- `isServiceEvidenceCandidate` (`service_evidence.go`) -- a 10-extension
  whitelist plus a 12-keyword path filter, applied to every listed file before a
  single hostname is extracted. A hostname living only in `terraform/main.tf`,
  `Dockerfile`, `nginx/nginx.conf` or `.env.production` is never extracted, so a
  consumer reachable only through it never enters the merged set and the flag
  stays false.
- `exactObservedHostnameCandidates` (`service_hostname_evidence.go`) -- keeps
  only `Classification == "exact_hostname"`. Ambiguous candidates reach the
  caller as `entrypoint_candidates` but are never searched for.
- `lineLikelyContainsHostname` plus the `falsePositiveTLDs` /
  `falsePositiveSegments` / `falsePositiveConfigKeyTerminals` tables in
  `internal/contentrefs/hostnames.go`.

The fix is a stated criterion, not another entry. A step is on the numbered list
when it is a **cardinality bound**: a numeric cap on how many items survive --
a backend LIMIT or a slice trim against a constant or the caller's limit -- on a
read that feeds this set, such that a consumer repository past the cap never
enters the set. "How many", not "which ones"; "this set", not some other one.

That leaves six numbered bounds (0 through 5), moves the affinity narrowing to
its own heading -- named, unnumbered, and still ORed into the bool because it
really does drop reachable consumers -- and puts the three steps above outside
the list by the same rule rather than by nobody having noticed them. Two
previously-recorded items move under the same heading with their reason
restated: `repositorySemanticEntityLimit` (5000) and
`ContentReader.ListFrameworkRoutes` (`frameworkRouteEvidenceLimit` = 50, in
`content_reader_framework_routes.go`) are both real cardinality bounds on sets
that cannot drop a repository from any of the three arrays.
`ListFrameworkRoutes` rows land only on `ServiceQueryEvidence.FrameworkRoutes`,
which `service_query_enrichment_rows.go` reads to build `api_surface` endpoints;
nothing there feeds a hostname, a candidate, or a consumer search. Recorded so
the next round does not re-derive it.

The public reference and the OpenAPI description now describe the same split:
six cardinality bounds, one disclosed non-bound filter, and the upstream
relevance predicates named as deliberately not disclosed.

## Round 10 P1-1: the row bound is not a repository bound

`docs/public/reference/http-api/context-and-stories.md` said "Three conditions
make them true when nothing was lost." There are five, and the first one is a
correctness problem rather than a counting problem.

`queryProvisioningRepositoryCandidates` bounds **rows**. The read returns one
row per `(repo_id, relationship_type, relationship_reason)` tuple and groups
them by `repo_id` only after trimming
(`deployment_trace_support_helpers.go`). At the default 25-row bound a graph
holding 26 rows across 3 repositories sets `truncated` true, trims one row, and
still returns all 3 repositories. Nothing was dropped from any list. What the
trim cost is one entry's `relationship_reasons`.

That made three OpenAPI descriptions false. `dependents_truncated` and
`provisioning_source_chains_truncated` are 1:1 over this slice and both said
only that the returned list "may not be exhaustive". Here the list is
exhaustive and the flag is true. Both descriptions now say the flag reports
either a non-exhaustive list or clipped `relationship_types` /
`relationship_reasons` on a returned entry, and point at the reference for the
false-positive conditions.

The second missing condition: sources 0, 2, 3 and the affinity narrowing all set
`consumer_repositories_truncated` when a **hostname** was dropped, whether or
not any consumer repository referenced that hostname. The reference already
conceded exactly this for source 0 ("whether or not the files past that bound
held any hostname at all") and omitted it for the three narrowings with the same
property. The "Three conditions" sentence is gone and all five are now a list.

`TestQueryProvisioningRepositoryCandidatesTruncatesRowsNotRepositories`
(`deployment_trace_repoid_tiebreak_test.go`) pins the behavior the reference now
describes: 26 rows across 3 repositories at the default bound, asserting
`truncated` true, all 3 repositories returned, and the third repository holding
one fewer reason than the graph offered.

Mutation proof. Moving the trim from rows-before-grouping to
repositories-after-grouping -- deleting `rows = rows[:limit]` and adding
`if len(candidates) > limit { candidates = candidates[:limit] }` before the
return -- reds exactly one test in `./internal/query`, the new one, on
`gamma relationship_reasons = 24, want 23`. Reverted and green again.

## Round 10 P2-2: a $ref read error produced a complete-looking answer

`buildSpecFileResolver` collapsed a read failure and a genuinely absent file
into the same empty string:

```go
fc, err := reader.GetFileContent(ctx, repoID, resolved)
if err != nil || fc == nil {
    return ""
}
```

`resolveOpenAPIPathRefs` and `resolveOpenAPIPathItemRefs` then returned or
continued on that empty string. A transient Postgres error resolving
`./paths/index.yaml` therefore yielded a spec with fewer `servers` entries,
fewer derived hostnames, fewer cross-repository consumer searches, and
`consumer_repositories_truncated` **false** -- a complete-looking answer
produced by a failure rather than by a bound, which is the failure mode
`CLAUDE.md` bars outright.

`specFileResolver` now returns `(string, error)`; an empty string with a nil
error still means "the repository holds nothing there", and a read failure is an
error. `resolveOpenAPIPathRefs`, `resolveOpenAPIPathItemRefs` and
`extractAPISpecEvidence` propagate it, and `loadServiceQueryEvidence` returns
it. That matches what the same function already does with the identical error
from the same reader method when it hydrates a listing row, two call sites
apart. A referenced file that parses badly is still tolerated: malformed YAML in
a committed spec is repository content, not a read failure, and every other
extractor in that file reads content best-effort.

`repository_narrative_enrichment.go` holds no reader and passed a nil resolver.
It now calls `extractAPISpecEvidenceWithoutRefs`, which is error-free because
`resolveOpenAPIPathRefs` returns on a nil resolver before it can read anything.

## Round 10 P2-3: service_evidence.go split, no new file

Four files sat within 15 lines of the 500-line rule `CLAUDE.md` says to split
*before* approaching: `service_evidence.go` 487,
`deployment_trace_truncation_disclosure_test.go` 488,
`openapi_paths_impact.go` 486, `tools_ecosystem.go` 485.

A new production `.go` file would stale
`docs/public/reference/code-coverage.md`, whose `included_files` count is built
from the coverage profile and covers non-test files only. The split needs no new
file. `service_evidence_types.go` (103 lines) is the natural home for the reader
port and the plumbing that reads through it, so `serviceEvidenceReader`,
`serviceEvidenceFileLimit`, `listServiceEvidenceFiles`, `specFileResolver`,
`buildSpecFileResolver`, `openAPIRefFilePath` and the three loose-document value
accessors moved there.

Adding no file did not keep the coverage doc valid, and an earlier revision of
this paragraph claimed it did. A file reaches the coverage profile only once it
holds executable statements, so a move that changes an existing file's function
count moves the number as surely as adding a file does.
`service_evidence_types.go` held type declarations and zero functions on `main`
and was absent from the profile; the six functions moved into it put it in.
Together with `deployment_trace_story_facts.go`, the one genuinely new non-test
file, the count went from 4817 on `main` to 4819 -- two above, from one added
file. The report was regenerated rather than derived (commit `7717cc2ba3`),
because the coverage check reports "skipping" rather than failing and a stale
count therefore merges silently.

`service_evidence.go` is 443 lines after the move and after P2-2's additions;
`service_evidence_types.go` is 211. `buildSpecFileResolver` also took its
`context.Context` back to the first parameter position on the way across.

The other three files are recorded rather than split: the two OpenAPI/tool
documents are single-statement string builders where a split would cut a wire
contract in half, and the new P1-1 regression went into
`deployment_trace_repoid_tiebreak_test.go` (205 to 283) specifically to keep
`deployment_trace_truncation_disclosure_test.go` at 488.

Two files this round grew that were already past 500 before it, both previously
raised with the owner rather than filed:
`docs/public/reference/http-api/context-and-stories.md` (788 to 816 this round;
732 at the branch point, for the P1-1 conditions) and this issue's own evidence
record, which is why rounds 10 and later live in this file instead of the
round-1-to-9 one. `go/internal/query/AGENTS.md` stays at 1312: its existing
#5720 line was rewritten to carry the cross-reference, not added to.

## Round 10 P3s

- `openapi_paths_impact.go`'s `consumer_repositories_truncated` description said
  "either hostname filter applied before the cross-repository searches", which
  reads as exactly two and left source 3 unnamed while the markdown named all
  seven. It now names all three narrowings applied to the hostname set.
- `repositorySemanticEntityLimit` clips `evidence_kinds` / `sample_paths` /
  `modules` / `config_paths` inside a chain entry with no channel of its own.
  Membership was already dispositioned honestly; the enumeration now says
  plainly that the payload clipping is not, and that this is a gap in what a
  caller can tell about an entry rather than about the list.
- `ListFrameworkRoutes`'s `LIMIT 50` is recorded on the enumeration with the
  trace that shows it cannot reach the three arrays.

Round 10 also refreshed prose that the renumbering made stale: the "sources 2
and 2b" phrasing in `service_evidence_file_bound_test.go` and
`service_query_truncation_wiring_test.go`, and two drifted `:79-84` / `:146-148`
line-number citations in `deployment_trace_overflow_regression_test.go` that
this round's comment growth had already invalidated.

No-Regression Evidence: round 10 changes comments, documentation, one error
return threaded through three functions that previously discarded it, and moves
declarations between two files in the same package. No query shape, anchor,
index, row bound, or terminal cardinality changed, so the
no-measurable-regression statement recorded above holds unchanged. The one
behavior change is that a `$ref` read failure now surfaces as an error instead
of silently shortening the evidence.

No-Observability-Change: no span, metric, label, or log event was added or
altered.
