// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcp

// assetTypeInventory is the static Cloud Asset Inventory asset-type
// vocabulary this generator synthesizes resources against. It is a literal
// copy of the asset-type strings registered with
// go/internal/collector/gcpcloud's typed-depth extractor registry
// (RegisterAssetExtractor call sites), not an import of that package: this
// module must not import collector internals (issue #4581, Contract System
// v1 §3.5), so the inventory is a deliberately duplicated, bounded constant
// list rather than a live reflection of the registry.
//
// Keeping this list in sync with the real registry is a manual, low-frequency
// maintenance task (a new extractor file lands rarely and is reviewed by a
// human), not a generated artifact — the two are independent by design so a
// synthetic corpus never accidentally imports collector internals through a
// generator script. AssetTypeInventory() is exported so a maintainer's parity
// check (see paritycheck.go) can iterate the same list this package's
// generator draws from.
var assetTypeInventory = []string{
	"alloydb.googleapis.com/Cluster",
	"apigateway.googleapis.com/Gateway",
	"apikeys.googleapis.com/Key",
	"appengine.googleapis.com/Application",
	"appengine.googleapis.com/Service",
	"artifactregistry.googleapis.com/DockerImage",
	"artifactregistry.googleapis.com/Repository",
	"bigquery.googleapis.com/Dataset",
	"bigquery.googleapis.com/Routine",
	"bigquery.googleapis.com/Table",
	"bigquerydatatransfer.googleapis.com/TransferConfig",
	"bigtableadmin.googleapis.com/Cluster",
	"bigtableadmin.googleapis.com/Instance",
	"certificatemanager.googleapis.com/Certificate",
	"cloudbuild.googleapis.com/Build",
	"cloudbuild.googleapis.com/BuildTrigger",
	"cloudfunctions.googleapis.com/CloudFunction",
	"cloudfunctions.googleapis.com/Function",
	"cloudkms.googleapis.com/CryptoKey",
	"cloudkms.googleapis.com/KeyRing",
	"cloudscheduler.googleapis.com/Job",
	"cloudtasks.googleapis.com/Queue",
	"composer.googleapis.com/Environment",
	"compute.googleapis.com/Address",
	"compute.googleapis.com/BackendService",
	"compute.googleapis.com/Disk",
	"compute.googleapis.com/Firewall",
	"compute.googleapis.com/ForwardingRule",
	"compute.googleapis.com/GlobalAddress",
	"compute.googleapis.com/GlobalForwardingRule",
	"compute.googleapis.com/HealthCheck",
	"compute.googleapis.com/Instance",
	"compute.googleapis.com/InterconnectAttachment",
	"compute.googleapis.com/Network",
	"compute.googleapis.com/NetworkEndpointGroup",
	"compute.googleapis.com/RegionBackendService",
	"compute.googleapis.com/Route",
	"compute.googleapis.com/Router",
	"compute.googleapis.com/SecurityPolicy",
	"compute.googleapis.com/SslCertificate",
	"compute.googleapis.com/Subnetwork",
	"compute.googleapis.com/TargetHttpsProxy",
	"compute.googleapis.com/UrlMap",
	"compute.googleapis.com/VpnGateway",
	"compute.googleapis.com/VpnTunnel",
	"container.googleapis.com/Cluster",
	"dataflow.googleapis.com/Job",
	"dataform.googleapis.com/Repository",
	"dataplex.googleapis.com/EntryGroup",
	"dataproc.googleapis.com/Cluster",
	"dns.googleapis.com/ManagedZone",
	"dns.googleapis.com/Policy",
	"eventarc.googleapis.com/Trigger",
	"file.googleapis.com/Instance",
	"firebase.googleapis.com/FirebaseAppInfo",
	"firebase.googleapis.com/FirebaseProject",
	"firebaserules.googleapis.com/Ruleset",
	"firestore.googleapis.com/Database",
	"iam.googleapis.com/Role",
	"iam.googleapis.com/ServiceAccount",
	"iam.googleapis.com/ServiceAccountKey",
	"iam.googleapis.com/WorkloadIdentityPool",
	"iam.googleapis.com/WorkloadIdentityPoolProvider",
	"identitytoolkit.googleapis.com/Config",
	"logging.googleapis.com/LogBucket",
	"logging.googleapis.com/LogSink",
	"memcache.googleapis.com/Instance",
	"orgpolicy.googleapis.com/Policy",
	"pubsub.googleapis.com/Subscription",
	"pubsub.googleapis.com/Topic",
	"recaptchaenterprise.googleapis.com/Key",
	"redis.googleapis.com/Instance",
	"run.googleapis.com/Revision",
	"run.googleapis.com/Service",
	"secretmanager.googleapis.com/Secret",
	"secretmanager.googleapis.com/SecretVersion",
	"spanner.googleapis.com/Database",
	"spanner.googleapis.com/Instance",
	"sqladmin.googleapis.com/Instance",
	"storage.googleapis.com/Bucket",
	"workflows.googleapis.com/Workflow",
}

// AssetTypeInventory returns the sorted, static Cloud Asset Inventory
// asset-type vocabulary this package synthesizes resources against. The
// returned slice is a defensive copy; callers may not mutate the package's
// internal inventory through it.
func AssetTypeInventory() []string {
	out := make([]string, len(assetTypeInventory))
	copy(out, assetTypeInventory)
	return out
}
