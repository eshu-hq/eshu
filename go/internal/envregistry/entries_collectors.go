package envregistry

// collectorFrameworkEntries returns the five standard hosted-collector framework
// variables for one collector. The lease/poll/heartbeat names are passed
// explicitly because collectors are inconsistent about the "_COLLECTOR_" infix;
// the registry records the exact names each collector reads. Defaults match
// internal/workflow/defaults.go (lease 60s, heartbeat 20s) and the common 1s
// poll interval; pass a non-empty pollDefault to override (e.g. kubernetes-live).
func collectorFrameworkEntries(subsystem, instanceID, ownerID, pollInterval, pollDefault, leaseTTL, heartbeat string) []Entry {
	if pollDefault == "" {
		pollDefault = "1s"
	}
	entries := make([]Entry, 0, 5)
	if instanceID != "" {
		entries = append(entries, Entry{Name: instanceID, Type: VarString, Subsystem: subsystem, Description: "Instance ID selecting this collector instance from ESHU_COLLECTOR_INSTANCES_JSON."})
	}
	if ownerID != "" {
		entries = append(entries, Entry{Name: ownerID, Type: VarString, Subsystem: subsystem, Description: "Lease owner identifier; defaults to the hostname."})
	}
	if pollInterval != "" {
		entries = append(entries, Entry{Name: pollInterval, Type: VarDuration, Default: pollDefault, Subsystem: subsystem, Description: "Poll interval for discovering targets."})
	}
	if leaseTTL != "" {
		entries = append(entries, Entry{Name: leaseTTL, Type: VarDuration, Default: "60s", Subsystem: subsystem, Description: "Workflow claim lease TTL."})
	}
	if heartbeat != "" {
		entries = append(entries, Entry{Name: heartbeat, Type: VarDuration, Default: "20s", Subsystem: subsystem, Description: "Claim heartbeat interval; must be less than the lease TTL."})
	}
	return entries
}

