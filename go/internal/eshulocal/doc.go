// Package eshulocal implements the filesystem contract for the local Eshu
// service.
//
// It owns workspace-root resolution, workspace-id derivation, the on-disk
// ${ESHU_HOME}/local/workspaces/<id>/ layout, the owner.lock flock protocol,
// and the owner.json record. It also owns embedded Postgres startup recovery:
// after owner.lock is held, StartEmbeddedPostgres can stop an ownerless live
// postmaster.pid only when PID, socket, and Postgres protocol probes agree.
// Embedded Postgres startup output is routed to the workspace postgres.log so
// local foreground progress remains readable.
// The layout, ID algorithm, and single-service rules are defined by
// docs/docs/reference/local-data-root-spec.md and
// docs/docs/reference/local-host-lifecycle.md.
package eshulocal
