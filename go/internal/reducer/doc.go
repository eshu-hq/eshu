// Package reducer owns cross-domain materialization, queued repair, and
// shared projection that runs after source-local facts have been committed.
//
// The reducer admits candidates from relationship evidence, projects
// resolved relationships, materializes code-call and code-reference edges
// between source entities, including bounded JavaScript-family import,
// CommonJS property require, module.exports self-alias, imported JavaScript
// namespace calls before same-file trailing-name fallback, function receiver,
// constructor, re-export, dynamic import, returned function-value, TypeScript
// type-reference, Java local receiver and overload arity, Python constructor,
// self receiver, class receiver, inherited classmethod, and local receiver
// evidence, static JavaScript registry dispatch, and package-file-root evidence.
// Code-call rows carry endpoint IDs plus the endpoint entity labels needed by
// the graph writer to keep canonical CALLS and REFERENCES writes selective, and
// the reducer drives repair flows for domains that depend on later phases of the
// bootstrap pipeline. Changes here need careful proof:
// track raw evidence, admitted candidates, projected rows, graph writes, and
// query surfaces before changing ordering, admission, retries, or
// backend-specific behavior. Code-call projection may wait for reducer graph
// domains to drain in local NornicDB runs, but that gate only controls write
// scheduling. Reducer code must remain idempotent across retries and replays so
// repair runs converge on the same truth.
// Workload materialization inputs reuse the deployable-unit correlation gate
// before projecting workload rows.
package reducer
