// Package cloudformation extracts CloudFormation and SAM template evidence for
// parser adapters.
//
// IsTemplate recognizes bounded CloudFormation JSON or YAML documents. Parse
// returns deterministic resource, parameter, output, condition, import, and
// export buckets that parent JSON and YAML adapters can attach to their payloads.
// The package is intentionally independent from the parent parser dispatcher so
// JSON and YAML package moves can share one contract.
package cloudformation
