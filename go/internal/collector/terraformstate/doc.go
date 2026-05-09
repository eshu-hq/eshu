// Package terraformstate reads Terraform state snapshots into redacted facts.
//
// The package keeps raw Terraform state inside source readers and parser-local
// windows only. Callers receive typed fact envelopes and redaction evidence, not
// raw state bytes or unredacted attribute values.
package terraformstate
