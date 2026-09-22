---
name: eshu-code-review
description: Produces the push/PR/merge review verdict on an Eshu diff — proof-tier selection, the five review passes, severity findings, merge-bar readiness. Not for closing GitHub threads (resolve-review-threads).
---

# Eshu Code Review

Review the final work product against its requirements and evidence. Author
confidence, local memory, and file type do not establish safety. Process wording
can authorize consequential mistakes; review it as carefully as executable code.
Load the project skills whose contracts match the diff, using root skill routing.

## Review Workflow

1. Bind the review to the intended base/head, branch, target, acceptance criteria,
   changed files, proof actually run, and open findings. Build the bounded
   [review packet](references/review-packet.md) for a separate reviewer or a
   self-review; never substitute chat history for it.
2. Map the changed flow, owners, consumers, invariants, and failure boundaries.
   Use the full-picture checklist in [cold-review-probes.md](references/cold-review-probes.md).
   Explain which runtime, concurrency, rollback, and operator concerns apply;
   group excluded concerns with a concrete reason. Missing applicable context
   is a P1 proof failure, or P0 if it risks private data, truth, deadlock, or main.
3. Select exactly one [proof tier](references/proof-tiers.md). Explain why its
   actual evidence covers every in-scope claim. A weaker test cannot substitute
   for required backend, scaled, or full-corpus proof.
4. Perform all five [review passes](references/review-passes.md): scope,
   correctness, performance, reliability/security/workflow, and hostile read.
   Load [runtime-surfaces.md](references/runtime-surfaces.md) only for the
   runtime, graph, query, capability, or performance surfaces it covers.
5. Apply every relevant adversarial probe from
   [cold-review-probes.md](references/cold-review-probes.md), checking the
   production subject rather than just a helper. Check
   [failure-classes.md](references/failure-classes.md), generated-artifact and
   private-data/AI-attribution scans, and contradictions between passes.
6. For an existing PR, collect [live GitHub truth](references/github-truth.md),
   including review bodies, issue comments, inline threads, checks, and current
   head/base. Before first PR creation record `no PR exists yet`; collect live
   truth immediately after creation and re-review any new findings or drift.
7. Record findings and readiness using [verdict.md](references/verdict.md) and
   the [merge bar](references/merge-bar.md). Keep every finding's identity,
   severity, confidence, disposition, evidence location, violated contract, and
   closing verification through subsequent rounds.

A separate reviewer works read-only until the verdict is delivered. Use a
separate context when delegation is available and authorized. If it is unavailable
or the user explicitly requests self-review, name that mode and limitation. An
external-review replacement has additional independence requirements in
`eshu-issue-driver`; author-side review does not satisfy that second review.

This skill runs at **Workhorse tier**; see
[default model bindings by tier](../../../docs/internal/agent-orchestration.md#default-model-bindings-by-tier).
A Deep-tier orchestrator dispatches it to the reviewer role rather than
reviewing inline. The role carries the model binding -- `review-eshu` on Claude
Code and opencode, a review profile on Codex -- so this skill never names a
model, and a subagent never changes its own.

## Promotion And Evidence Reuse

After focused proof, complete a preliminary full review before push. Do not
begin promotion with any P0, P1, or blocking P2 finding. Fix those, rerun
affected proof, and repeat the full review. Deferred P2 findings need the linked
issue and owner agreement required by the merge bar; P3 does not restart the loop.

Capture the clean verdict's exact inputs with `ci-gates review-attest capture`.
The order is: proof, clean preliminary review plus that capture, `make
pre-push`, `ci-gates review-attest verify`, then push. `make pre-push` is the
required floor before every push — changed-package test/lint/build/vet, the
file cap, registry-selected static gates, and docs-contradiction. For
queue/lease/claim code, schema DDL, hot-path Cypher or graph writes, reducer
projection/materialization, or a package move, the orchestrator also runs one
serialized `make pre-pr` (`make pre-pr-full` for a move) before that push —
recommended for that risk class, not required otherwise; subagents never run
it themselves. After preflight, `ci-gates review-attest verify` replaces a
second full semantic review only when the receipt matches. Any changed base,
commit, tree, worktree, submodule, PR claim, review packet, or verdict
invalidates it: repeat affected proof and full review, then capture a new
receipt. Do not edit between verified attestation and push. This receipt
reuses semantic review; it does not waive independent review, current GitHub
state, CI, or authorization. CI's `required-gates-complete` (with
`go-core-complete` and `go-race-complete`) is the actual blocking authority
once pushed — nothing merges while it is red, and reproduce only the failing
gate locally rather than re-running everything.

## Reporting

Use the output template in [cold-review-probes.md](references/cold-review-probes.md)
as a checklist, scaling prose to the diff. A compact verdict may group clean
passes and excluded surfaces, but must record all five passes, full-picture and
proof-tier rationale, cross-pass comparison, applicable probe results, scans,
GitHub state or no-PR disposition, evidence, finding counts/dispositions, target
readiness, and stale-verdict conditions. Expand only where needed to assess a
finding or proof gap. Do not manufacture findings to fill a template.
