{{/*
Validation for componentExtensionCollector.

Every value that reaches an init-container script, a file path, or a resource
name is checked against a strict pattern here, so the rendered script never
interpolates anything that could carry shell syntax.
*/}}

{{/* Legacy single-worker checks: the operator mounts the registry volume by hand. */}}
{{- define "eshu.validate.componentExtensionLegacyMounts" -}}
{{- if not .Values.workflowCoordinator.extraVolumes -}}
{{- fail "workflowCoordinator.extraVolumes must mount the component registry when componentExtensionCollector.enabled=true" -}}
{{- end -}}
{{- if not .Values.workflowCoordinator.extraVolumeMounts -}}
{{- fail "workflowCoordinator.extraVolumeMounts must mount componentExtensionCollector.componentHome when componentExtensionCollector.enabled=true" -}}
{{- end -}}
{{- if not .Values.componentExtensionCollector.extraVolumes -}}
{{- fail "componentExtensionCollector.extraVolumes must mount the component registry when componentExtensionCollector.enabled=true" -}}
{{- end -}}
{{- if not .Values.componentExtensionCollector.extraVolumeMounts -}}
{{- fail "componentExtensionCollector.extraVolumeMounts must mount componentExtensionCollector.componentHome when componentExtensionCollector.enabled=true" -}}
{{- end -}}
{{- $coordinatorComponentHomeMounted := false -}}
{{- range .Values.workflowCoordinator.extraVolumeMounts -}}
{{- if eq (default "" .mountPath) $.Values.componentExtensionCollector.componentHome -}}
{{- $coordinatorComponentHomeMounted = true -}}
{{- end -}}
{{- end -}}
{{- if not $coordinatorComponentHomeMounted -}}
{{- fail "workflowCoordinator.extraVolumeMounts must include componentExtensionCollector.componentHome when componentExtensionCollector.enabled=true" -}}
{{- end -}}
{{- $workerComponentHomeMounted := false -}}
{{- range .Values.componentExtensionCollector.extraVolumeMounts -}}
{{- if eq (default "" .mountPath) $.Values.componentExtensionCollector.componentHome -}}
{{- $workerComponentHomeMounted = true -}}
{{- end -}}
{{- end -}}
{{- if not $workerComponentHomeMounted -}}
{{- fail "componentExtensionCollector.extraVolumeMounts must include componentExtensionCollector.componentHome when componentExtensionCollector.enabled=true" -}}
{{- end -}}
{{- end -}}

{{/*
Prefixes of well-known credential formats: GitHub, Slack, AWS, an sk- API key
and a PEM block. A best-effort heuristic: it catches a pasted token, not every
secret. Credentials belong only in an activation's extraEnv secretKeyRef.
*/}}
{{- define "eshu.validate.componentActivationTokenPattern" -}}
^(ghp_|gho_|ghs_|github_pat_|xox[abprs]-|AKIA[0-9A-Z]{16}|sk-[A-Za-z0-9]{20}|-----BEGIN )
{{- end -}}

