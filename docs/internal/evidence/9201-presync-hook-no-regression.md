# Schema-Bootstrap ArgoCD PreSync Hook No-Regression Note (#9201)

The chart change adds a default-off `schemaBootstrap.argocdPreSync` switch
and two fail-closed guards; the QA and AWS overlays set only values. No
migration, query, queue, or worker semantics change.

No-Regression Evidence (#9201): the default `helm template` render is
annotation-identical to before — Helm pre-install/pre-upgrade hooks only, no
ArgoCD annotations — asserted by
`TestHelmSchemaBootstrapDefaultKeepsHelmHooks` and the extended
`verify-helm-schema-bootstrap.sh` reject. The ArgoCD annotations render only
under explicit opt-in (`useHelmHooks=false` plus `argocdPreSync.enabled=true`,
enforced mutually exclusive by a fail-fast render error), and the
bundled-backend combination fails fast before any hook exists. The overlay
`activeDeadlineSeconds: 3600` only extends the client-side Job bound; the
Postgres backend behavior (receipts, advisory-lock waiters, invalid-index
cleanup) is unchanged, so a deadline kill still resumes safely.

No-Observability-Change: no new metrics, spans, logs, or status fields. The
operator signal stays ArgoCD sync state plus the hook Job's logs and status,
with the documented `kubectl wait` on the component label.
