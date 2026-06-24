// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entrypoints

// Manifest is the schema consumed by the collector entrypoint generator.
type Manifest struct {
	SchemaVersion        int        `json:"schema_version" yaml:"schema_version"`
	CommandDir           string     `json:"command_dir" yaml:"command_dir"`
	RuntimeName          string     `json:"runtime_name" yaml:"runtime_name"`
	BinaryName           string     `json:"binary_name" yaml:"binary_name"`
	CollectorLabel       string     `json:"collector_label" yaml:"collector_label"`
	GoName               string     `json:"go_name" yaml:"go_name"`
	Env                  EnvSpec    `json:"env" yaml:"env"`
	StoreName            string     `json:"store_name" yaml:"store_name"`
	ClaimIDPrefix        string     `json:"claim_id_prefix" yaml:"claim_id_prefix"`
	CollectorKindExpr    string     `json:"collector_kind_expr" yaml:"collector_kind_expr"`
	MaxAttemptsExpr      string     `json:"max_attempts_expr,omitempty" yaml:"max_attempts_expr,omitempty"`
	ScopeKind            string     `json:"scope_kind" yaml:"scope_kind"`
	AuthMode             string     `json:"auth_mode" yaml:"auth_mode"`
	TargetListField      string     `json:"target_list_field" yaml:"target_list_field"`
	TargetIdentityFields []string   `json:"target_identity_fields" yaml:"target_identity_fields"`
	TargetAuthFields     []string   `json:"target_auth_fields" yaml:"target_auth_fields"`
	Source               SourceSpec `json:"source" yaml:"source"`
}

// EnvSpec names the environment variables the generated runtime loader reads.
type EnvSpec struct {
	CollectorInstances string `json:"collector_instances" yaml:"collector_instances"`
	InstanceID         string `json:"instance_id" yaml:"instance_id"`
	PollInterval       string `json:"poll_interval" yaml:"poll_interval"`
	ClaimLeaseTTL      string `json:"claim_lease_ttl" yaml:"claim_lease_ttl"`
	HeartbeatInterval  string `json:"heartbeat_interval" yaml:"heartbeat_interval"`
	OwnerID            string `json:"owner_id" yaml:"owner_id"`
	OwnerIDConstName   string `json:"owner_id_const_name" yaml:"owner_id_const_name"`
}

// SourceSpec describes the provider-owned source constructor and config hooks.
type SourceSpec struct {
	ImportPath        string `json:"import_path" yaml:"import_path"`
	PackageName       string `json:"package_name" yaml:"package_name"`
	ConfigType        string `json:"config_type" yaml:"config_type"`
	Constructor       string `json:"constructor" yaml:"constructor"`
	ConfigLoader      string `json:"config_loader" yaml:"config_loader"`
	ConfigAttacher    string `json:"config_attacher" yaml:"config_attacher"`
	RuntimeConfigType string `json:"runtime_config_type" yaml:"runtime_config_type"`
}

// GeneratedFile is one generated file and its formatted contents.
type GeneratedFile struct {
	Name     string
	Contents []byte
}
