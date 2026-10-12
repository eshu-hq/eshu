// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package preparse

// FoldedEnvKeys are the env keys, read by the git collector, discovery, or the
// parsers, that change what a snapshot emits. Each maps to the Config field
// that folds it into the identity. TestEveryEnvKeyIsClassified fails when a
// key read in those trees is in neither this map nor NonOutputEnvKeys.
var FoldedEnvKeys = map[string]string{
	"ESHU_DISCOVERY_IGNORED_PATH_GLOBS":   "Config.IgnoredPathGlobs",
	"ESHU_DISCOVERY_PRESERVED_PATH_GLOBS": "Config.PreservedPathGlobs",
	"ESHU_EMIT_DATAFLOW":                  "Config.EmitDataflow",
	"SCIP_INDEXER":                        "Config.SCIPEnabled",
	"SCIP_LANGUAGES":                      "Config.SCIPLanguages and Config.SCIPBinaries",
	"ESHU_BOOTSTRAP_IS_DEPENDENCY":        "Config.DependencyMode",
	"ESHU_BOOTSTRAP_PACKAGE_NAME":         "Config.DependencyName",
	"ESHU_BOOTSTRAP_PACKAGE_LANGUAGE":     "Config.DependencyLanguage",
}

// NonOutputEnvKeys are the env keys read in the same trees that do not change
// what a snapshot of one repository at one commit emits, each with the reason.
// Scheduling and concurrency knobs, credentials, and repository selection only
// decide when, how fast, or which repositories are snapshotted.
var NonOutputEnvKeys = map[string]string{
	"SCIP_WORKERS":                   "SCIP indexing concurrency",
	"ESHU_PARSE_WORKERS":             "parse concurrency; output is partition-order independent",
	"ESHU_SNAPSHOT_WORKERS":          "snapshot concurrency",
	"ESHU_LARGE_REPO_FILE_THRESHOLD": "large-repository scheduling class",
	"ESHU_LARGE_REPO_MAX_CONCURRENT": "large-repository concurrency cap",
	"ESHU_STREAM_BUFFER":             "fact stream channel size",
	"ESHU_CLONE_DEPTH":               "clone history depth; per-file commit facts carry the head commit",

	"ESHU_GIT_AUTH_METHOD":            "credential",
	"ESHU_GIT_TOKEN":                  "credential",
	"ESHU_GITHUB_APP_ID":              "credential",
	"ESHU_GITHUB_APP_INSTALLATION_ID": "credential",
	"ESHU_GITHUB_APP_PRIVATE_KEY":     "credential",
	"GITHUB_APP_ID":                   "credential",
	"GITHUB_APP_INSTALLATION_ID":      "credential",
	"GITHUB_APP_PRIVATE_KEY":          "credential",
	"GITHUB_TOKEN":                    "credential",
	"ESHU_SSH_KNOWN_HOSTS_PATH":       "credential",
	"ESHU_SSH_PRIVATE_KEY_PATH":       "credential",

	"ESHU_GITHUB_ORG":             "repository selection",
	"ESHU_INCLUDE_ARCHIVED_REPOS": "repository selection",
	"ESHU_REPO_LIMIT":             "repository selection",
	"ESHU_REPOSITORY_RULES_JSON":  "repository selection",
	"ESHU_REPO_SHARD_COUNT":       "repository selection (sharding)",
	"ESHU_REPO_SHARD_INDEX":       "repository selection (sharding)",
	"ESHU_REPO_SOURCE_MODE":       "source mode; filesystem mode has no commit and no identity",
	"ESHU_FILESYSTEM_ROOT":        "filesystem mode only; no commit and no identity",
	"ESHU_FILESYSTEM_DIRECT":      "filesystem mode only; no commit and no identity",
	"ESHU_REPOS_DIR":              "checkout location; a new location re-clones and parses",

	"ESHU_PINNED_REFS_JSON":               "pinned refs are their own scopes",
	"ESHU_PINNED_REF_FLEET_CAP":           "pinned-ref scheduling cap",
	"ESHU_PINNED_REF_PER_REPO_CAP":        "pinned-ref scheduling cap",
	"ESHU_REPO_RECONCILE_INTERVAL_HOURS":  "reconcile scheduling",
	"ESHU_REPO_RECONCILE_MAX_PER_CYCLE":   "reconcile scheduling",
	"ESHU_REPO_RECONCILE_PARSE_MAX_SKIPS": "reconcile scheduling (the skip valve itself)",
	"ESHU_REPO_SELECTION_LIVENESS_WINDOW": "selection observation lifetime",

	"ESHU_WEBHOOK_TRIGGER_CLAIM_LEASE_WINDOW": "webhook trigger scheduling",
	"ESHU_WEBHOOK_TRIGGER_CLAIM_LIMIT":        "webhook trigger scheduling",
	"ESHU_WEBHOOK_TRIGGER_HANDOFF_ENABLED":    "webhook trigger scheduling",
	"ESHU_WEBHOOK_TRIGGER_HANDOFF_OWNER":      "webhook trigger scheduling",
	"ESHU_WEBHOOK_TRIGGER_MAX_CLAIM_ATTEMPTS": "webhook trigger scheduling",
}
