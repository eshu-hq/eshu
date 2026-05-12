// Package ociruntime scans configured OCI registry repositories and returns
// collected generations for the shared collector boundary.
//
// Source implements collector.Source by calling a provider-supplied
// RegistryClient for ping, tag list, manifest, and referrer reads. The runtime
// parses OCI and Docker-compatible manifest bytes, preserves digest identity,
// emits warning facts for non-fatal capability gaps such as missing referrers,
// and records bounded OCI registry metrics and spans without placing registry
// hosts, repository names, tags, or digests in metric labels.
package ociruntime
