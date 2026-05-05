// Package app wires the shared runtime config, observability, lifecycle, and
// runner contract used by every Eshu hosted command.
//
// Constructors load runtime config, build the lifecycle, and bind a Runner so
// the binary's main function only needs to call Application.Run. ComposeLifecycles
// chains start/stop hooks with rollback on partial start; MountStatusServer adds
// the shared admin and metrics surfaces from internal/runtime to a hosted app.
package app