{{/* Recursively rejects secret-looking keys and credential-shaped values. */}}
{{- define "eshu.validate.componentActivationNoSecrets" -}}
{{- $path := .path -}}
{{- $value := .value -}}
{{- if kindIs "map" $value -}}
{{- range $key, $child := $value -}}
{{- if and (regexMatch "(?i)(password|passwd|secret|token|api[-_]?key|credential|private[-_]?key|authorization|bearer)" (toString $key)) (not (regexMatch "(?i)(_env|env|_env_name|envname|_ref|ref)$" (toString $key))) -}}
{{- fail (printf "%s.%s looks like a credential; activation config is stored in a ConfigMap, so pass secrets through extraEnv secretKeyRef and put only the env var NAME in config" $path $key) -}}
{{- end -}}
{{- include "eshu.validate.componentActivationNoSecrets" (dict "path" (printf "%s.%s" $path $key) "value" $child) -}}
{{- end -}}
{{- else if kindIs "slice" $value -}}
{{- range $index, $child := $value -}}
{{- include "eshu.validate.componentActivationNoSecrets" (dict "path" (printf "%s[%d]" $path $index) "value" $child) -}}
{{- end -}}
{{- else if kindIs "string" $value -}}
{{- if regexMatch (include "eshu.validate.componentActivationTokenPattern" .) (trim $value) -}}
{{- fail (printf "%s holds a credential-shaped value; activation config is stored in a ConfigMap, so pass secrets through extraEnv secretKeyRef" $path) -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "eshu.validate.componentActivation" -}}
{{- $root := .root -}}
{{- $activation := .activation -}}
{{- $at := printf "componentExtensionCollector.activations[%d]" .index -}}
{{- if not (kindIs "map" $activation) -}}
{{- fail (printf "%s must be an object" $at) -}}
{{- end -}}
{{- $id := toString (default "" $activation.instanceId) -}}
{{- if eq $id "" -}}
{{- fail (printf "%s.instanceId is required" $at) -}}
{{- end -}}
{{- if not (regexMatch "^[a-z0-9]([-a-z0-9]*[a-z0-9])?$" $id) -}}
{{- fail (printf "%s.instanceId %q must be a lowercase DNS label (letters, digits, hyphen)" $at $id) -}}
{{- end -}}
{{- if gt (len $id) 30 -}}
{{- fail (printf "%s.instanceId %q must be at most 30 characters" $at $id) -}}
{{- end -}}
{{- $name := include "eshu.componentActivationFullname" (dict "root" $root "activation" $activation) -}}
{{- if gt (len $name) 55 -}}
{{- fail (printf "%s.instanceId %q makes the resource name %q longer than 55 characters (the metrics Service adds -metrics); shorten the instance ID or the release name" $at $id $name) -}}
{{- end -}}
{{- $componentId := toString (default "" $activation.componentId) -}}
{{- if not (regexMatch "^[a-z0-9][a-z0-9._-]*$" $componentId) -}}
{{- fail (printf "%s.componentId is required and must be a lowercase component identifier" $at) -}}
{{- end -}}
{{- $allowed := list -}}
{{- range splitList "," (include "eshu.componentCSVList" $root.Values.componentExtensionCollector.allowIds) -}}
{{- $allowed = append $allowed . -}}
{{- end -}}
{{- if not (has $componentId $allowed) -}}
{{- fail (printf "%s.componentId %q must be listed in componentExtensionCollector.allowIds" $at $componentId) -}}
{{- end -}}
{{- if not (regexMatch "^[0-9]+\\.[0-9]+\\.[0-9]+([-+][0-9A-Za-z.-]+)?$" (toString (default "" $activation.componentVersion))) -}}
{{- fail (printf "%s.componentVersion is required and must be the semantic version in the seeded manifest" $at) -}}
{{- end -}}
{{- $seed := default (dict) $activation.seed -}}
{{- $seedImage := toString (default "" $seed.image) -}}
{{- if eq $seedImage "" -}}
{{- fail (printf "%s.seed.image is required" $at) -}}
{{- end -}}
{{- if not (regexMatch "^[a-z0-9][A-Za-z0-9._-]*(:[0-9]+(/[A-Za-z0-9._-]+)+|(/[A-Za-z0-9._-]+)*)@sha256:[a-f0-9]{64}$" $seedImage) -}}
{{- fail (printf "%s.seed.image %q must be pinned by digest (repository@sha256:<64 hex>); tags are not accepted, not even next to a digest, because --image-ref would carry the tag" $at $seedImage) -}}
{{- end -}}
{{- if hasKey $seed "pullPolicy" -}}
{{- if not (has (toString $seed.pullPolicy) (list "Always" "IfNotPresent" "Never")) -}}
{{- fail (printf "%s.seed.pullPolicy %q must be Always, IfNotPresent or Never" $at (toString $seed.pullPolicy)) -}}
{{- end -}}
{{- end -}}
{{- if hasKey $activation "replicas" -}}
{{- $replicas := $activation.replicas -}}
{{- if not (or (kindIs "float64" $replicas) (kindIs "int64" $replicas) (kindIs "int" $replicas)) -}}
{{- fail (printf "%s.replicas %q must be a whole number" $at (toString $replicas)) -}}
{{- end -}}
{{- if or (lt (float64 $replicas) (float64 0)) (ne (float64 $replicas) (float64 (int $replicas))) -}}
{{- fail (printf "%s.replicas %v must be a whole number of at least 0" $at $replicas) -}}
{{- end -}}
{{- end -}}
{{- range $arg := default (list) $seed.command -}}
{{- if not (regexMatch "^[A-Za-z0-9._/:=@-]+$" (toString $arg)) -}}
{{- fail (printf "%s.seed.command entries may only contain letters, digits and ._/:=@-" $at) -}}
{{- end -}}
{{- end -}}
{{- $host := default (dict) $activation.host -}}
{{- $hostScope := default (dict) $host.scope -}}
{{- if not (regexMatch "^[A-Za-z0-9._-]+$" (toString (default "" $host.sourceSystem))) -}}
{{- fail (printf "%s.host.sourceSystem is required (letters, digits, ._-)" $at) -}}
{{- end -}}
{{- if not (regexMatch "^[A-Za-z0-9._:/@-]+$" (toString (default "" $hostScope.id))) -}}
{{- fail (printf "%s.host.scope.id is required (letters, digits, ._:/@-)" $at) -}}
{{- end -}}
{{- if not (regexMatch "^[a-z0-9_.-]+$" (toString (default "" $hostScope.kind))) -}}
{{- fail (printf "%s.host.scope.kind is required (lowercase letters, digits, ._-)" $at) -}}
{{- end -}}
{{- if not (regexMatch "^[a-z0-9_-]+$" (toString (default "scheduled" $activation.mode))) -}}
{{- fail (printf "%s.mode must be a lowercase identifier" $at) -}}
{{- end -}}
{{- $process := default (dict) $activation.process -}}
{{- include "eshu.validate.componentActivationLease" (dict "root" $root "activation" $activation "at" $at) -}}
{{- include "eshu.validate.componentActivationNoSecrets" (dict "path" (printf "%s.process" $at) "value" $process) -}}
{{- range $entry := default (list) $process.env -}}
{{- $parts := splitList "=" (toString $entry) -}}
{{- $envName := first $parts -}}
{{- $envValue := join "=" (rest $parts) -}}
{{- if regexMatch "(?i)(password|passwd|secret|token|api[-_]?key|credential|private[-_]?key|authorization|bearer)" $envName -}}
{{- fail (printf "%s.process.env entry %q looks like a credential; process.env is stored in a ConfigMap, so pass secrets through extraEnv secretKeyRef" $at $envName) -}}
{{- end -}}
{{- if regexMatch (include "eshu.validate.componentActivationTokenPattern" $) (trim $envValue) -}}
{{- fail (printf "%s.process.env entry %q holds a credential-shaped value; process.env is stored in a ConfigMap, so pass secrets through extraEnv secretKeyRef" $at $envName) -}}
{{- end -}}
{{- end -}}
{{- include "eshu.validate.componentActivationNoSecrets" (dict "path" (printf "%s.config" $at) "value" (default (dict) $activation.config)) -}}
{{- $grants := default (list) $activation.grants -}}
{{- range $gi, $grant := $grants -}}
{{- $gat := printf "%s.grants[%d]" $at $gi -}}
{{- if not (regexMatch "^[a-z0-9_.]+$" (toString (default "" $grant.kind))) -}}
{{- fail (printf "%s.kind is required (lowercase letters, digits, _.)" $gat) -}}
{{- end -}}
{{- if not $grant.schemaVersions -}}
{{- fail (printf "%s.schemaVersions must list at least one schema version" $gat) -}}
{{- end -}}
{{- range $version := $grant.schemaVersions -}}
{{- if not (regexMatch "^[0-9]+\\.[0-9]+\\.[0-9]+$" (toString $version)) -}}
{{- fail (printf "%s.schemaVersions entry %q must be major.minor.patch" $gat $version) -}}
{{- end -}}
{{- end -}}
{{- if not (regexMatch "^[A-Za-z0-9._:-]+$" (toString (default "" $grant.scope))) -}}
{{- fail (printf "%s.scope is required (letters, digits, ._:-)" $gat) -}}
{{- end -}}
{{- if not (regexMatch "^[1-9][0-9]*(h|m|s)([0-9]+(h|m|s))*$" (toString (default "720h" $grant.expiresIn))) -}}
{{- fail (printf "%s.expiresIn must be a positive Go duration such as 720h" $gat) -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
Seconds in a Go duration written with h, m and s only (for example 5m or 1h30m).
Returns 0 for anything else, which the caller reports as invalid.
*/}}
{{- define "eshu.validate.durationSeconds" -}}
{{- $text := toString . -}}
{{- $total := 0 -}}
{{- if regexMatch "^([0-9]+(h|m|s))+$" $text -}}
{{- range $part := regexFindAll "[0-9]+[hms]" $text -1 -}}
{{- $number := int (trimSuffix "h" (trimSuffix "m" (trimSuffix "s" $part))) -}}
{{- if hasSuffix "h" $part -}}{{- $total = add $total (mul $number 3600) -}}
{{- else if hasSuffix "m" $part -}}{{- $total = add $total (mul $number 60) -}}
{{- else -}}{{- $total = add $total $number -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- $total -}}
{{- end -}}

