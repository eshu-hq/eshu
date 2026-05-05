// Package correlation aggregates the correlation evaluation entry points and
// operator-facing summary helpers used by the reducer.
//
// Sub-packages own the pipeline split: model holds shared types, rules holds
// declarative rule packs, engine applies a rule pack to candidates, admission
// evaluates confidence and structural gates, and explain renders evaluator
// output. This root package depends on engine and model only to derive
// summary counters; it does not perform admission or rule evaluation itself.
package correlation
