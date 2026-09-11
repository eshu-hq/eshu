# #6525 — dead reducer doc references, and the gate that prevents recurrence

## What the branch does

On the current base, the moved-file gate and its historical-reference allowlist
have already landed. This rebased change repoints the remaining ordinary
references to paths vacated by the #6061 subpackage moves and refreshes the
evidence against current `main`.

## Historical residual-debt investigation

Commit `899f2d388` says "Repoint 40 of them across 50 files" and "Thirteen dead
paths are deliberate negative fixtures", which sums to 53 and reads as a
complete sweep. **That revision was not complete, and the arithmetic should not
be read as claiming it is.**

Re-derived over the commit's own scope (`docs/ scripts/ specs/ go/`, distinct
`go/internal/reducer/*.go` paths):

| ref | total referenced paths | dead |
|---|---|---|
| base `c74d5b6c5` | 517 | 53 |
| head | 506 | 27 |

Of the 53: 28 distinct paths were repointed, 13 are declared fixtures, 1 is an
undeclared fixture (`container_image_identity.go`, referenced only from
`scripts/test-verify-performance-evidence-inherited-marker.sh`), 1 is an
allowlisted transcript, 2 are self-test artifacts, and **13 ordinary repointable
dead paths remained at that revision**:

Grouped by why each survived, because the reason differs and an earlier revision
of this note tracked them by list position, which broke every time the list grew.

Cited from `docs/internal/design/4784-reducer-derived-fact-governance.md` and
`4786-contract-integration-matrix.md`, which that revision never opened (11):

    ci_cd_run_correlation_writer.go              -> reducer/cicdrun/
    container_image_identity_provenance.go       -> reducer/containerimage/
    container_image_identity_writer.go           -> reducer/containerimage/
    eshu_search_document_domain.go               -> reducer/eshusearch/
    eshu_search_document_writer.go               -> reducer/eshusearch/
    package_correlation_writer.go                -> reducer/packages/correlation/writer.go
    platform_materialization_writer.go           -> reducer/platformfam/
    sbom_attestation_attachment_index.go         -> reducer/sbomattest/
    sbom_attestation_attachment_writer.go        -> reducer/sbomattest/
    secrets_iam_graph_projection_extract_test.go -> reducer/secretsiam/
    secrets_iam_trust_chain_writer.go            -> reducer/secretsiam/

Cited from the package-family docs instead, so the 4784/4786 explanation does
not reach it (1):

    package_consumption_correlation.go           -> reducer/packages/correlation/consumption.go

Cited only from Go comments; that revision cleared it and had to revert (1):

    factschema_decode.go                         -> reducer/schemadecode/

The last two entries are the ones the rebase onto `9cfb05ace` added: main moved
the package-correlation family underneath this branch. Both are alive at
`c74d5b6c5` and dead at `9cfb05ace` — `git cat-file -e <ref>:<path>` confirms
each — and both are ordinary repointable references rather than fixtures.

Those categories were tallied across separate measurements and **do not
reconcile**: they sum to 58 against a base count of 53, an excess of 5. At least
one path is counted twice — a repointed path that is also a fixture, or a listed
remainder already inside the 28 — and splitting them correctly needs a full
re-run of the sweep, which is not done here. The LIST is the checkable part:
every entry can be confirmed with `git cat-file -e <base>:<path>`. Read the
category tally as approximate and the list as exact.

**`factschema_decode.go` was dead at the base too; the historical difference was
why that revision retained it.**
An earlier revision of this note called it "this branch's own doing". That was
wrong, and `git cat-file -e c74d5b6c5:go/internal/reducer/factschema_decode.go`
fails while the subpackage form resolves at that same base — so the reference
was already dead before that revision started, exactly like the other twelve.
What was actually different is that it was the one path that revision tried to
clear and had to put back; why the 4784/4786 explanation did not cover it is
stated further down this section. Commit
`899f2d388` repointed the three `factschema_decode.go` references in the Go
comments of `go/internal/relationships/gcp_evidence.go`, and `fa222cef9`
REVERTED that file. The reason is recorded in that commit: the
parser-relationship kit reasons over file PATHS rather than diffs, so any
change under `go/internal/relationships/**.go` — a comment edit included —
demands relationship `*_test.go` coverage and a relationship-mapping docs
update, neither of which is meaningful for a comment. Teaching the kit to
recognise a comment-only change was the principled fix; the current follow-up
below now carries that fix with a token-stream regression.

At the `fa222cef9` historical head that file still carried three references
(lines 28, 39 and 62) to `go/internal/reducer/factschema_decode.go`, a path that
did not exist — the live file was already
`go/internal/reducer/schemadecode/factschema_decode.go`.
Unlike the eleven grouped above, this one is NOT reachable from 4784 or 4786, so
that explanation does not cover it.

