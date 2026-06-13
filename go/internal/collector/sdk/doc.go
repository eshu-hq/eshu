// Package sdk provides shared first-party collector helper contracts.
//
// The package owns low-level collector-kernel behavior that is common across
// hosted source collectors: bounded HTTP request execution, safe provider
// errors, retry-after parsing, and status-to-failure classification. It does
// not define fact payloads, source-specific pagination, provider redaction, or
// graph truth; those stay with the collector package that owns the source.
package sdk
