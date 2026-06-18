// Command collector-gcp-cloud runs the GCP Cloud Asset Inventory collector.
//
// In fixture mode, the binary loads a declarative JSON config (collector
// instance id, poll interval, and bounded scopes referencing read-only
// credentials by name), constructs a gcpruntime.Source backed by an offline
// FixturePageProvider, and commits each collected generation through the shared
// Postgres ingestion store wrapped by a status committer that records the
// bounded GCP claim metric.
//
// In claimed-live mode, the binary requires explicit workflow collector
// configuration with live_collection_enabled=true, constructs the explicit
// gcpruntime.LiveClient, and runs through collector.ClaimedService so claim
// acquire, heartbeat, fenced commit, retry, and terminal failure behavior follow
// the shared workflow lifecycle. Helm values, ServiceMonitor wiring, and live
// smoke proof remain gated per docs/public/reference/gcp-cloud-collector-contract.md.
package main
