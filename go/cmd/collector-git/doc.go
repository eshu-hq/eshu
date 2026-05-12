// Package main runs the collector-git binary, the local verification runtime
// for native Go repository selection, sync, snapshot collection, content
// shaping, and fact commit into Postgres.
//
// When invoked with --version or -v, it prints the embedded application
// version and exits before runtime setup. Otherwise the binary opens Postgres
// through the runtime config helpers, builds a collector.Service backed by the
// native repository selector and snapshotter, optionally prioritizes queued
// GitHub webhook refresh triggers, and hosts it through
// app.NewHostedWithStatusServer so it exposes the shared `/healthz`, `/readyz`,
// `/metrics`, and `/admin/status` admin surface. It honors SIGINT and SIGTERM
// for clean shutdown.
package main
