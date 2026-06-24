// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package azurecloud

import (
	"strings"
)

var supportedDNSRecordTypes = map[string]struct{}{
	DNSRecordTypeA:     {},
	DNSRecordTypeAAAA:  {},
	DNSRecordTypeCNAME: {},
	DNSRecordTypeMX:    {},
	DNSRecordTypeNS:    {},
	DNSRecordTypePTR:   {},
	DNSRecordTypeTXT:   {},
	DNSRecordTypeSRV:   {},
	DNSRecordTypeCAA:   {},
}

// dnsRecordObservationFromRow derives a DNS fact observation from Resource Graph
// DNS record-set rows. It returns false for unsupported record families or rows
// whose safe properties do not expose a usable target.
func dnsRecordObservationFromRow(boundary Boundary, row ResourceRow) (DNSRecordObservation, bool) {
	recordType, ok := dnsRecordTypeFromRow(row)
	if !ok {
		return DNSRecordObservation{}, false
	}
	targets := dnsRecordTargets(row.Properties, recordType)
	if len(targets) == 0 {
		return DNSRecordObservation{}, false
	}
	zoneID, ok := parentARMID(row.ID, 1)
	if !ok {
		return DNSRecordObservation{}, false
	}
	return DNSRecordObservation{
		Boundary:          boundary,
		ZoneARMResourceID: zoneID,
		RecordType:        recordType,
		RecordName:        dnsRecordName(row),
		Targets:           targets,
		TTLSeconds:        int64Value(caseValue(row.Properties, "ttl")),
		ProviderTime:      row.ProviderTime(),
		SourceRecordID:    row.ID,
	}, true
}

func dnsRecordTypeFromRow(row ResourceRow) (string, bool) {
	segments := strings.Split(strings.ToLower(strings.TrimSpace(row.Type)), "/")
	if len(segments) != 3 || segments[0] != "microsoft.network" {
		return "", false
	}
	if segments[1] != "dnszones" && segments[1] != "privatednszones" {
		return "", false
	}
	recordType := strings.ToUpper(segments[2])
	if _, ok := supportedDNSRecordTypes[recordType]; !ok {
		return "", false
	}
	return recordType, true
}

func dnsRecordName(row ResourceRow) string {
	if name := strings.TrimSpace(row.Name); name != "" {
		parts := strings.Split(name, "/")
		return parts[len(parts)-1]
	}
	identity, err := ParseARMIdentity(row.ID)
	if err != nil {
		return ""
	}
	return identity.ResourceName
}

func dnsRecordTargets(properties map[string]any, recordType string) []string {
	switch recordType {
	case DNSRecordTypeA:
		return recordArrayTargets(caseValue(properties, "aRecords"), "ipv4Address")
	case DNSRecordTypeAAAA:
		return recordArrayTargets(caseValue(properties, "aaaaRecords"), "ipv6Address")
	case DNSRecordTypeCNAME:
		return compactStrings(stringValue(caseMapValue(properties, "cnameRecord"), "cname"))
	case DNSRecordTypeMX:
		return recordArrayTargets(caseValue(properties, "mxRecords"), "exchange")
	case DNSRecordTypeNS:
		return recordArrayTargets(caseValue(properties, "nsRecords"), "nsdname")
	case DNSRecordTypePTR:
		return recordArrayTargets(caseValue(properties, "ptrRecords"), "ptrdname")
	case DNSRecordTypeTXT:
		return txtRecordTargets(caseValue(properties, "txtRecords"))
	case DNSRecordTypeSRV:
		return recordArrayTargets(caseValue(properties, "srvRecords"), "target")
	case DNSRecordTypeCAA:
		return recordArrayTargets(caseValue(properties, "caaRecords"), "value")
	default:
		return nil
	}
}

func recordArrayTargets(raw any, field string) []string {
	records, ok := raw.([]any)
	if !ok {
		return nil
	}
	var out []string
	for _, record := range records {
		if value := stringValue(anyMap(record), field); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func txtRecordTargets(raw any) []string {
	records, ok := raw.([]any)
	if !ok {
		return nil
	}
	var out []string
	for _, record := range records {
		values, ok := caseValue(anyMap(record), "value").([]any)
		if !ok {
			continue
		}
		var parts []string
		for _, value := range values {
			if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
				parts = append(parts, strings.TrimSpace(text))
			}
		}
		if len(parts) > 0 {
			out = append(out, strings.Join(parts, ""))
		}
	}
	return out
}

// imageReferenceObservationsFromRow derives runtime image reference observations
// only from Azure resource families whose control-plane shape exposes container
// images directly. It intentionally ignores AKS cluster rows because cluster
// metadata is not workload image truth.
func imageReferenceObservationsFromRow(boundary Boundary, row ResourceRow) []ImageReferenceObservation {
	if !strings.EqualFold(strings.TrimSpace(row.Type), "microsoft.app/containerapps") {
		return nil
	}
	containers, ok := caseValue(caseMapValue(row.Properties, "template"), "containers").([]any)
	if !ok {
		return nil
	}
	seen := map[string]struct{}{}
	var out []ImageReferenceObservation
	for _, container := range containers {
		entry := anyMap(container)
		image := strings.TrimSpace(stringValue(entry, "image"))
		if image == "" {
			continue
		}
		digest := imageDigest(image)
		key := image + "|" + digest
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ImageReferenceObservation{
			Boundary:            boundary,
			OwningARMResourceID: row.ID,
			ImageReference:      image,
			ImageDigest:         digest,
			ContainerName:       stringValue(entry, "name"),
			ProviderTime:        row.ProviderTime(),
			SourceRecordID:      row.ID + "|" + image,
		})
	}
	return out
}

// resourceExtensionFromRow returns the generic azure_cloud_resource extension
// payload for a Resource Graph row. DNS record-set properties contain record
// targets, so those rows use only the dedicated azure_dns_record fact where
// names and targets are fingerprinted.
func resourceExtensionFromRow(row ResourceRow) map[string]any {
	if _, ok := dnsRecordTypeFromRow(row); ok {
		return nil
	}
	return row.Properties
}

func imageDigest(image string) string {
	_, digest, ok := strings.Cut(image, "@")
	if !ok {
		return ""
	}
	return strings.TrimSpace(digest)
}

func parentARMID(armID string, pairs int) (string, bool) {
	segments := strings.Split(strings.Trim(strings.TrimSpace(armID), "/"), "/")
	if len(segments) < pairs*2 {
		return "", false
	}
	parent := "/" + strings.Join(segments[:len(segments)-(pairs*2)], "/")
	if _, err := ParseARMIdentity(parent); err != nil {
		return "", false
	}
	return parent, true
}

func caseMapValue(values map[string]any, key string) map[string]any {
	return anyMap(caseValue(values, key))
}

func caseValue(values map[string]any, key string) any {
	if values == nil {
		return nil
	}
	for candidate, value := range values {
		if strings.EqualFold(candidate, key) {
			return value
		}
	}
	return nil
}

func stringValue(values map[string]any, key string) string {
	value, _ := caseValue(values, key).(string)
	return strings.TrimSpace(value)
}

func int64Value(value any) int64 {
	switch typed := value.(type) {
	case int64:
		return typed
	case int:
		return int64(typed)
	case float64:
		return int64(typed)
	default:
		return 0
	}
}

func anyMap(value any) map[string]any {
	typed, _ := value.(map[string]any)
	return typed
}

func compactStrings(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
