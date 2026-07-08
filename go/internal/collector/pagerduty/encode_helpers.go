// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pagerduty

import (
	"strings"

	incidentv1 "github.com/eshu-hq/eshu/sdk/go/factschema/incident/v1"
)

func mergeContractPayload(payload map[string]any, encode func() (map[string]any, error)) error {
	encoded, err := encode()
	if err != nil {
		return err
	}
	for key, value := range encoded {
		if _, exists := payload[key]; !exists {
			payload[key] = value
		}
	}
	return nil
}

func stringPtr(value string) *string {
	return &value
}

func int64Ptr(value int64) *int64 {
	return &value
}

func serviceReferencePtr(ref Reference) *incidentv1.ServiceReference {
	typed := serviceReference(ref)
	if typed == (incidentv1.ServiceReference{}) {
		return nil
	}
	return &typed
}

func serviceReferences(refs []Reference) []incidentv1.ServiceReference {
	out := make([]incidentv1.ServiceReference, 0, len(refs))
	for _, ref := range refs {
		typed := serviceReference(ref)
		if typed != (incidentv1.ServiceReference{}) {
			out = append(out, typed)
		}
	}
	return out
}

func serviceReference(ref Reference) incidentv1.ServiceReference {
	var typed incidentv1.ServiceReference
	if value := strings.TrimSpace(ref.ID); value != "" {
		typed.ID = stringPtr(value)
	}
	if value := strings.TrimSpace(ref.Type); value != "" {
		typed.Type = stringPtr(value)
	}
	if value := strings.TrimSpace(ref.Summary); value != "" {
		typed.Summary = stringPtr(value)
	}
	if value := safeSourceURI(ref.HTMLURL); value != "" {
		typed.URL = stringPtr(value)
	}
	return typed
}

func changeLinks(links []Link) []incidentv1.ChangeLink {
	out := make([]incidentv1.ChangeLink, 0, len(links))
	for _, link := range links {
		href := safeSourceURI(link.Href)
		text := strings.TrimSpace(link.Text)
		if href == "" && text == "" {
			continue
		}
		out = append(out, incidentv1.ChangeLink{
			Href: stringPtr(href),
			Text: stringPtr(text),
		})
	}
	return out
}

func optionalStringPtrFromPayload(payload map[string]any, key string) *string {
	value, ok := payload[key].(string)
	if !ok {
		return nil
	}
	return &value
}

func optionalBoolPtrFromPayload(payload map[string]any, key string) *bool {
	value, ok := payload[key].(bool)
	if !ok {
		return nil
	}
	return &value
}
