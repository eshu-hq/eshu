// Package main wires the OCI registry collector binary.
//
// The binary reads configured OCI Distribution-compatible registries, maps
// provider endpoint and auth shapes onto the shared Distribution client, emits
// digest-addressed OCI registry facts through collector.Service or
// collector.ClaimedService, and commits those facts through the shared Postgres
// ingestion store. When ESHU_COLLECTOR_INSTANCES_JSON is present it selects a
// claim-enabled oci_registry instance and uses workflow claim fencing.
package main
