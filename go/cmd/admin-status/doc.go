// Package main runs the admin-status binary, which renders the shared
// Eshu status report from Postgres for local CLI inspection.
//
// When invoked with --version or -v, it prints the embedded application
// version through the test-covered printAdminStatusVersionFlag helper and exits
// before opening Postgres. Otherwise the binary opens the configured Postgres database via the runtime config
// helpers, loads a status report through the shared status reader, and prints
// it in either text or JSON form. It is a one-shot CLI: it does not mount the
// long-running runtime admin surface and exits as soon as the report is
// emitted (or the database connection fails).
package main
