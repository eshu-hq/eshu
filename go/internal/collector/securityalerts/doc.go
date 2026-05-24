// Package securityalerts normalizes repository-scoped provider security alert
// evidence into durable source facts.
//
// This package owns the first provider security alert collector slice:
// synthetic GitHub Dependabot alert fixtures and the bounded request client
// shape used by a later hosted runtime. Emitted facts preserve provider alert
// identifiers, state, dependency coordinates, advisory identifiers, version
// ranges, severity, CVSS, EPSS, CWE, timestamps, and source URLs with reported
// confidence. They are not canonical Eshu impact truth; reducers reconcile them
// with Eshu-owned dependency and vulnerability evidence.
package securityalerts
