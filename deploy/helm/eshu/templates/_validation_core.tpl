{{- define "eshu.validate.core" -}}
{{- if and .Values.exposure.ingress.enabled .Values.exposure.gateway.enabled -}}
{{- fail "exposure.ingress.enabled and exposure.gateway.enabled cannot both be true" -}}
{{- end -}}
{{- $nornicdbBackendSelected := false -}}
{{- $globalEnv := default (dict) .Values.env -}}
{{- if eq (include "eshu.envMapsSelectNornicDB" (list $globalEnv (default (dict) .Values.api.env))) "true" -}}
{{- $nornicdbBackendSelected = true -}}
{{- end -}}
{{- if and .Values.mcpServer.enabled (eq (include "eshu.envMapsSelectNornicDB" (list $globalEnv (default (dict) .Values.mcpServer.env))) "true") -}}
{{- $nornicdbBackendSelected = true -}}
{{- end -}}
{{- if and .Values.repoSync.enabled (eq (include "eshu.envMapsSelectNornicDB" (list $globalEnv (default (dict) .Values.ingester.env))) "true") -}}
{{- $nornicdbBackendSelected = true -}}
{{- end -}}
{{- if .Values.resolutionEngine.enabled -}}
{{- $resolutionLanes := default (list) .Values.resolutionEngine.lanes -}}
{{- if eq (len $resolutionLanes) 0 -}}
{{- if eq (include "eshu.envMapsSelectNornicDB" (list $globalEnv (default (dict) .Values.resolutionEngine.env))) "true" -}}
{{- $nornicdbBackendSelected = true -}}
{{- end -}}
{{- else -}}
{{- range $lane := $resolutionLanes -}}
{{- if eq (include "eshu.envMapsSelectNornicDB" (list $globalEnv (default (dict) $.Values.resolutionEngine.env) (default (dict) $lane.env))) "true" -}}
{{- $nornicdbBackendSelected = true -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- if and .Values.workflowCoordinator.enabled (eq (include "eshu.envMapsSelectNornicDB" (list $globalEnv (default (dict) .Values.workflowCoordinator.env))) "true") -}}
{{- $nornicdbBackendSelected = true -}}
{{- end -}}
{{- $globalOnlyWorkloads := list .Values.schemaBootstrap .Values.awsCloudCollector .Values.azureCloudCollector .Values.cicdRunCollector .Values.componentExtensionCollector .Values.confluenceCollector .Values.gcpCloudCollector .Values.grafanaCollector .Values.jiraCollector .Values.kubernetesLiveCollector .Values.lokiCollector .Values.ociRegistryCollector .Values.packageRegistryCollector .Values.pagerDutyCollector .Values.prometheusMimirCollector .Values.sbomAttestationCollector .Values.securityAlertCollector .Values.tempoCollector .Values.terraformStateCollector .Values.vaultLiveCollector .Values.vulnerabilityIntelligenceCollector .Values.scannerWorker -}}
{{- range $workload := $globalOnlyWorkloads -}}
{{- if and $workload.enabled (eq (include "eshu.envMapsSelectNornicDB" (list $globalEnv)) "true") -}}
{{- $nornicdbBackendSelected = true -}}
{{- end -}}
{{- end -}}
{{- if and $nornicdbBackendSelected (not .Values.nornicdb.capabilities.relationshipMergePropertyIdentity) -}}
{{- fail "ESHU_GRAPH_BACKEND=nornicdb requires a backend that preserves relationship MERGE identity properties; verify the selected external or bundled endpoint uses orneryd/NornicDB#290 or a later compatible build, then set nornicdb.capabilities.relationshipMergePropertyIdentity=true" -}}
{{- end -}}
{{- if and .Values.nornicdb.enabled .Values.nornicdb.persistence.enabled (eq .Values.nornicdb.persistence.existingClaim (include "eshu.nornicdbLegacyClaimName" .)) -}}
{{- fail "nornicdb.persistence.existingClaim cannot reuse the pre-v1.3.2 NornicDB PVC; stop NornicDB, snapshot or back up the graph, use a fresh v1.3.3-compatible claim, and rebuild the graph from Postgres facts" -}}
{{- end -}}
{{- $generationRetentionDisabled := false -}}
{{- $generationRetentionEnvMaps := list (default (dict) .Values.env) (default (dict) .Values.resolutionEngine.env) -}}
{{- range $envMap := $generationRetentionEnvMaps -}}
{{- if and (hasKey $envMap "ESHU_GENERATION_RETENTION_ENABLED") (eq (lower (trim (toString (get $envMap "ESHU_GENERATION_RETENTION_ENABLED")))) "false") -}}
{{- $generationRetentionDisabled = true -}}
{{- end -}}
{{- end -}}
{{- range $lane := default (list) .Values.resolutionEngine.lanes -}}
{{- $laneEnv := default (dict) $lane.env -}}
{{- if and (hasKey $laneEnv "ESHU_GENERATION_RETENTION_ENABLED") (eq (lower (trim (toString (get $laneEnv "ESHU_GENERATION_RETENTION_ENABLED")))) "false") -}}
{{- $generationRetentionDisabled = true -}}
{{- end -}}
{{- end -}}
{{- if and .Values.resolutionEngine.enabled $generationRetentionDisabled -}}
{{- fail "ESHU_GENERATION_RETENTION_ENABLED=false is not allowed for production resolutionEngine Helm deployments; generation retention must run beside reducer work" -}}
{{- end -}}
{{- if and .Values.contentStore.dsn .Values.contentStore.secretName -}}
{{- fail "contentStore.dsn and contentStore.secretName cannot both be set" -}}
{{- end -}}
{{- if and .Values.contentStore.secretName (not (.Values.contentStore.dsnKey | default "dsn")) -}}
{{- fail "contentStore.dsnKey is required when contentStore.secretName is set" -}}
{{- end -}}
{{- if and (eq .Values.repoSync.auth.method "ssh") (eq .Values.repoSync.source.mode "githubOrg") -}}
{{- fail "repoSync.auth.method=ssh requires repoSync.source.mode=explicit or filesystem" -}}
{{- end -}}
{{- if and .Values.repoSync.enabled (gt (.Values.ingester.replicas | int) 1) -}}
{{- if not (semverCompare ">=1.32.0-0" .Capabilities.KubeVersion.Version) -}}
{{- fail "ingester.replicas greater than 1 requires Kubernetes 1.32 or newer for the stable StatefulSet apps.kubernetes.io/pod-index label" -}}
{{- end -}}
{{- if .Values.ingester.persistence.existingClaim -}}
{{- fail "ingester.persistence.existingClaim cannot be used with ingester.replicas greater than 1; use StatefulSet volumeClaimTemplates so each shard owns one workspace PVC" -}}
{{- end -}}
{{- $shardEnvMaps := list (default (dict) .Values.env) (default (dict) .Values.ingester.env) -}}
{{- range $envMap := $shardEnvMaps -}}
{{- if or (hasKey $envMap "ESHU_REPO_SHARD_COUNT") (hasKey $envMap "ESHU_REPO_SHARD_INDEX") -}}
{{- fail "horizontal ingesters manage ESHU_REPO_SHARD_COUNT and ESHU_REPO_SHARD_INDEX from replicas and pod ordinal; remove static shard env overrides" -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- if and (eq .Values.exposure.ingress.backend "mcp") (not .Values.mcpServer.enabled) -}}
{{- fail "exposure.ingress.backend=mcp requires mcpServer.enabled=true" -}}
{{- end -}}
{{- if and (eq .Values.exposure.gateway.backend "mcp") (not .Values.mcpServer.enabled) -}}
{{- fail "exposure.gateway.backend=mcp requires mcpServer.enabled=true" -}}
{{- end -}}
{{- if and .Values.ociRegistryCollector.enabled (not .Values.ociRegistryCollector.targets) -}}
{{- fail "ociRegistryCollector.targets must contain at least one target when ociRegistryCollector.enabled=true" -}}
{{- end -}}
{{- if .Values.terraformStateCollector.enabled -}}
{{- if not .Values.terraformStateCollector.instanceId -}}
{{- fail "terraformStateCollector.instanceId is required when terraformStateCollector.enabled=true" -}}
{{- end -}}
{{- if not .Values.terraformStateCollector.collectorInstances -}}
{{- fail "terraformStateCollector.collectorInstances must contain at least one instance when terraformStateCollector.enabled=true" -}}
{{- end -}}
{{- if not .Values.terraformStateCollector.redaction.secretName -}}
{{- fail "terraformStateCollector.redaction.secretName is required when terraformStateCollector.enabled=true" -}}
{{- end -}}
{{- if not .Values.terraformStateCollector.redaction.keyKey -}}
{{- fail "terraformStateCollector.redaction.keyKey is required when terraformStateCollector.enabled=true" -}}
{{- end -}}
{{- if not .Values.terraformStateCollector.redaction.rulesetVersion -}}
{{- fail "terraformStateCollector.redaction.rulesetVersion is required when terraformStateCollector.enabled=true" -}}
{{- end -}}
{{- end -}}
{{- end -}}
