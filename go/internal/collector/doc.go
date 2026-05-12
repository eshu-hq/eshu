// Package collector owns git collection, repository discovery, snapshot
// capture, and parser input shaping for Eshu indexing runs.
//
// It turns source repositories into the inputs required by parser and fact
// emission: cloned snapshots, native filesystem snapshots, discovery reports,
// file selections, and entity metadata. The package is the source of truth for
// what files exist in a snapshot and the metadata attached to them; it does not
// make graph projection or query-time truth decisions, which belong to the
// projector, reducer, storage, and query packages. Callers must treat
// collection as best-effort over remote and local filesystems and handle
// partial-snapshot and discovery-skip outcomes explicitly. Filesystem source
// manifests fingerprint the effective collector input, including ignore-rule
// files but excluding paths removed by `.gitignore` or `.eshuignore`, so local
// watch mode does not reindex on ignored generated output. Native snapshots
// choose parser variable scope from the source language so high-cardinality
// local variables do not enter graph projection unless a language needs them
// for query truth. Snapshot entity mapping carries parser buckets, including
// Terraform import/refactor/check and lockfile-provider evidence, into content
// facts before projector or query policy decides how to present them. Native
// snapshots pass Go package semantic roots from
// Engine.PreScanGoPackageSemanticRoots, including interface escapes, imported
// receiver method calls, chained receiver roots, generic constraint roots, and
// package import paths, into per-file parser options. Native and SCIP snapshots
// preserve parser-emitted dead-code root metadata in content entity facts;
// query-time classification decides how that evidence is presented.
// ObservedSource implementations return CollectorObservation so Service
// telemetry can start once a real collection attempt begins, making
// collector.observe traces include source collection and write time without
// emitting spans for idle polls.
// Service.AfterBatchDrained fires only after at least one committed
// generation and a drained source batch, so callers can hook reducer or status
// work to a real collection boundary instead of an idle poll.
// WebhookTriggerRepositorySelector is a compatibility selector for the
// webhook-listener rollout: it claims queued trigger rows, syncs GitHub,
// GitLab, and Bitbucket repositories through provider-scoped clone URLs, marks
// unsupported providers and handoff failures visibly, and lets
// PriorityRepositorySelector fall back to the scheduled selector when no
// webhook work is waiting.
package collector
