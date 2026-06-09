// Package semanticdocs builds provenance-only semantic documentation
// observation facts from bounded documentation sections.
//
// The package accepts existing doctruth section inputs and mocked, already
// redacted observation output, then returns validated fact envelopes. It does
// not call providers, persist facts, write graph state, expose API routes, or
// admit observations as canonical truth. Unsafe redaction state fails closed by
// forcing provenance-only output and dropping observation text.
package semanticdocs