// collectorEntries declares the hosted-collector production configuration
// variables. ESHU_COMPONENT_* and ESHU_COLLECTOR_INSTANCES_JSON are intentionally
// absent here because they are already declared in coreEntries (component and
// coordinator subsystems) and are reused by the component-extension collector.
// Container-registry credential variables (ESHU_*_OCI_*, ESHU_*_PACKAGE_*) are
// integration-test gating read only from _test.go and are out of scope.
var collectorEntries = func() []Entry {
	var entries []Entry
	add := func(es ...Entry) { entries = append(entries, es...) }

	// Standard-framework collectors (infix "_COLLECTOR_" on poll/lease/heartbeat).
	add(collectorFrameworkEntries("collector-tempo",
		"ESHU_TEMPO_COLLECTOR_INSTANCE_ID", "ESHU_TEMPO_COLLECTOR_OWNER_ID",
		"ESHU_TEMPO_COLLECTOR_POLL_INTERVAL", "", "ESHU_TEMPO_COLLECTOR_CLAIM_LEASE_TTL", "ESHU_TEMPO_COLLECTOR_HEARTBEAT_INTERVAL")...)
	add(collectorFrameworkEntries("collector-loki",
		"ESHU_LOKI_COLLECTOR_INSTANCE_ID", "ESHU_LOKI_COLLECTOR_OWNER_ID",
		"ESHU_LOKI_COLLECTOR_POLL_INTERVAL", "", "ESHU_LOKI_COLLECTOR_CLAIM_LEASE_TTL", "ESHU_LOKI_COLLECTOR_HEARTBEAT_INTERVAL")...)
	add(collectorFrameworkEntries("collector-prometheus-mimir",
		"ESHU_PROMETHEUS_MIMIR_COLLECTOR_INSTANCE_ID", "ESHU_PROMETHEUS_MIMIR_COLLECTOR_OWNER_ID",
		"ESHU_PROMETHEUS_MIMIR_COLLECTOR_POLL_INTERVAL", "", "ESHU_PROMETHEUS_MIMIR_COLLECTOR_CLAIM_LEASE_TTL", "ESHU_PROMETHEUS_MIMIR_COLLECTOR_HEARTBEAT_INTERVAL")...)
	add(collectorFrameworkEntries("collector-grafana",
		"ESHU_GRAFANA_COLLECTOR_INSTANCE_ID", "ESHU_GRAFANA_COLLECTOR_OWNER_ID",
		"ESHU_GRAFANA_COLLECTOR_POLL_INTERVAL", "", "ESHU_GRAFANA_COLLECTOR_CLAIM_LEASE_TTL", "ESHU_GRAFANA_COLLECTOR_HEARTBEAT_INTERVAL")...)
	add(collectorFrameworkEntries("collector-aws-cloud",
		"ESHU_AWS_COLLECTOR_INSTANCE_ID", "ESHU_AWS_COLLECTOR_OWNER_ID",
		"ESHU_AWS_COLLECTOR_POLL_INTERVAL", "", "ESHU_AWS_COLLECTOR_CLAIM_LEASE_TTL", "ESHU_AWS_COLLECTOR_HEARTBEAT_INTERVAL")...)
	add(Entry{Name: "ESHU_AWS_REDACTION_KEY", Type: VarString, Subsystem: "collector-aws-cloud", Description: "Encryption key for redacting AWS secrets when sensitive service scans are enabled."})
	add(collectorFrameworkEntries("collector-gcp-cloud",
		"ESHU_GCP_COLLECTOR_INSTANCE_ID", "ESHU_GCP_COLLECTOR_OWNER_ID",
		"ESHU_GCP_COLLECTOR_POLL_INTERVAL", "", "ESHU_GCP_COLLECTOR_CLAIM_LEASE_TTL", "ESHU_GCP_COLLECTOR_HEARTBEAT_INTERVAL")...)

	// Collectors whose poll/lease/heartbeat omit the "_COLLECTOR_" infix.
	add(collectorFrameworkEntries("collector-jira",
		"ESHU_JIRA_COLLECTOR_INSTANCE_ID", "ESHU_JIRA_COLLECTOR_OWNER_ID",
		"ESHU_JIRA_POLL_INTERVAL", "", "ESHU_JIRA_CLAIM_LEASE_TTL", "ESHU_JIRA_HEARTBEAT_INTERVAL")...)
	add(collectorFrameworkEntries("collector-cicd-run",
		"ESHU_CICD_RUN_COLLECTOR_INSTANCE_ID", "ESHU_CICD_RUN_COLLECTOR_OWNER_ID",
		"ESHU_CICD_RUN_POLL_INTERVAL", "", "ESHU_CICD_RUN_CLAIM_LEASE_TTL", "ESHU_CICD_RUN_HEARTBEAT_INTERVAL")...)
	add(collectorFrameworkEntries("collector-pagerduty",
		"ESHU_PAGERDUTY_COLLECTOR_INSTANCE_ID", "ESHU_PAGERDUTY_COLLECTOR_OWNER_ID",
		"ESHU_PAGERDUTY_POLL_INTERVAL", "", "ESHU_PAGERDUTY_CLAIM_LEASE_TTL", "ESHU_PAGERDUTY_HEARTBEAT_INTERVAL")...)
	add(collectorFrameworkEntries("collector-security-alerts",
		"ESHU_SECURITY_ALERT_COLLECTOR_INSTANCE_ID", "ESHU_SECURITY_ALERT_COLLECTOR_OWNER_ID",
		"ESHU_SECURITY_ALERT_POLL_INTERVAL", "", "ESHU_SECURITY_ALERT_CLAIM_LEASE_TTL", "ESHU_SECURITY_ALERT_HEARTBEAT_INTERVAL")...)
	add(collectorFrameworkEntries("collector-sbom-attestation",
		"ESHU_SBOM_ATTESTATION_COLLECTOR_INSTANCE_ID", "ESHU_SBOM_ATTESTATION_COLLECTOR_OWNER_ID",
		"ESHU_SBOM_ATTESTATION_POLL_INTERVAL", "", "ESHU_SBOM_ATTESTATION_CLAIM_LEASE_TTL", "ESHU_SBOM_ATTESTATION_HEARTBEAT_INTERVAL")...)
	add(collectorFrameworkEntries("collector-vulnerability-intelligence",
		"ESHU_VULNERABILITY_INTELLIGENCE_COLLECTOR_INSTANCE_ID", "ESHU_VULNERABILITY_INTELLIGENCE_COLLECTOR_OWNER_ID",
		"ESHU_VULNERABILITY_INTELLIGENCE_POLL_INTERVAL", "", "ESHU_VULNERABILITY_INTELLIGENCE_CLAIM_LEASE_TTL", "ESHU_VULNERABILITY_INTELLIGENCE_HEARTBEAT_INTERVAL")...)
	add(collectorFrameworkEntries("collector-vault-live",
		"ESHU_VAULT_LIVE_COLLECTOR_INSTANCE_ID", "ESHU_VAULT_LIVE_COLLECTOR_OWNER_ID",
		"ESHU_VAULT_LIVE_POLL_INTERVAL", "", "ESHU_VAULT_LIVE_CLAIM_LEASE_TTL", "ESHU_VAULT_LIVE_HEARTBEAT_INTERVAL")...)
	add(Entry{Name: "ESHU_VAULT_LIVE_REDACTION_KEY", Type: VarString, Subsystem: "collector-vault-live", Description: "Encryption key for redacting sensitive Vault data."})

	// Package-registry and OCI-registry collectors (poll/lease/heartbeat without infix).
	add(collectorFrameworkEntries("collector-package-registry",
		"ESHU_PACKAGE_REGISTRY_COLLECTOR_INSTANCE_ID", "ESHU_PACKAGE_REGISTRY_COLLECTOR_OWNER_ID",
		"ESHU_PACKAGE_REGISTRY_POLL_INTERVAL", "", "ESHU_PACKAGE_REGISTRY_CLAIM_LEASE_TTL", "ESHU_PACKAGE_REGISTRY_HEARTBEAT_INTERVAL")...)
	add(collectorFrameworkEntries("collector-oci-registry",
		"ESHU_OCI_REGISTRY_COLLECTOR_INSTANCE_ID", "ESHU_OCI_REGISTRY_COLLECTOR_OWNER_ID",
		"ESHU_OCI_REGISTRY_POLL_INTERVAL", "", "ESHU_OCI_REGISTRY_CLAIM_LEASE_TTL", "ESHU_OCI_REGISTRY_HEARTBEAT_INTERVAL")...)
	add(Entry{Name: "ESHU_OCI_REGISTRY_TARGETS_JSON", Type: VarString, Subsystem: "collector-oci-registry", Description: "JSON array of OCI registry target configurations."})

	// terraform-state collector (TFSTATE prefix; extra redaction/source vars).
	add(collectorFrameworkEntries("collector-terraform-state",
		"ESHU_TFSTATE_COLLECTOR_INSTANCE_ID", "ESHU_TFSTATE_COLLECTOR_OWNER_ID",
		"ESHU_TFSTATE_COLLECTOR_POLL_INTERVAL", "", "ESHU_TFSTATE_COLLECTOR_CLAIM_LEASE_TTL", "ESHU_TFSTATE_COLLECTOR_HEARTBEAT_INTERVAL")...)
	add(
		Entry{Name: "ESHU_TFSTATE_COLLECTOR_HEARTBEAT", Type: VarDuration, Default: "20s", Subsystem: "collector-terraform-state", Deprecated: true, ReplacedBy: "ESHU_TFSTATE_COLLECTOR_HEARTBEAT_INTERVAL", Description: "Legacy heartbeat interval alias."},
		Entry{Name: "ESHU_TFSTATE_REDACTION_KEY", Type: VarString, Subsystem: "collector-terraform-state", Description: "Encryption key for redacting Terraform state secrets."},
		Entry{Name: "ESHU_TFSTATE_REDACTION_RULESET_VERSION", Type: VarString, Subsystem: "collector-terraform-state", Description: "Versioned policy identifier for redacting sensitive keys."},
		Entry{Name: "ESHU_TFSTATE_REDACTION_SENSITIVE_KEYS", Type: VarString, Subsystem: "collector-terraform-state", Description: "Comma-separated leaf keys to redact; defaults to password,secret,token,access_key,private_key,certificate,key_pair."},
		Entry{Name: "ESHU_TFSTATE_SOURCE_MAX_BYTES", Type: VarInt, Default: "0", Subsystem: "collector-terraform-state", Description: "Maximum bytes accepted per Terraform state source; 0 means unlimited."},
		Entry{Name: "ESHU_TERRAFORM_SCHEMA_DIR", Type: VarString, Subsystem: "collector-terraform-state", Description: "Directory of Terraform provider schemas; defaults to the built-in schema directory."},
	)

	// azure-cloud collector (does not follow the framework).
	add(
		Entry{Name: "ESHU_AZURE_COLLECTOR_INSTANCE_ID", Type: VarString, Subsystem: "collector-azure-cloud", Description: "Instance ID selecting this Azure collector instance."},
		Entry{Name: "ESHU_AZURE_POLL_INTERVAL", Type: VarDuration, Default: "5m", Subsystem: "collector-azure-cloud", Description: "Poll interval for discovering Azure targets."},
		Entry{Name: "ESHU_AZURE_TARGETS_JSON", Type: VarString, Subsystem: "collector-azure-cloud", Description: "JSON array of Azure target scopes. Each target may set source_lane to resource_graph or fixture-only resource_changes."},
		Entry{Name: "ESHU_AZURE_FIXTURE_PAGES_JSON", Type: VarString, Subsystem: "collector-azure-cloud", Description: "JSON fixture pages for single-lane offline Resource Graph or resourcechanges smoke testing; not used in production."},
		Entry{Name: "ESHU_AZURE_REDACTION_KEY_FILE", Type: VarString, Subsystem: "collector-azure-cloud", Description: "Read-only file path for Azure redaction key material used to fingerprint tags, managed identities, and resource-change actors."},
	)

	// kubernetes-live collector (5m default poll; clusters JSON; no lease/heartbeat).
	add(
		Entry{Name: "ESHU_KUBERNETES_LIVE_COLLECTOR_INSTANCE_ID", Type: VarString, Subsystem: "collector-kubernetes-live", Description: "Instance ID for the kubernetes-live collector."},
		Entry{Name: "ESHU_KUBERNETES_LIVE_CLUSTERS_JSON", Type: VarString, Subsystem: "collector-kubernetes-live", Description: "JSON array of Kubernetes cluster targets with auth configuration."},
		Entry{Name: "ESHU_KUBERNETES_LIVE_POLL_INTERVAL", Type: VarDuration, Default: "5m", Subsystem: "collector-kubernetes-live", Description: "Poll interval for discovering Kubernetes resources."},
	)

	// component-extension collector: the ESHU_COMPONENT_* policy vars are already
	// in coreEntries; only the collector-framework vars are added here.
	add(
		Entry{Name: "ESHU_COMPONENT_COLLECTOR_INSTANCE_ID", Type: VarString, Subsystem: "collector-component-extension", Description: "Instance ID for the component-extension collector."},
		Entry{Name: "ESHU_COMPONENT_COLLECTOR_OWNER_ID", Type: VarString, Subsystem: "collector-component-extension", Description: "Lease owner identifier; defaults to the hostname."},
		Entry{Name: "ESHU_COMPONENT_COLLECTOR_POLL_INTERVAL", Type: VarDuration, Default: "1s", Subsystem: "collector-component-extension", Description: "Poll interval for discovering component activations."},
		Entry{Name: "ESHU_COMPONENT_COLLECTOR_CLAIM_LEASE_TTL", Type: VarDuration, Default: "60s", Subsystem: "collector-component-extension", Description: "Workflow claim lease TTL."},
		Entry{Name: "ESHU_COMPONENT_COLLECTOR_HEARTBEAT_INTERVAL", Type: VarDuration, Default: "20s", Subsystem: "collector-component-extension", Description: "Claim heartbeat interval; must be less than the lease TTL."},
		Entry{Name: "ESHU_COMPONENT_COLLECTOR_SCOPE_KIND", Type: VarString, Subsystem: "collector-component-extension", Description: "Scope kind for component activations (e.g. organization, team)."},
	)

	return entries
}()
