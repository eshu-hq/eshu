// Package admission applies the bounded confidence and structural-evidence
// gates that decide whether a correlation candidate is admitted or rejected.
//
// `Evaluate` validates the candidate and the requirement set, computes
// `MeetsConfidence` against a `[0,1]` threshold, computes `MeetsStructure`
// by counting evidence atoms that satisfy every selector in each
// `EvidenceRequirement`, and sets the candidate state accordingly. Empty
// requirement sets are treated as structurally satisfied. Identity fields on
// the candidate are not modified; callers must rely on the returned candidate,
// not on mutations to their input.
package admission
