// Package main wires the OCI registry collector binary.
//
// The binary reads configured OCI Distribution-compatible registries, maps
// provider endpoint and auth shapes onto the shared Distribution client, emits
// digest-addressed OCI registry facts through collector.Service, and commits
// those facts through the shared Postgres ingestion store.
package main
