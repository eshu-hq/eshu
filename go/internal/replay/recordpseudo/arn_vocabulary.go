// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recordpseudo

// tokenSet is a vocabulary of structural ARN components.
type tokenSet map[string]bool

func tokens(words ...string) tokenSet {
	out := make(tokenSet, len(words))
	for _, word := range words {
		out[word] = true
	}
	return out
}

// arnTypeTokens is the per-service vocabulary of resource-type tokens that
// may lead an ARN's resource part and are kept verbatim. The reducer's
// CAN_PERFORM target classifier reads exactly these prefixes
// (reducer/iamcan), so they must survive; everything else in the first
// position is a customer name. A service absent here has no type tokens:
// SNS topics, SQS queues and every unknown service are learned whole. Keep
// the lists to structural words that AWS defines, never a value a customer
// could choose.
var arnTypeTokens = map[string]tokenSet{
	"ec2": tokens("instance", "image", "subnet", "vpc", "security-group", "volume", "network-interface",
		"snapshot", "key-pair", "launch-template", "route-table", "internet-gateway", "nat-gateway",
		"vpc-endpoint", "elastic-ip", "transit-gateway", "transit-gateway-attachment", "vpn-connection",
		"customer-gateway", "vpn-gateway", "placement-group", "dhcp-options", "network-acl", "prefix-list",
		"vpc-peering-connection", "spot-instances-request", "capacity-reservation", "fleet", "host",
		"launch-template-version", "reserved-instances", "vpc-flow-log", "elastic-gpu", "dedicated-host"),
	"iam":                      tokens("role", "user", "group", "policy", "instance-profile", "oidc-provider", "saml-provider", "server-certificate", "mfa", "u2f"),
	"sts":                      tokens("assumed-role", "federated-user"),
	"lambda":                   tokens("function", "layer", "event-source-mapping", "code-signing-config"),
	"ecs":                      tokens("task", "task-definition", "service", "cluster", "container-instance", "capacity-provider", "task-set"),
	"ecr":                      tokens("repository"),
	"eks":                      tokens("cluster", "nodegroup", "fargateprofile", "addon", "podidentityassociation", "identityproviderconfig", "accessentry"),
	"rds":                      tokens("db", "cluster", "snapshot", "cluster-snapshot", "subgrp", "pg", "cluster-pg", "og", "secgrp", "es", "ri", "deployment", "target-group", "proxy", "cluster-endpoint", "auto-backup", "cluster-auto-backup", "db-proxy-endpoint", "cev"),
	"neptune-db":               tokens("cluster"),
	"docdb":                    tokens("db", "cluster"),
	"dynamodb":                 tokens("table", "global-table"),
	"kms":                      tokens("key", "alias"),
	"secretsmanager":           tokens("secret"),
	"ssm":                      tokens("parameter", "document", "managed-instance", "maintenancewindow", "patchbaseline", "association", "session", "automation-execution", "opsitem", "resource-data-sync"),
	"cloudformation":           tokens("stack", "stackset", "changeset", "type"),
	"logs":                     tokens("log-group", "destination"),
	"elasticloadbalancing":     tokens("loadbalancer", "targetgroup", "listener", "listener-rule", "truststore"),
	"route53":                  tokens("hostedzone", "healthcheck", "change", "delegationset", "trafficpolicy", "trafficpolicyinstance", "cidrcollection"),
	"cloudfront":               tokens("distribution", "streaming-distribution", "origin-access-identity", "cache-policy", "origin-request-policy", "response-headers-policy", "function", "field-level-encryption-config", "origin-access-control", "realtime-log-config"),
	"states":                   tokens("stateMachine", "execution", "activity", "express", "mapRun"),
	"elasticache":              tokens("cluster", "replicationgroup", "subnetgroup", "parametergroup", "snapshot", "user", "usergroup", "serverlesscache", "globalreplicationgroup", "reserved-instance"),
	"memorydb":                 tokens("cluster", "subnetgroup", "parametergroup", "snapshot", "user", "acl"),
	"kafka":                    tokens("cluster", "configuration", "vpc-connection", "replicator"),
	"es":                       tokens("domain"),
	"aoss":                     tokens("collection"),
	"glue":                     tokens("database", "table", "job", "crawler", "catalog", "connection", "trigger", "workflow", "devEndpoint", "mlTransform", "registry", "schema", "session", "blueprint", "userDefinedFunction"),
	"athena":                   tokens("workgroup", "datacatalog", "capacity-reservation"),
	"guardduty":                tokens("detector", "filter", "ipset", "threatintelset", "publishingDestination"),
	"securityhub":              tokens("hub", "product", "standards", "subscription", "finding-aggregator", "automation-rule"),
	"access-analyzer":          tokens("analyzer"),
	"organizations":            tokens("account", "ou", "organization", "root", "policy", "handshake"),
	"sso":                      tokens("instance", "permissionSet"),
	"cloudwatch":               tokens("alarm", "dashboard", "insight-rule", "metric-stream", "slo", "service"),
	"events":                   tokens("rule", "event-bus", "archive", "api-destination", "connection", "endpoint", "replay", "event-source"),
	"scheduler":                tokens("schedule", "schedule-group"),
	"backup":                   tokens("backup-vault", "backup-plan", "recovery-point", "framework", "report-plan", "legal-hold", "restore-testing-plan"),
	"batch":                    tokens("job-queue", "job-definition", "compute-environment", "job", "scheduling-policy"),
	"codebuild":                tokens("project", "build", "report-group", "report", "fleet"),
	"codedeploy":               tokens("application", "deploymentgroup", "deploymentconfig", "instance"),
	"codecommit":               tokens(),
	"acm":                      tokens("certificate"),
	"acm-pca":                  tokens("certificate-authority"),
	"elasticfilesystem":        tokens("file-system", "access-point"),
	"fsx":                      tokens("file-system", "backup", "volume", "storage-virtual-machine", "snapshot", "file-cache"),
	"redshift":                 tokens("cluster", "namespace", "workgroup", "snapshot", "subnetgroup", "parametergroup", "dbuser", "dbname", "dbgroup", "eventsubscription", "hsmclientcertificate", "hsmconfiguration", "snapshotcopygrant", "snapshotschedule", "usagelimit"),
	"redshift-serverless":      tokens("namespace", "workgroup", "snapshot", "recoverypoint"),
	"sagemaker":                tokens("notebook-instance", "endpoint", "model", "training-job", "domain", "endpoint-config", "pipeline", "processing-job", "transform-job", "user-profile", "app", "feature-group", "model-package", "model-package-group", "image", "space", "inference-component", "cluster", "project", "experiment"),
	"appsync":                  tokens("apis", "domainnames"),
	"apprunner":                tokens("service", "connection", "autoscalingconfiguration", "observabilityconfiguration", "vpcconnector", "vpcingressconnection"),
	"appmesh":                  tokens("mesh"),
	"mq":                       tokens("broker", "configuration"),
	"opensearch":               tokens("domain"),
	"timestream":               tokens("database"),
	"wafv2":                    tokens("regional", "global"),
	"cognito-idp":              tokens("userpool"),
	"cognito-identity":         tokens("identitypool"),
	"kinesis":                  tokens("stream"),
	"firehose":                 tokens("deliverystream"),
	"kinesisvideo":             tokens("stream", "channel"),
	"elasticbeanstalk":         tokens("application", "applicationversion", "environment", "configurationtemplate", "platform", "solutionstack"),
	"autoscaling":              tokens("autoScalingGroup", "launchConfiguration"),
	"servicediscovery":         tokens("namespace", "service"),
	"servicecatalog":           tokens("product", "portfolio", "provisionedproduct", "application", "attribute-group"),
	"transfer":                 tokens("server", "user", "workflow", "connector", "agreement", "certificate", "profile"),
	"network-firewall":         tokens("firewall", "firewall-policy", "stateful-rulegroup", "stateless-rulegroup", "tls-configuration"),
	"globalaccelerator":        tokens("accelerator"),
	"directconnect":            tokens("dxcon", "dxvif", "dxlag", "dx-gateway"),
	"ram":                      tokens("resource-share", "resource-share-invitation", "permission"),
	"inspector2":               tokens("filter", "finding", "owner"),
	"macie2":                   tokens("classification-job", "custom-data-identifier", "findings-filter", "member", "allow-list"),
	"cloudtrail":               tokens("trail", "eventdatastore", "channel"),
	"config":                   tokens("config-rule", "config-aggregator", "aggregation-authorization", "conformance-pack", "organization-config-rule", "organization-conformance-pack", "remediation-configuration", "stored-query"),
	"xray":                     tokens("group", "sampling-rule"),
	"grafana":                  tokens("/workspaces"),
	"aps":                      tokens("workspace", "rulegroupsnamespace", "scraper"),
	"emr-serverless":           tokens("/applications"),
	"elasticmapreduce":         tokens("cluster", "editor", "studio", "notebook-execution"),
	"dms":                      tokens("rep", "endpoint", "task", "subgrp", "cert", "es", "replication-config"),
	"datasync":                 tokens("agent", "location", "task"),
	"appconfig":                tokens("application", "deploymentstrategy", "extension", "extensionassociation"),
	"bedrock":                  tokens("foundation-model", "custom-model", "provisioned-model", "model-customization-job", "agent", "agent-alias", "knowledge-base", "guardrail", "inference-profile", "evaluation-job", "model-invocation-job"),
	"lakeformation":            tokens("catalog"),
	"license-manager":          tokens("license-configuration", "license", "grant"),
	"lightsail":                tokens("Instance", "StaticIp", "KeyPair", "Domain", "Disk", "LoadBalancer", "Database", "Bucket", "Certificate", "ContainerService", "Distribution"),
	"amplify":                  tokens("apps"),
	"apigateway":               tokens("/restapis", "/apis", "/apikeys", "/usageplans", "/domainnames", "/vpclinks", "/clientcertificates"),
	"imagebuilder":             tokens("image", "image-pipeline", "image-recipe", "component", "container-recipe", "distribution-configuration", "infrastructure-configuration", "workflow", "lifecycle-policy"),
	"signer":                   tokens("/signing-profiles", "/signing-jobs"),
	"rolesanywhere":            tokens("trust-anchor", "profile", "crl", "subject"),
	"verifiedpermissions":      tokens("policy-store"),
	"vpc-lattice":              tokens("service", "servicenetwork", "targetgroup", "accesslogsubscription", "servicenetworkvpcassociation", "servicenetworkserviceassociation", "resourceconfiguration", "resourcegateway"),
	"shield":                   tokens("protection", "protection-group"),
	"fms":                      tokens("policy", "applications-list", "protocols-list", "resource-set"),
	"fis":                      tokens("experiment-template", "experiment", "action", "target-account-configuration"),
	"resiliencehub":            tokens("app", "resiliency-policy", "app-assessment"),
	"drs":                      tokens("source-server", "recovery-instance", "job", "replication-configuration-template", "launch-configuration-template"),
	"mgn":                      tokens("source-server", "job", "replication-configuration-template", "launch-configuration-template", "application", "wave"),
	"outposts":                 tokens("outpost", "site", "order"),
	"storagegateway":           tokens("gateway", "share", "tape", "volume", "tapepool"),
	"synthetics":               tokens("canary", "group"),
	"workspaces":               tokens("workspace", "directory", "workspacebundle", "workspaceimage", "ipgroup", "connectionalias", "workspacespool"),
	"pinpoint":                 tokens("apps", "templates", "recommenders", "journeys", "campaigns", "segments"),
	"proton":                   tokens("environment", "service", "environment-template", "service-template", "repository", "component"),
	"quicksight":               tokens("user", "group", "namespace", "dashboard", "analysis", "dataset", "datasource", "theme", "template", "folder", "topic", "vpcconnection"),
	"ses":                      tokens("identity", "configuration-set", "dedicated-ip-pool", "contact-list", "template", "mailmanager-ingress-point"),
	"cleanrooms":               tokens("collaboration", "membership", "configuredtable", "configuredtableassociation"),
	"controltower":             tokens("enabledcontrol", "landingzone", "baseline", "enabledbaseline"),
	"auditmanager":             tokens("assessment", "assessmentFramework", "assessmentControlSet", "control"),
	"detective":                tokens("graph"),
	"datazone":                 tokens("domain"),
	"databrew":                 tokens("dataset", "job", "project", "recipe", "ruleset", "schedule"),
	"appflow":                  tokens("flow", "connectorprofile", "connectors"),
	"appstream":                tokens("fleet", "stack", "image", "image-builder", "app-block", "application"),
	"cloudhsm":                 tokens("cluster", "backup"),
	"codeguru-reviewer":        tokens("association"),
	"codeguru-profiler":        tokens("profilingGroup"),
	"codeartifact":             tokens("domain", "repository", "package"),
	"compute-optimizer":        tokens("enrollment"),
	"cassandra":                tokens("/keyspace"),
	"kinesisanalytics":         tokens("application"),
	"dax":                      tokens("cache"),
	"docdb-elastic":            tokens("cluster", "cluster-snapshot"),
	"ds":                       tokens("directory"),
	"amp":                      tokens("workspace"),
	"networkmanager":           tokens("global-network", "core-network", "site", "device", "link", "connection", "attachment", "connect-peer"),
	"route53resolver":          tokens("resolver-endpoint", "resolver-rule", "resolver-query-log-config", "firewall-rule-group", "firewall-domain-list", "outpost-resolver"),
	"route53-recovery-control": tokens("cluster", "controlpanel", "routingcontrol", "safetyrule"),
	"servicequotas":            tokens("quota"),
	"resource-groups":          tokens("group"),
	"securitylake":             tokens("data-lake", "subscriber"),
	"location":                 tokens("map", "place-index", "route-calculator", "geofence-collection", "tracker", "api-key"),
	"keyspaces":                tokens("/keyspace"),
	"verified-access":          tokens("verified-access-instance", "verified-access-trust-provider", "verified-access-group", "verified-access-endpoint"),
	"wafregional":              tokens("webacl", "rule", "ipset", "ratebasedrule", "rulegroup"),
}

