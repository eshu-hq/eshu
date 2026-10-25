// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package correlation

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/packageidentity"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

func packageManifestMetadataString(payload map[string]any, key string) string {
	if value := payloadcore.PayloadStr(payload, key); value != "" {
		return value
	}
	raw, ok := payload["entity_metadata"].(map[string]any)
	if !ok {
		return ""
	}
	return payloadcore.PayloadStr(raw, key)
}

// dependencyResolutionStateUnresolved is the state the Gradle, Maven, and sbt
// parsers give a row whose version they could not read as one exact version.
const dependencyResolutionStateUnresolved = "unresolved"

// packageManifestObservedVersion returns the exact version a manifest or
// lockfile row observed, or "" when it observed none. A row a parser marked
// dependency_resolution_state=unresolved never observes a version: the parser
// kept its text as written (a Gradle map-form "1.0:jdk8", a "1.0!" the Gradle
// rule rejects, an sbt identifier it could not resolve), and the exact-version
// check below is a cross-ecosystem backstop that accepts some of those. A
// partial row keeps the backstop's answer, because an sbt row whose scope is a
// placeholder still carries its exact literal. Ecosystems that emit no state
// are unaffected.
func packageManifestObservedVersion(payload map[string]any, packageManager string, lockfile bool) string {
	if !packageManifestCanObserveExactVersion(packageManager, lockfile) {
		return ""
	}
	if strings.EqualFold(strings.TrimSpace(packageManifestMetadataString(payload, "dependency_resolution_state")), dependencyResolutionStateUnresolved) {
		return ""
	}
	for _, key := range []string{"resolved_version", "value"} {
		value := packageManifestMetadataString(payload, key)
		if version, ok := payloadcore.ExactManifestDependencyVersion(value); ok {
			return version
		}
	}
	return ""
}

func packageManifestCanObserveExactVersion(packageManager string, lockfile bool) bool {
	switch packageidentity.NormalizeEcosystem(packageidentity.Ecosystem(packageManager)) {
	case packageidentity.EcosystemCargo, packageidentity.EcosystemNuGet:
		return lockfile
	default:
		return true
	}
}

func packageManifestRequestedRange(payload map[string]any) string {
	return payloadcore.FirstNonBlank(
		packageManifestMetadataString(payload, "requested_range"),
		packageManifestMetadataString(payload, "requested_version"),
		packageManifestMetadataString(payload, "value"),
	)
}

func packageManifestMetadataStrings(payload map[string]any, key string) []string {
	values := payloadOrderedStrings(payload, key)
	if len(values) > 0 {
		return values
	}
	raw, ok := payload["entity_metadata"].(map[string]any)
	if !ok {
		return nil
	}
	return payloadOrderedStrings(raw, key)
}

// payloadOrderedStrings forwards to [payloadcore.PayloadOrderedStrings].
func payloadOrderedStrings(payload map[string]any, key string) []string {
	return payloadcore.PayloadOrderedStrings(payload, key)
}

func packageManifestMetadataInt(payload map[string]any, key string) int {
	if value := packageManifestMetadataString(payload, key); value != "" {
		parsed, _ := strconv.Atoi(value)
		return parsed
	}
	return 0
}

func packageManifestMetadataBool(payload map[string]any, key string) *bool {
	raw := packageManifestMetadataString(payload, key)
	if raw == "" {
		return nil
	}
	value := strings.EqualFold(raw, "true")
	return &value
}

// packageManifestMetadataBoolValue returns the metadata boolean for key as a
// plain bool, defaulting to false when missing. It is used for flags whose
// absence and false value carry the same meaning, such as lockfile and
// source_ambiguous, where only true changes downstream behavior.
func packageManifestMetadataBoolValue(payload map[string]any, key string) bool {
	value := packageManifestMetadataBool(payload, key)
	return value != nil && *value
}

// centralFlagText returns a payload flag as the readiness SQL reads it: a
// missing key and null are empty, only ASCII blanks are trimmed (the set BTRIM
// takes in the postgres package's NotCentralVersionDeclarationSQL), and a
// literal "<nil>" is ordinary text. It differs on purpose from
// packageManifestMetadataString, whose strings.TrimSpace also strips Unicode
// spaces such as NBSP and NEL and whose PayloadStr blanks "<nil>": with that
// rule a NBSP-padded "true" would be a pin here and consumption in the
// sidecar and readiness count. It mirrors the postgres package's helper of
// the same name instead of importing it, because the reducer must not depend
// on storage.
func centralFlagText(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	value, ok := payload[key]
	if !ok || value == nil {
		return ""
	}
	return strings.Trim(fmt.Sprint(value), " \t\n\r\f\v")
}
