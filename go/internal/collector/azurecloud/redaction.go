// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package azurecloud

import (
	"slices"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/redact"
)

// RedactionPolicyVersion identifies the Azure cloud collector sensitive-key and
// data-plane policy attached to redacted azure_cloud_resource extension
// payloads. It is persisted on every fact so downstream reducers and audits can
// prove which redaction policy produced a payload.
const RedactionPolicyVersion = "azure-resource-graph-2026-06-09"

// redactionReasonTagValue is the stable classification label attached to
// fingerprinted Azure tag values so audits can prove why the value was redacted.
const redactionReasonTagValue = "azure_tag_value"

// maxTagObservationEntries bounds the number of tags carried on a single
// azure_tag_observation fact. A pathological resource with thousands of tags
// cannot emit an unbounded payload; excess tags are dropped deterministically
// (by sorted key) and the fact records truncation as explicit evidence.
const maxTagObservationEntries = 50

// azureForbiddenExtensionTokens lists case/separator sub-tokens that mark an
// extension field as secret-like or data-plane. Any extension key whose token
// windows match one of these is dropped, not merely masked, so secret values
// and provider response bodies never reach durable facts. The policy is
// deny-by-token: a key is dropped if any contiguous token window of its name
// matches an entry.
var azureForbiddenExtensionTokens = mustTokenSet([]string{
	"password",
	"passwd",
	"pwd",
	"secret",
	"secrets",
	"key", // matches accessKey, primaryKey, sharedKey, etc.
	"keys",
	"token",
	"credential",
	"credentials",
	"connectionstring",
	"connection_string",
	"sastoken",
	"sas",
	"certificate",
	"privatekey",
	"publickey",
	"template", // ARM deployment templates
	"templatelink",
	"parameters", // deployment parameter values
	"responsebody",
	"requestbody",
	"body",
	"payload",
	"publicip",
	"publicipaddress",
	"privateip",
	"privateipaddress",
	"ipaddress",
	"ipconfiguration",
	"fqdn",
	"hostname",
	"endpoint", // private endpoint hostnames
	"privateendpoint",
	"customdata", // VM custom data / cloud-init can embed secrets
	"userdata",
	"adminpassword",
	"administratorloginpassword",
})

// ExtensionRedaction is the result of redacting a provider extension payload.
// Extension is the surviving safe metadata; RedactedKeys lists the dropped
// keys (sorted, deterministic) for audit; Redacted reports whether any key was
// dropped.
type ExtensionRedaction struct {
	// Extension is the redacted, persistence-safe extension object.
	Extension map[string]any
	// RedactedKeys lists the dropped keys in deterministic order.
	RedactedKeys []string
	// Redacted reports whether any key was dropped.
	Redacted bool
}

