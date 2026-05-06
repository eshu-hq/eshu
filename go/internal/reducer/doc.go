// Package reducer owns cross-domain materialization, queued repair, and
// shared projection that runs after source-local facts have been committed.
//
// The reducer admits candidates from relationship evidence, projects
// resolved relationships and shared graph rows, and drives repair flows for
// domains that depend on later phases of the bootstrap pipeline. Changes
// here need careful proof: track raw evidence, admitted candidates,
// projected rows, graph writes, and query surfaces before changing
// ordering, admission, retries, or backend-specific behavior. Reducer code
// must remain idempotent across retries and replays so repair runs
// converge on the same truth. Workload materialization inputs reuse the
// deployable-unit correlation gate before projecting workload rows.
package reducer
