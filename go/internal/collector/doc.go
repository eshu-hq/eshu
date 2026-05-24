// Package collector owns source observation, repository selection, snapshot
// capture, parser input shaping, and fact streaming for Eshu indexing runs.
//
// The package turns selected sources into cloned or native filesystem
// snapshots, discovery reports, parser metadata, content entity snapshots, and
// facts.Envelope streams. It is the source of truth for snapshot input shape,
// but graph projection and query-time truth belong to downstream projector,
// reducer, storage, and query packages.
//
// Collection is best-effort over remote and local filesystems. Callers must
// handle partial snapshots, discovery skips, webhook-triggered refreshes, claim
// fencing, and batch-drain hooks explicitly. Raw Terraform-state bytes do not
// enter normal repository snapshots; only metadata-only state candidates are
// emitted for the Terraform-state collector path to approve and read.
//
// The scannerworker subpackage is a contract package for future isolated
// security analyzers. It defines claim input, target scope, resource limits,
// source-fact output, and retry/dead-letter payloads, but it does not implement
// analyzer execution.
package collector
