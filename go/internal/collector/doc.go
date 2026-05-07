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
// watch mode does not reindex on ignored generated output. Native and SCIP
// snapshots preserve parser-emitted dead-code root metadata in content entity
// facts; query-time classification decides how that evidence is presented.
package collector
