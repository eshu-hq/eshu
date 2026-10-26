# GitHub Actions inbox guidance

Read `README.md`, `schema.go`, `sql.go`, and `store.go` before editing this
package. Keep the delivery identity immutable, claims leased and fenced, and
terminal rows sticky. Every DDL change must update migration 236 or add a new
migration. Recheck duplicate delivery, stale ACK, expiry, attempt cap, and
concurrent claim tests against real PostgreSQL. Do not store raw webhook bodies,
credentials, or provider response payloads.
