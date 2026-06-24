// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contentrefs

import (
	"reflect"
	"testing"
)

func TestHostnamesRejectsDottedConfigKeysAndFieldPaths(t *testing.T) {
	t.Parallel()

	got := Hostnames(`
base_url: "https://sample-service-api.qa.example.test/v1"
public_url: "https://portal.example/status"
compound: "https://gateway.example/status", "settings.retry.count"
hostname: "app.config.retry.count"
endpoint: "fixture.response.body.items.id"
url: "search.fields.title.keyword"
host: "catalog.service"
`)
	want := []string{"gateway.example", "portal.example", "sample-service-api.qa.example.test"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Hostnames() = %#v, want %#v", got, want)
	}
}

func TestHostnamesAcceptsPublicIDAndNameTLDs(t *testing.T) {
	t.Parallel()

	got := Hostnames(`
host: "portal.id"
hostname: "directory.name"
`)
	want := []string{"directory.name", "portal.id"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Hostnames() = %#v, want %#v", got, want)
	}
}

func TestHostnameCandidatesClassifyRejectedAndAmbiguousEvidence(t *testing.T) {
	t.Parallel()

	got := HostnameCandidates(`
base_url: "https://sample-service-api.qa.example.test/v1"
public_url: "https://portal.example/status"
compound: "https://gateway.example/status", "settings.retry.count"
hostname: "app.config.retry.count"
endpoint: "fixture.response.body.items.id"
url: "search.fields.title.keyword"
host: "catalog.service"
`)
	want := []HostnameCandidate{
		{
			Value:          "app.config.retry.count",
			Classification: "rejected_config_key",
			Reason:         "dotted_config_key",
		},
		{
			Value:          "catalog.service",
			Classification: "ambiguous",
			Reason:         "two_label_hostname_candidate",
		},
		{
			Value:          "fixture.response.body.items.id",
			Classification: "rejected_field_path",
			Reason:         "dotted_field_path",
		},
		{
			Value:          "gateway.example",
			Classification: "exact_hostname",
			Reason:         "url_hostname_reference",
		},
		{
			Value:          "portal.example",
			Classification: "exact_hostname",
			Reason:         "url_hostname_reference",
		},
		{
			Value:          "sample-service-api.qa.example.test",
			Classification: "exact_hostname",
			Reason:         "url_hostname_reference",
		},
		{
			Value:          "search.fields.title.keyword",
			Classification: "rejected_field_path",
			Reason:         "dotted_field_path",
		},
		{
			Value:          "settings.retry.count",
			Classification: "rejected_config_key",
			Reason:         "dotted_config_key",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("HostnameCandidates() = %#v, want %#v", got, want)
	}
}
