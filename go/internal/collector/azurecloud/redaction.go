package azurecloud

import (
	"sort"
	"strings"
)

// RedactionPolicyVersion identifies the Azure cloud collector sensitive-key and
// data-plane policy attached to redacted azure_cloud_resource extension
// payloads. It is persisted on every fact so downstream reducers and audits can
// prove which redaction policy produced a payload.
const RedactionPolicyVersion = "azure-resource-graph-2026-06-09"

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
