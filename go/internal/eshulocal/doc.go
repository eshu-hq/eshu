// Package eshulocal implements the filesystem contract for the local Eshu
// service.
//
// It owns workspace-root resolution, workspace-id derivation, the on-disk
// ${ESHU_HOME}/local/workspaces/<id>/ layout, the owner.lock flock protocol,
// and the owner.json record. The layout, ID algorithm, and single-service
// rules are defined by docs/docs/reference/local-data-root-spec.md and
// docs/docs/reference/local-host-lifecycle.md.
package eshulocal
