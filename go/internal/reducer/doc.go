// Package reducer owns cross-domain materialization, queued repair, and
// shared projection that runs after source-local facts have been committed.
//
// The reducer admits candidates from relationship evidence, projects
// resolved relationships, materializes code-call and code-reference edges
// between source entities, including bounded JavaScript-family import,
// CommonJS property require, module.exports self-alias, imported JavaScript
// namespace calls before same-file trailing-name fallback, function receiver,
// constructor, re-export, dynamic import, returned and constructor-argument
// function-value, Go package-qualified imports including explicit Go aliases,
// method-return chains with parser-proven receiver types, TypeScript
// type-reference, Java local
// receiver, method-reference, literal reflection, ServiceLoader provider,
// Spring auto-configuration, overload arity, and typed
// argument/parameter signatures including helper-call return types, Java
// enhanced-for receiver evidence, and Java enclosing-class and explicit
// outer-this field receiver contexts, Python constructor, self receiver, class
// receiver, inherited classmethod, and local receiver evidence, static
// JavaScript registry dispatch, and package-file-root evidence.
// SQL relationship materialization emits trigger-to-table TRIGGERS edges and
// trigger-to-function EXECUTES edges from parser-proven SqlTrigger metadata so
// trigger-bound SqlFunction routines remain reachable in code dead-code
// analysis. SQL name helpers index exact names and unqualified trailing
// aliases, target resolution prefers same-file entities, and ambiguous
// cross-file names are skipped rather than manufacturing reachability.
// Code-call rows carry endpoint IDs plus the endpoint entity labels needed by
// the graph writer to keep canonical CALLS and REFERENCES writes selective, and
// the reducer drives repair flows for domains that depend on later phases of the
// bootstrap pipeline. Java reference materialization uses REFERENCES rather
// than CALLS when source text proves runtime reachability without proving direct
// invocation. Changes here need careful proof: track raw evidence, admitted
// candidates, projected rows, graph writes, and query surfaces before changing
// ordering, admission, retries, or
// backend-specific behavior. Code-call projection may wait for reducer graph
// domains to drain in local NornicDB runs, but that gate only controls write
// scheduling. It can process large accepted repo/run units in chunks, and it
// must skip retraction after the first current-run chunk so earlier chunk writes
// remain graph-visible. Reducer code must remain idempotent across retries and
// replays so repair runs converge on the same truth. Code-call materialization
// logs stage timings for fact load, extraction, intent build, and intent upsert
// so repo-scale bottlenecks can be classified before changing query shape or
// worker counts.
// Workload materialization inputs reuse the deployable-unit correlation gate
// before projecting workload rows.
package reducer
