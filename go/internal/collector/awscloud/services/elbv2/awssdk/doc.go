// Package awssdk adapts AWS SDK for Go v2 ELBv2 responses into scanner-owned
// ELBv2 records. It owns SDK pagination, batched tag reads, response mapping,
// and per-call telemetry.
package awssdk
