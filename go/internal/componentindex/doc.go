// Package componentindex validates the static community extension index.
//
// The package treats index membership as advisory metadata. It checks reviewed
// entries for deterministic schema, digest, lifecycle, revocation, and
// fact-kind ownership errors, but it never installs packages, pulls artifacts,
// or overrides local component trust policy.
package componentindex
