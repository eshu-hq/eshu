// Package ghactionsruntime collects bounded GitHub Actions run metadata for
// the CI/CD run collector family.
//
// The package owns hosted provider polling and claim resolution. It delegates
// fact envelope construction to the fixture-backed cicdrun normalizer so live
// provider rows and offline fixtures share one schema. It does not read
// artifact contents, logs, secrets, graph state, or query state.
package ghactionsruntime