**The head row above predates this revert.** The table was measured in
`9e8939007`, which is an ancestor of `fa222cef9` (`git merge-base
--is-ancestor` confirms it), so its `27` counts the state before the three
references came back. Read the head dead-path count as one higher than the
table shows. The table is left as measured rather than silently re-stated,
because the number it reports is what that run actually produced.

**Why disclosing them mattered more than the count being wrong.** The moved-file
gate is branch-scoped: it only inspects paths the branch itself vacates. All
thirteen were already dead at the base, so the gate will never report them, and
there is no decreasing baseline ledger (the sibling
`verify-doc-citations.sh` has one) to keep them visible. Without this note the
historical sweep, guard, and commit message would each have hidden the same 13
paths.

## Follow-up: the remaining ordinary references, repointed

### Rebase refresh (2026-09-13)

The historical measurements below remain tied to their named commits. After
rebasing this branch onto `f8b0c2cf24bd555994ec2a7a9a3f71fd08d527c2`, the
issue's distinct-path scan was rerun over `docs/ scripts/ specs/ go/` with the
same `go/internal/reducer/*.go` path shape:

| tree | total referenced paths | dead |
|---|---|---|
| rebased `main` | 559 | 57 |
| this branch | 543 | 36 |

The branch resolves 21 paths from the current dead set and introduces zero new
dead paths (`comm -13` of the sorted base and branch dead sets is empty). The
lower total is expected because several replacement targets were already cited
elsewhere. As controls, `go/internal/reducer/supplychain/core/writer.go`
resolves in the branch reference set, while a synthetic
`go/internal/reducer/does_not_exist.go` remains non-resolving.

The rebase also moved several non-reducer query, projector, and collector files
cited on the same edited matrix rows. Those rows now point at their current
subpackage homes. Every added `go/*.go` and `sdk/go/*.go` path in the two design
documents and the corrected source comments resolves in the working tree; the
evidence note separately retains named historical references and one declared
synthetic negative control.

Measured at `2b01f131a` with the issue's own command (distinct
`go/internal/reducer/*.go` paths across `docs/ scripts/ specs/ go/`, each
tested with `-e`): 523 referenced paths, 53 dead. After this follow-up: 506
referenced, 31 dead, and `comm -13` of the before/after dead sets is empty, so
no reference became dead. Positive control: `supplychain/core/writer.go` is in
the referenced set and not reported dead. Negative control: `does_not_exist.go`
is reported dead.

The 53 is larger than the 13 listed above because main moved more families
after that list was written: #6600 (cloud inventory, IAM instance profile),
#6619 (package correlation) and #6636 (the supply-chain stanza, into
`supplychain/core/` with destuttered filenames). A path counts as ordinary if
any one of its references is ordinary.

| bucket at `2b01f131a` | paths | dead after |
|---|---|---|
| ordinary, fully repointed | 22 | 0 |
| ordinary refs repointed, historical refs left | 3 | 3 |
| ordinary, blocked (see below) | 1 | 1 |
| historical record only | 8 | 8 |
| negative fixture | 17 | 17 |
| allowlisted | 2 | 2 |
| **total** | **53** | **31** |

Repointed (old basename, then its home under `go/internal/reducer/`):

    ci_cd_run_correlation_writer.go          -> cicdrun/
    container_image_identity_provenance.go   -> containerimage/
    container_image_identity_writer.go       -> containerimage/
    eshu_search_document_domain.go           -> eshusearch/
    eshu_search_document_writer.go           -> eshusearch/
    platform_materialization_writer.go       -> platformfam/
    sbom_attestation_attachment_index.go     -> sbomattest/
    sbom_attestation_attachment_writer.go    -> sbomattest/
    secrets_iam_graph_projection_extract_test.go -> secretsiam/
    secrets_iam_trust_chain_writer.go        -> secretsiam/
    package_correlation_writer.go            -> packages/correlation/writer.go
    package_consumption_correlation.go       -> packages/correlation/consumption.go
    cloud_inventory_admission.go             -> cloudinventory/
    cloud_inventory_admission_writer.go      -> cloudinventory/  (Go comments only)
    iam_instance_profile_role_materialization.go -> iaminstprofile/  (scripts only)
    securityalert/security_alert_reconciliation.go        -> securityalert/reconciliation.go
    securityalert/security_alert_reconciliation_writer.go -> securityalert/reconciliation_writer.go
    supply_chain_impact_writer.go            -> supplychain/core/writer.go
    supply_chain_impact_match.go             -> supplychain/core/match.go  (design docs only)
    supply_chain_impact_finding.go           -> supplychain/core/finding.go
    supply_chain_impact_winners_maintainer.go -> supplychain/core/winners_maintainer.go
    supply_chain_suppression_decode.go       -> supplychain/core/decode.go
    supplychain/core/suppression_decode.go   -> supplychain/core/decode.go
    go_vulnerability_reachability_test.go    -> supplychain/core/go_reachability_test.go
    supply_chain_impact_reachability_test.go -> supplychain/core/reachability_test.go