{{/*
The host kills the collector process at the INITIAL claim lease expiry and
heartbeats do not extend it, so the lease must be explicit and the heartbeat
must be shorter than it (claimed_service.go). Defaults when unset are the
worker's own: 60s lease, 20s heartbeat.
*/}}
{{- define "eshu.validate.componentActivationLease" -}}
{{- $at := .at -}}
{{- $activation := .activation -}}
{{- $global := .root.Values.componentExtensionCollector -}}
{{- $ttl := toString (default "60s" (default $global.claimLeaseTTL $activation.claimLeaseTTL)) -}}
{{- $heartbeat := toString (default "20s" (default $global.heartbeatInterval $activation.heartbeatInterval)) -}}
{{- $ttlSeconds := int (include "eshu.validate.durationSeconds" $ttl) -}}
{{- $heartbeatSeconds := int (include "eshu.validate.durationSeconds" $heartbeat) -}}
{{- if eq $ttlSeconds 0 -}}
{{- fail (printf "%s claimLeaseTTL %q must be a positive duration written with h, m and s (for example 5m)" $at $ttl) -}}
{{- end -}}
{{- if eq $heartbeatSeconds 0 -}}
{{- fail (printf "%s heartbeatInterval %q must be a positive duration written with h, m and s (for example 30s)" $at $heartbeat) -}}
{{- end -}}
{{- if ge $heartbeatSeconds $ttlSeconds -}}
{{- fail (printf "%s heartbeatInterval %s must be less than claimLeaseTTL %s" $at $heartbeat $ttl) -}}
{{- end -}}
{{- end -}}

