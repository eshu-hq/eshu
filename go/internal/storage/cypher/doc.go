// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package cypher owns backend-neutral Cypher write contracts, canonical
// node and edge writers, statement metadata, and write instrumentation for
// Eshu's canonical graph.
//
// Writers in this package emit Statements that any supported graph backend
// can run through the Executor seam (InstrumentedExecutor, RetryingExecutor,
// TimeoutExecutor, BackpressureExecutor, ExecuteOnlyExecutor, and — only under
// the ifafaultinjection build tag — the test-only FaultingExecutor). The
// BackpressureExecutor bounds concurrent writes to a configurable in-flight
// ceiling so a slow backend slows intake instead of dead-lettering recoverable
// work (issue #3560); it wraps the outermost retry/timeout layer so one permit
// covers a whole write attempt. Dialect-specific behavior must stay
// narrow and explicit: schema adapters, writer options, and the BuildCanonical*
// statement builders own backend differences so callers do not need to branch
// on ESHU_GRAPH_BACKEND. Writes must be idempotent and retry-safe; the
// canonical writers (CanonicalNodeWriter, EdgeWriter) and OrphanSweepStore are
// the boundary where node, edge, and cleanup invariants are enforced before
// bytes reach Neo4j or NornicDB.
// Code-call and inheritance rows carry reducer-stamped resolution provenance;
// the edge writer derives confidence and reason from that method before the
// Cypher SET clause persists CALLS, REFERENCES, INHERITS, IMPLEMENTS,
// OVERRIDES, ALIASES, INSTANTIATES, or USES_METACLASS. Go and TypeScript
// type-reference metadata must remain REFERENCES so graph truth does not claim
// that type literals are invocations. SQL relationship rows may materialize as
// QUERIES_TABLE, READS_FROM, REFERENCES_TABLE, WRITES_TO, TRIGGERS, EXECUTES,
// INDEXES, MIGRATES, or column containment, with exact endpoint labels and
// EXECUTES preserving trigger-bound SqlFunction reachability for dead-code
// analysis. NornicDB SQL relationship writes use auto-commit because its
// managed transaction acknowledges these MERGE statements without persisting
// them; batching, backpressure, retry idempotency, and worker concurrency remain
// intact, while Neo4j retains grouped execution. Shell execution rows
// materialize Function-[:EXECUTES_SHELL]->ShellCommand using structural
// call-site metadata only; command text and arguments are not stored. When
// reducer evidence includes endpoint entity labels, EdgeWriter anchors code and
// SQL relationship writes on whitelisted exact labels such as Function, Class,
// File, Interface, Struct,
// TypeAlias, SqlTrigger, SqlTable, and SqlFunction plus uid, and falls back to
// the older label-family shape only for legacy rows with supported fallback
// templates.
// Canonical entity retractions run after current entity upserts and keep
// concrete labels in the Cypher anchor so stale-node and stale-edge cleanup
// remains selective on supported graph backends. Stale File-to-entity CONTAINS
// cleanup is owned by entity retraction, not a separate per-file relationship
// filter. Terraform evidence labels, including backend, import, moved, removed,
// check, and lockfile-provider nodes, stay in the same label-anchored cleanup
// path as other canonical entities. Repository writes clear current identity
// nodes in a dedicated cleanup phase before their MERGE phase. Directory and
// File nodes update in place because replacing them with DETACH DELETE is too
// expensive on local graph backends; missing nested files use a guarded MERGE
// through their parent Directory, while repository-root files use a
// Repository-contained path that does not require a synthetic root Directory.
// High-volume analysis metadata such as dead_code_root_kinds and
// exactness_blockers stays in the content store unless a graph query owns a
// proven need for that property. OCI registry writes keep manifests, indexes,
// and descriptors on ContainerImage-family labels keyed by digest-backed uid
// values; tag observations remain weak mutable evidence and participate in
// cleanup without becoming canonical image identity. The first_observed_at
// property on a ContainerImageTagObservation node -- the first queryable
// node-property timestamp in the canonical graph (issue #5459) -- is written
// by a separate, deferred set-once statement
// (canonicalOCIImageTagFirstObservedSetOnceCypher) that runs in the same
// second ExecuteGroup as the package_registry edges, never fused into the
// identity MERGE; see oci_tag_first_observed_prove_theory_live_test.go for the
// live proof this shape is required. Package-registry writes
// keep package, version, and package dependency identity keyed by uid; source
// repository hints remain weak evidence until reducer correlation admits an
// ownership or publication relationship, and duplicate package UIDs are
// coordinated by each CanonicalNodeWriter instance before backend execution so
// uniqueness retries are not the normal path for one projector process. Backend
// uniqueness and retry handling still own cross-process convergence.
// Repo-dependency writes enforce evidence-source capabilities before backend
// execution: projection/code-imports accepts only DEPENDS_ON and omits the
// impossible RUNS_ON retract, while other sources retain their full retract
// surface.
// AzureCloudResourceEdgeWriter mirrors the GCP CloudResource relationship writer
// for Azure managed relationships: it MATCHes both CloudResource endpoints by
// uid, MERGEs only bounded static AZURE_managed_by edge tokens, preserves
// relationship_type as a readback property, and never fabricates endpoint nodes.
// IncidentRoutingEvidenceWriter writes
// PagerDuty routing evidence nodes and static intended/applied/live evidence
// relationships without creating service, runtime, image, code-review, work
// item, or root-cause graph truth. EC2InternetExposureNodeWriter stamps
// reducer-owned exposure properties onto existing EC2 CloudResource nodes with
// MATCH-only Cypher and never fabricates instances. CodeTaintEvidenceWriter and
// CodeInterprocEvidenceWriter keep side-cleanup stale value-flow deletes scoped
// to one active scope/source and `generation_id <> current_generation`, with a
// bounded LIMIT so current evidence survives gate-disabled generations.
// OrphanSweepStore detects orphans with an app-side anti-join over the closed
// Repository, Platform, EvidenceArtifact, File, Directory, and Module label
// set: a static-label candidate read and a concrete-relationship-variable
// connected-keys read, differenced in Go, because NornicDB mis-evaluates every
// relationship-existence predicate. Its mark/clear/sweep writes are key-anchored
// static-label SET/REMOVE/DELETE statements, and it deletes only nodes whose
// orphan marker aged past the configured TTL (re-verifying connectivity first).
package cypher
