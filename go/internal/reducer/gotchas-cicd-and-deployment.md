# Reducer Gotchas — CI/CD Generations And Deployment Mapping

Split from `README.md` (issue #6061), following the same split
`gotchas-supply-chain-and-vulnerabilities.md` and
`gotchas-correlation-queue-and-graph-security.md` already made: the README had
crept one line over the repository's 500-line Markdown cap, which fails the
blocking `markdown-file-cap` gate for every branch, not just the one that
pushed it over.

The invariants below govern how a CI/CD generation is admitted, how git workflow
evidence crosses scopes, and when `deployment_mapping` may reopen a closed
phase. The package-wide invariants — idempotency, generation supersession, and
the phase-publication/graph-write atomicity gap — stay in `README.md`.

- **Artifact-only CI/CD generations are patches** — a generation containing a
  `ci.artifact` but no `ci.run` rebuilds the complete bounded run snapshot from
  the newest older generation containing `ci.run`, then unions exact keys from
  current live artifacts and overlays current-generation control rows. It does not
  depend on an immediately preceding derived snapshot: queue supersession can
  legitimately prevent that snapshot from being published. Rebuilding the
  latest normal run window keeps its unaffected runs visible behind the
  active-generation fence without resurrecting runs that a newer normal window
  omitted. A generation containing any `ci.run` remains a normal full
  replacement. For a
  patched run, current-generation artifacts replace retained artifacts even
  when the current payload has no digest. The latest normal run generation is
  the lower bound for ancillary evidence. A live artifact for an omitted run may
  recover only that run's older `ci.run` anchor; its pre-baseline artifact,
  environment, workflow-image, deployment, trigger, and step evidence stays
  absent. Retained workflow-image evidence reloads by recovered repository and
  keeps the existing exact-commit versus repository-fallback rule inside that
  window. A payload-empty artifact tombstone uses its opaque stable key only to
  remove matching baseline or later evidence. It never seeds a run, and a key
  already absent from the window is a no-op. A valid tombstone is control
  evidence and is not quarantined as a malformed live artifact. Current
  non-artifact facts replace retained history
  only by the exact `(fact kind, stable key)` pair, so workflow-image,
  environment, deployment, trigger, and step tombstones retract their older
  evidence without crossing fact-kind or unrelated-key boundaries. Every valid
  current tombstone is removed before typed classification; a blank identity
  fails the patch closed.
  The reducer result reports every rebuilt decision as `evaluated`; `preserved`
  remains zero because no prior derived decision is copied. Outcome totals
  describe the complete snapshot written for the target generation.
  Deployment events join by commit SHA rather than run key, so the history read
  reloads them after recovering the run and the normal classifier reselects the
  environment. The active container-image loader returns support-grain rows;
  correlation groups rows that agree on the same non-empty digest and image
  reference before deciding cardinality, preserving all support fact IDs. Two
  different image references for one digest remain ambiguous. Evidence IDs on
  unaffected rebuilt decisions are best-effort links to retained superseded
  facts. Each newer full CI/CD run window immediately rebases the patch
  baseline; after retention, an older retained-source link may no longer
  resolve.
- **Git workflow evidence crosses scopes by repository owner** — a normal or
  rebuilt CI run snapshot supplies the distinct typed `repository_id` values.
  Before deriving image references, the handler asks the fact store for active
  Git `ci.workflow_image_evidence` in the matching default and explicit-ref
  scopes. The reducer decodes those rows again, rejects foreign owners, wrong
  fact kinds, and duplicate fact IDs, and leaves malformed rows for the typed
  quarantine-aware classifier. The storage read is capped and fails closed
  rather than truncating evidence. Static workflow
  generations and direct workflow-file deletions trigger
  `container_image_identity`; its durable completion event reopens only current
  `ci_cd_run_correlation` work, so Git-before-CI and CI-before-Git activation
  orders converge without serializing either collector.
- **Exact workflow-image decisions own a separate graph assertion** — after
  the durable CI/CD decision write, the handler retracts and reprojects
  `ContainerImage-[:BUILT_FROM]->Repository` only for exact decisions whose
  correlation kind is `workflow_image` and canonical target is
  `container_image`. The evidence source is
  `reducer/ci-cd-run-correlation/workflow-image`; artifact-only exact and every
  non-exact outcome remain graph no-ops. The writer identity includes scope and
  evidence source, so this assertion coexists with the independent
  `container_image_identity` assertion for the same endpoints.
- **`deployment_mapping` requires post-Phase-3 reopen** — any domain
  consuming `resolved_relationships` needs its own post-Phase-3 reopen
  mechanism (see `queue-and-runners.md`'s Facts-First Bootstrap Ordering).