{{/* Validates the activations list as a whole when it is non-empty. */}}
{{- define "eshu.validate.componentActivations" -}}
{{- $activations := default (list) .Values.componentExtensionCollector.activations -}}
{{- if and (gt (len $activations) 0) .Values.componentExtensionCollector.enabled -}}
{{- if .Values.componentExtensionCollector.instanceId -}}
{{- fail "componentExtensionCollector.instanceId cannot be set together with componentExtensionCollector.activations; each activation carries its own instanceId" -}}
{{- end -}}
{{- if or .Values.componentExtensionCollector.extraVolumes .Values.componentExtensionCollector.extraVolumeMounts -}}
{{- fail "componentExtensionCollector.extraVolumes and extraVolumeMounts cannot be set together with componentExtensionCollector.activations; the chart owns the registry volume, and each activation takes its own extraVolumes" -}}
{{- end -}}
{{- if .Values.componentExtensionCollector.extraEnv -}}
{{- fail "componentExtensionCollector.extraEnv cannot be set together with componentExtensionCollector.activations; set extraEnv on the activation that needs the credential so one collector never sees another's secret" -}}
{{- end -}}
{{- $seenIDs := dict -}}
{{- $versionByComponent := dict -}}
{{- $imageByComponent := dict -}}
{{- range $index, $activation := $activations -}}
{{- include "eshu.validate.componentActivation" (dict "root" $ "activation" $activation "index" $index) -}}
{{- if hasKey $seenIDs $activation.instanceId -}}
{{- fail (printf "componentExtensionCollector.activations[%d].instanceId %q is used more than once" $index $activation.instanceId) -}}
{{- end -}}
{{- $_ := set $seenIDs $activation.instanceId true -}}
{{- if hasKey $versionByComponent $activation.componentId -}}
{{- if ne (get $versionByComponent $activation.componentId) $activation.componentVersion -}}
{{- fail (printf "componentExtensionCollector.activations[%d] pins %s at version %s but an earlier activation pins version %s; one component ID must use one version" $index $activation.componentId $activation.componentVersion (get $versionByComponent $activation.componentId)) -}}
{{- end -}}
{{- if ne (get $imageByComponent $activation.componentId) $activation.seed.image -}}
{{- fail (printf "componentExtensionCollector.activations[%d] uses a different seed.image than an earlier activation of %s; one component ID must use one seed image" $index $activation.componentId) -}}
{{- end -}}
{{- end -}}
{{- $_ := set $versionByComponent $activation.componentId $activation.componentVersion -}}
{{- $_ := set $imageByComponent $activation.componentId $activation.seed.image -}}
{{- end -}}
{{- end -}}
{{- end -}}