// RedactExtension returns a persistence-safe copy of an Azure provider
// extension object. Keys whose names match the forbidden secret or data-plane
// token policy are dropped entirely, including inside nested maps, and recorded
// in RedactedKeys. A nil input yields a nil extension and no redaction.
//
// Dropping (rather than masking) is deliberate: the Azure contract forbids
// persisting deployment templates, secret values, connection strings, access
// keys, tokens, IP addresses, private endpoint hostnames, and provider response
// bodies, so the safest posture is to never carry the value at all.
func RedactExtension(raw map[string]any) ExtensionRedaction {
	if raw == nil {
		return ExtensionRedaction{}
	}
	dropped := map[string]struct{}{}
	safe := redactMap(raw, dropped)
	keys := make([]string, 0, len(dropped))
	for key := range dropped {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return ExtensionRedaction{
		Extension:    safe,
		RedactedKeys: keys,
		Redacted:     len(keys) > 0,
	}
}

// hasUsableTag reports whether a tag map has at least one non-blank key, i.e.
// whether NewTagObservationEnvelope would produce a fact rather than reject the
// input as missing evidence. The emission path uses it to skip untagged
// resources without treating the builder's empty-input error as fatal.
func hasUsableTag(tags map[string]string) bool {
	for k := range tags {
		if strings.TrimSpace(k) != "" {
			return true
		}
	}
	return false
}

// FingerprintTagValues fingerprints Azure tag values into deterministic,
// key-derived markers while preserving the tag keys, so reducers can correlate
// resources that share a tag value without tag value text ever becoming a graph
// property (Azure cloud collector contract: "compare Azure tags without making
// tag value text a graph property").
//
// Keys are trimmed and de-duplicated; blank keys are dropped. The result is
// bounded to maxTagObservationEntries by sorted key, so a resource with an
// unbounded tag set cannot emit an unbounded fact. It returns the
// key-to-fingerprint map, the sorted retained keys, and whether the input was
// truncated. A nil/empty input (or a key set that is entirely blank) yields a
// nil map and no truncation, so callers can treat "no tags" as missing evidence.
func FingerprintTagValues(tags map[string]string, key redact.Key) (map[string]string, []string, bool) {
	if len(tags) == 0 {
		return nil, nil, false
	}
	trimmed := make(map[string]string, len(tags))
	for k, v := range tags {
		kt := strings.TrimSpace(k)
		if kt == "" {
			continue
		}
		trimmed[kt] = v
	}
	if len(trimmed) == 0 {
		return nil, nil, false
	}
	keys := make([]string, 0, len(trimmed))
	for k := range trimmed {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	truncated := false
	if len(keys) > maxTagObservationEntries {
		keys = keys[:maxTagObservationEntries]
		truncated = true
	}
	out := make(map[string]string, len(keys))
	for _, k := range keys {
		out[k] = redact.String(trimmed[k], redactionReasonTagValue, "azure_tag:"+k, key).Marker
	}
	return out, slices.Clone(keys), truncated
}

func redactMap(input map[string]any, dropped map[string]struct{}) map[string]any {
	output := make(map[string]any, len(input))
	for key, value := range input {
		if keyIsForbidden(key) {
			dropped[key] = struct{}{}
			continue
		}
		switch typed := value.(type) {
		case map[string]any:
			output[key] = redactMap(typed, dropped)
		case []any:
			output[key] = redactSlice(typed, dropped)
		default:
			output[key] = typed
		}
	}
	return output
}

func redactSlice(input []any, dropped map[string]struct{}) []any {
	output := make([]any, 0, len(input))
	for _, value := range input {
		switch typed := value.(type) {
		case map[string]any:
			output = append(output, redactMap(typed, dropped))
		case []any:
			output = append(output, redactSlice(typed, dropped))
		default:
			output = append(output, typed)
		}
	}
	return output
}

// keyIsForbidden reports whether any contiguous lowercased token window of a key
// matches the forbidden policy. Windowing catches compound keys such as
// "primaryAccessKey" (window "access_key" / token "key") and
// "administratorLoginPassword" (token "password").
func keyIsForbidden(key string) bool {
	tokens := splitKeyTokens(key)
	var window strings.Builder
	for start := range tokens {
		window.Reset()
		for index := start; index < len(tokens); index++ {
			window.WriteString(tokens[index])
			if _, ok := azureForbiddenExtensionTokens[window.String()]; ok {
				return true
			}
		}
	}
	return false
}

// splitKeyTokens lowercases and splits a key on camelCase boundaries and common
// separators into component words.
func splitKeyTokens(key string) []string {
	var tokens []string
	var current strings.Builder
	flush := func() {
		if current.Len() > 0 {
			tokens = append(tokens, current.String())
			current.Reset()
		}
	}
	runes := []rune(key)
	for index, char := range runes {
		switch {
		case char == '_' || char == '-' || char == '.' || char == '/' || char == ' ':
			flush()
		case char >= 'A' && char <= 'Z':
			if index > 0 {
				previous := runes[index-1]
				var next rune
				if index+1 < len(runes) {
					next = runes[index+1]
				}
				if (previous >= 'a' && previous <= 'z') ||
					(previous >= 'A' && previous <= 'Z' && next >= 'a' && next <= 'z') {
					flush()
				}
			}
			current.WriteRune(char - 'A' + 'a')
		default:
			current.WriteRune(char)
		}
	}
	flush()
	return tokens
}

func mustTokenSet(tokens []string) map[string]struct{} {
	set := make(map[string]struct{}, len(tokens))
	for _, token := range tokens {
		set[strings.ToLower(strings.TrimSpace(token))] = struct{}{}
	}
	return set
}
