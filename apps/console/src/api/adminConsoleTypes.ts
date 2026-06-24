// api/adminConsoleTypes.ts
// Shared provenance types for the admin console loaders (issue #3703). Kept in a
// leaf module so adminConsole.ts and adminConsoleAudit.ts can both import them
// without a circular dependency.

// AdminProvenance distinguishes live data from an empty-because-error result.
// An "unavailable" loader always returns an EMPTY result set — never fabricated
// rows.
export type AdminProvenance = "live" | "unavailable";

// AdminAuditProvenance adds "forbidden" (HTTP 403): the audit routes are global
// shared-operator only, so a tenant admin is not authorized for this scope. That
// is a scope boundary, not a failure.
export type AdminAuditProvenance = AdminProvenance | "forbidden";