Details a reviewer would otherwise have to rediscover:

- **Line suffixes on edited 4784/4786 rows were dropped, live ones included.**
  `verify-doc-citations.sh` binds LINE debt to the exact bytes of the line
  that contains it, at the branch base. Rewording a row therefore turns every
  other `file.go:N` on that row into new debt, even when the cited file still
  exists. Each raw suffix on an edited row became a file anchor, and the
  post-rebase `-update` removed 128 LINE rows (34 from 4784, 93 from 4786, 1
  from 5385) and added none.
- **Split files were followed to the content, not the filename.**
  `packages/correlation/writer.go` (a 67% rename) holds the three payload
  builders and `WriteCorrelations`, which is what the 4784/4786 rows cite. The
  `cicdrun` decode comment cited the old file only for its `payloadString`
  forwarder, so it now names `payloadcore/payload.go`, where the
  `strings.TrimSpace(fmt.Sprint(value))` it describes lives. In 5385,
  `supplyChainWorkloadIDsFromPayload` is now a one-line wrapper in
  `core/match.go`; the prefix parse the row describes is
  `payloadcore.SupplyChainWorkloadIDsFromPayload` in `payloadcore/identity.go`,
  and the row now names both.
- **`supplychain/core/suppression_decode.go` never existed.** #6636 repointed
  the 4367 schedulereplay note to a name its own destutter did not keep. It now
  names `core/decode.go`, which holds `BuildVulnerabilitySuppressions`.
- **IFA row 13.** The `IFA_FAMILY_HANDLER_GO_FILE` value and three hand-typed
  comments now name `iaminstprofile/`. Their line numbers had drifted as well,
  and were re-derived by reading the file (`FactLoader` field :57 -> :78, nil
  check :85 -> :106, extraction call :114 -> :135). Only the
  `shared_intent_lock` kill cell reads that value. This family is `table_lock`,
  so no gate's behaviour changes.
- **The prod-supply-chain-impact artifact had a false green.** Its Reproduce
  recipes ran `go test ./internal/reducer -run ...`. Since #6636 that matches
  no test and still exits 0. Both recipes and both test-file paths now name
  `supplychain/core`. The `Validation-Command` header, which records the
  deployed run, is untouched. Run at `2b01f131a` with both `-run` patterns
  combined: the old package exits 0 with `[no tests to run]` and zero
  `--- PASS` lines, and `./internal/reducer/supplychain/core` exits 0 with 11.

Historical or intentional references left in the measured set:

- `factschema_decode.go` was the one blocked ordinary path at `2b01f131a`.
  This follow-up repoints the three comments in
  `go/internal/relationships/gcp_evidence.go` and changes
  `verify-parser-relationship-kit.sh` to exempt only token-identical,
  comment-only relationship-source edits. Its regression proves a plain
  comment correction passes without manufactured relationship behavior tests,
  while an actual code change still requires the existing test and docs pair.
  Historical mentions of the old path in this evidence note remain records,
  not live pointers.
- Historical records describe the tree as it was when they were written:
  evidence notes 5237, 5238 (two), 5426, 5779, 5780, 5813 and 6309, the
  reducer's own `evidence-4633` note, and the dated
  `architecture-review-2026-07.md`. This is why three repointed paths
  (`cloud_inventory_admission_writer.go`,
  `iam_instance_profile_role_materialization.go`, `supply_chain_impact_match.go`)
  still count as dead.
- The `Reason` string for `aws_iam_instance_profile` in
  `go/internal/mcp/kind_disclosure_ledger.go` is a dated `rg` transcript
  ("round-2 re-verify (2026-07-21)"), so it is a record, not a pointer.
- Fixtures and the two allowlisted entries are deliberate, as described above.
- Before the current rebase refresh, seven distinct non-reducer paths on the
  edited 4784/4786 rows no longer resolved after the Lane B `query/` and
  `projector/` moves. The current matrix repoints all seven and also corrects
  Kubernetes, OCI-image, and security-alert consumer ownership on those edited
  rows.

## Gate cost, measured

