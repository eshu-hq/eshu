// Package main hosts the Terraform-state collector runtime.
//
// The runtime claims workflow-coordinator Terraform-state work for one enabled
// collector instance, opens only exact configured or approved state sources,
// parses redacted evidence, and commits facts through the shared ingestion
// boundary. It exposes the hosted Eshu admin and metrics surface, including
// Terraform-state claim, source, parse, redaction, schema-resolver, and
// composite-capture telemetry. It does not scan buckets, read unapproved local
// state, or decide which work items exist.
package main