// arnSecondTokens are structural second-position tokens (kept when the
// first position is a type token): ELBv2 load-balancer types and WAFv2
// scopes' resource kinds.
var arnSecondTokens = map[string]tokenSet{
	"elasticloadbalancing": tokens("app", "net", "gwy"),
	"wafv2":                tokens("webacl", "rulegroup", "ipset", "regexpatternset", "managedruleset"),
}

// awsServiceWords are the ARN service names and collector service kinds
// that are not already keys of arnTypeTokens: the services the aws-cloud
// collector scans (one config test per service in cmd/collector-aws-cloud)
// plus the ARN-only names. AWS-defined words, never customer-chosen.
var awsServiceWords = []string{
	"s3", "sns", "sqs", "apigateway", "apigatewayv2", "execute-api", "appmesh", "autoscaling", "bedrock",
	"cloudtrail", "config", "codepipeline", "cognito", "cognito-idp", "cognito-identity", "directconnect",
	"ds", "efs", "elasticbeanstalk", "emr", "elasticmapreduce", "globalaccelerator", "inspector2", "kinesis",
	"firehose", "macie", "macie2", "mq", "neptune", "network-firewall", "networkfirewall", "opensearch",
	"ram", "route53resolver", "servicediscovery", "ssoadmin", "transitgateway", "vpc", "xray",
	"ecr-public", "eks-auth", "elasticloadbalancing", "s3-object-lambda", "s3-outposts",
}

// awsVocabulary is every AWS-defined structural word: ARN service names,
// resource-type tokens, second-position tokens, AWS host service labels
// and the collector's service kinds. A learned token equal to one would
// rewrite the service or type segment of every scope id, stable key,
// source uri and ARN that carries it, so the dictionary never learns one
// (structural in dictionary.go). Case-sensitive: the words are lowercase
// or camelCase exactly as AWS writes them.
var awsVocabulary = buildAWSVocabulary()

func buildAWSVocabulary() map[string]struct{} {
	out := map[string]struct{}{}
	for service, words := range arnTypeTokens {
		out[service] = struct{}{}
		for word := range words {
			out[word] = struct{}{}
		}
	}
	for service, words := range arnSecondTokens {
		out[service] = struct{}{}
		for word := range words {
			out[word] = struct{}{}
		}
	}
	for label := range awsHostServiceLabels {
		out[label] = struct{}{}
	}
	for _, word := range awsServiceWords {
		out[word] = struct{}{}
	}
	return out
}
