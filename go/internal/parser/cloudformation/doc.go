// Package cloudformation extracts CloudFormation and SAM template evidence for
// parser adapters.
//
// IsTemplate recognizes bounded CloudFormation JSON or YAML documents. Parse
// returns deterministic resource, parameter, output, condition, import, and
// export buckets that parent JSON and YAML adapters can attach to their
// payloads. Condition evaluation is intentionally limited to simple literal and
// parameter-default expressions, and unresolved values stay unevaluated instead
// of inventing deployment truth.
package cloudformation