The gate runs one full-tree `git grep` per vacated path, so cost is linear in
the size of the move set. Measured on this host:

| vacated paths | wall | user | CPU |
|---|---|---|---|
| 0 (this branch) | instant | — | — |
| 19 (`--base 392351ffd^`, the RDS/S3/EC2 move) | 15.4 s | 2.95 s | 754% |
| 294 (`--base 85f7458e1`, a 52-commit window) | 132 s | 45 s | 1051% |

`85f7458e1` is a pre-rebase object that exists only in the authoring clone —
`git branch -r --contains` finds no remote ref for it — so the 294-path row
and the 43-reference proof below are **not reproducible from a fresh clone as
written**. They are recorded as measured rather than restated against a base
that was never measured. The `392351ffd^` rows are on `main` and are the ones
a reader can re-run.

15 s for a realistic extraction PR is acceptable for a blocking gate. The
header's cheapness claim is correctly scoped to the no-move case; this table is
the number that was missing.

## Gate behaviour, proven rather than asserted

- fires correctly at repo scale: `--base 85f7458e1` -> exit 1, **43 dangling
  references**, each naming the correct repoint target
- clean on a slice already fixed: `--base 392351ffd^` -> 19 vacated, no findings
- **fails closed**: `--base 0000000000000000000000000000000000000000` ->
  exit 2, "the scan did not run" — never a silent pass. The all-zero ref is
  used deliberately so any clone reproduces it; the gate fails at the `git
  diff` before any sweep, so the control is one git call
- the self-test's five negative controls assert exit 1, so it proves failure
  and not merely success: move-and-forget, outright deletion, a move+rewrite
  git leaves unpaired, an allowlist entry leaking to another referencing file,
  and the widened-base control that makes the attribution case non-vacuous

Known scope limit, tracked in #6525: the gate matches only fully-qualified
`go/internal/...` paths, so any other spelling is invisible to it. The live
examples sit in `go/internal/exposure/sink_catalog.go`, and the spelling there is
neither repo-relative nor `go/`-prefixed — it is a `reducer/<file>.go` shorthand
inside the `Provenance:` strings. Measured at this head: seven such shorthands,
of which THREE no longer resolve because #6061 moved their targets into
subpackages —

    reducer/iam_escalation_materialization.go   -> reducer/iamescalation/
    reducer/sql_relationship_materialization.go -> reducer/sqlrelationship/
    reducer/security_group_reachability.go      -> reducer/secgroup/

three of the other four name their subpackage and resolve; the fourth,
`reducer/shell_exec_materialization.go`, is flat and resolves because that file
was never moved. Stating the shorthand
rather than "repo-relative" matters, because a reader looking for
`internal/exposure/...`-style paths in that file finds NONE — the blind spot is
wider than one alternate spelling.

Second known limit, same class: the reference scan is `git grep -F`, so a
vacated path is matched as a plain substring. A vacated `a/b/c.go` therefore
also matches a longer literal that merely starts with it, such as
`a/b/c.golden.json`. Reproduced deliberately; there are zero such pairs in the
tree today, so it reports no false positive now, and it can only ever produce
a false POSITIVE (a reference reported that is not one), never a false
negative that lets a genuinely dead reference through.

## Why the perf-evidence gate fired on this branch

Worth recording, because the demand is spurious. `is_comment_only_change()` in
`scripts/verify-performance-evidence.sh` recognises comments only at column 0 —
it strips the leading `+`/`-` without trimming whitespace. The one hot file it
flagged, `reducer_queue_readiness_sql.go`, has a diff consisting entirely of a
single **tab-indented** comment line inside a `var` block, so its own
suppression logic failed to suppress it. Ten of the eleven changed runtime files
were correctly auto-skipped.

The markers below are therefore truthful rather than ceremonial: this branch
changes no behaviour.

No-Regression Evidence: production Go changes are comment-only, including the
three corrected references in `relationships/gcp_evidence.go`; their compiled
token stream is unchanged. The parser/relationship gate now deliberately
changes only for a relationship source whose base and head token streams are
identical after safe plain-line-comment removal. The self-test proves a plain
comment correction passes without unrelated docs/tests and an actual code
change still fails without them. Directive comments, block comments, raw-string
contents, cgo preambles, added/deleted files, tool failures, and committed-vs-
worktree ambiguity remain fail-closed through the shared `cmd/token-diff`
contract. No production runtime path changes, so there is no runtime latency or
throughput surface to measure; the moved-file gate cost remains the table above.

No-Observability-Change: this change adds no runtime signal and removes none. A
dangling reference surfaces as a gate failure naming `file:line` and the vacated
path; nothing is emitted at runtime.
