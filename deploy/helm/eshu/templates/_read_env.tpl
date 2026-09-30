{{/* Extra API/MCP EnvVar entries. Only the reader DSN has a render-time conflict guard. */}}
{{- define "eshu.renderReadExtraEnv" -}}
{{- $service := .service -}}
{{- $global := default dict .env -}}
{{- $local := default dict .serviceEnv -}}
{{- $seenReader := false -}}
{{- range $entry := .extraEnv -}}
{{- if eq (toString (get $entry "name")) "ESHU_POSTGRES_READ_DSN" -}}
{{- if $seenReader -}}
{{- fail (printf "%s.extraEnv repeats ESHU_POSTGRES_READ_DSN" $service) -}}
{{- end -}}
{{- $seenReader = true -}}
{{- if or (hasKey $global "ESHU_POSTGRES_READ_DSN") (hasKey $local "ESHU_POSTGRES_READ_DSN") -}}
{{- fail (printf "%s.extraEnv ESHU_POSTGRES_READ_DSN conflicts with env" $service) -}}
{{- end -}}
{{- $valueFrom := default dict (get $entry "valueFrom") -}}
{{- $secret := default dict (get $valueFrom "secretKeyRef") -}}
{{- if eq (toString (get $secret "optional")) "true" -}}
{{- fail (printf "%s.extraEnv ESHU_POSTGRES_READ_DSN secretKeyRef.optional=true is not allowed" $service) -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- toYaml .extraEnv -}}
{{- end -}}
