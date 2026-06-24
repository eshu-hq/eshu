// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"strings"
	"testing"

	mwaaservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/mwaa"
)

// TestAdapterAPIClientForbidsMutationAndTokenMethods reflects over the apiClient
// interface and fails if it ever exposes a mutation, token-minting, REST-API,
// metric-publishing, or tagging method. The metadata-only MWAA adapter must
// never be able to create, update, or delete an environment, mint an Airflow
// CLI or web-login token, or invoke the Airflow REST API.
func TestAdapterAPIClientForbidsMutationAndTokenMethods(t *testing.T) {
	forbiddenExact := []string{
		"CreateCliToken",
		"CreateWebLoginToken",
		"InvokeRestApi",
		"PublishMetrics",
	}
	// Any method whose name begins with one of these verbs is a write,
	// lifecycle, or token operation and must not exist on the adapter surface.
	forbiddenPrefixes := []string{
		"Create", "Delete", "Update", "Put", "Set",
		"Tag", "Untag", "Publish", "Invoke",
		"Start", "Stop", "Enable", "Disable",
	}
	iface := reflect.TypeOf((*apiClient)(nil)).Elem()
	if iface.NumMethod() == 0 {
		t.Fatalf("apiClient interface has no methods; expected the MWAA read surface")
	}
	for i := 0; i < iface.NumMethod(); i++ {
		name := iface.Method(i).Name
		for _, banned := range forbiddenExact {
			if name == banned {
				t.Fatalf("apiClient exposes forbidden mutation/token method %q; the MWAA adapter is metadata-only", name)
			}
		}
		for _, prefix := range forbiddenPrefixes {
			if strings.HasPrefix(name, prefix) {
				t.Fatalf("apiClient exposes mutation/token method %q (prefix %q); the MWAA adapter is metadata-only", name, prefix)
			}
		}
	}
}

// TestAdapterMethodsAreListOrGetReads asserts every apiClient method is a List
// or Get read, so the read surface stays explicit and auditable: only
// ListEnvironments and GetEnvironment are reachable.
func TestAdapterMethodsAreListOrGetReads(t *testing.T) {
	iface := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < iface.NumMethod(); i++ {
		name := iface.Method(i).Name
		if !strings.HasPrefix(name, "List") && !strings.HasPrefix(name, "Get") {
			t.Fatalf("apiClient method %q is not a List/Get read", name)
		}
	}
}

// TestEnvironmentTypeHasNoAirflowConfigurationField is a structural guard: the
// scanner-owned Environment type must never declare a field that can carry
// Apache Airflow configuration option values, connection strings, executor
// queue ARNs, webserver URLs, or login tokens. A leak would fail to compile
// the mapper; this test makes the intent explicit and fails loudly if the type
// ever grows such a field.
func TestEnvironmentTypeHasNoAirflowConfigurationField(t *testing.T) {
	forbiddenFieldFragments := []string{
		"airflowconfiguration",
		"configurationoption",
		"connectionstring",
		"celeryexecutorqueue",
		"webserverurl",
		"clitoken",
		"weblogintoken",
		"token",
		"password",
		"secret",
	}
	typ := reflect.TypeOf(mwaaservice.Environment{})
	for i := 0; i < typ.NumField(); i++ {
		field := strings.ToLower(typ.Field(i).Name)
		for _, fragment := range forbiddenFieldFragments {
			if strings.Contains(field, fragment) {
				t.Fatalf("Environment grew field %q matching forbidden fragment %q; Airflow config values and secrets must never be persistable", typ.Field(i).Name, fragment)
			}
		}
	}
}
