// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"strings"
	"testing"
)

// TestOpenSearchAPIClientExcludesMutationAndIndexAPIs is the load-bearing proof
// for issue #749: the SDK adapter reaches OpenSearch and OpenSearch Serverless
// only through the metadata-only reads listed below. The two AWS API interfaces
// (domainAPI and serverlessAPI) are the only ways the adapter reaches AWS, so
// asserting their method shape proves the forbidden APIs are unreachable from
// this code path.
//
// The OpenSearch HTTP API (_search, _index, _doc, _bulk, and similar) is not
// part of the AWS SDK client surface at all; it is only reachable over the
// domain HTTP endpoint, which this adapter never constructs or calls. Asserting
// the SDK interfaces carry no GetIndex method and no mutation verbs is the
// strongest in-tree proof that no index or search API is reachable.
func TestOpenSearchAPIClientExcludesMutationAndIndexAPIs(t *testing.T) {
	wantDomain := map[string]bool{
		"ListDomainNames":       true,
		"DescribeDomains":       true,
		"DescribePackages":      true,
		"ListDomainsForPackage": true,
		"ListTags":              true,
	}
	assertInterfaceMethods(t, reflect.TypeOf((*domainAPI)(nil)).Elem(), wantDomain)

	wantServerless := map[string]bool{
		"ListCollections":     true,
		"BatchGetCollection":  true,
		"ListSecurityConfigs": true,
		"ListVpcEndpoints":    true,
		"BatchGetVpcEndpoint": true,
	}
	assertInterfaceMethods(t, reflect.TypeOf((*serverlessAPI)(nil)).Elem(), wantServerless)

	// Defensive substring check across both interfaces. Any mutation verb,
	// inbound-connection acceptance, or index/data read is a contract violation
	// regardless of the want-lists above. "GetIndex" is the OpenSearch control
	// plane's index-reference read; the scanner does not need it and must never
	// reach it, so it is forbidden alongside the HTTP index/search verbs.
	forbidden := []string{
		"Create",
		"Update",
		"Delete",
		"Put",
		"Associate",
		"Dissociate",
		"Accept",
		"Reject",
		"Start",
		"Stop",
		"Cancel",
		"Upgrade",
		"Purchase",
		"Revoke",
		"Add",
		"Remove",
		"GetIndex",
		"Index",
		"Search",
		"Doc",
		"Bulk",
		"Query",
		"Document",
	}
	for _, iface := range []reflect.Type{
		reflect.TypeOf((*domainAPI)(nil)).Elem(),
		reflect.TypeOf((*serverlessAPI)(nil)).Elem(),
	} {
		for i := 0; i < iface.NumMethod(); i++ {
			name := iface.Method(i).Name
			for _, bad := range forbidden {
				if strings.Contains(name, bad) {
					t.Errorf("%s method %q contains forbidden substring %q; metadata-only contract violated", iface.Name(), name, bad)
				}
			}
		}
	}
}

func assertInterfaceMethods(t *testing.T, iface reflect.Type, want map[string]bool) {
	t.Helper()
	have := map[string]bool{}
	for i := 0; i < iface.NumMethod(); i++ {
		have[iface.Method(i).Name] = true
	}
	for name := range want {
		if !have[name] {
			t.Errorf("%s missing required method %q", iface.Name(), name)
		}
	}
	for name := range have {
		if !want[name] {
			t.Errorf("%s exposes unexpected method %q; metadata-only contract violated", iface.Name(), name)
		}
	}
}

// TestDomainModelHasNoMasterUserPasswordField proves the scanner-owned Domain
// type cannot carry a master user password. DescribeDomains never returns the
// password, but this struct-shape assertion makes the absence enforced by the
// type system: a future edit that adds a password-shaped field fails this test.
func TestDomainModelHasNoMasterUserPasswordField(t *testing.T) {
	// Resolve the scanner Domain type through a mapped value so the test stays
	// coupled to the real type the adapter produces.
	domainType := reflect.TypeOf(mapDomain(emptyDomainStatus(), nil))
	for i := 0; i < domainType.NumField(); i++ {
		name := strings.ToLower(domainType.Field(i).Name)
		for _, bad := range []string{"password", "secret", "token", "credential"} {
			if strings.Contains(name, bad) {
				t.Errorf("Domain field %q contains forbidden substring %q; master password material must never be modeled", domainType.Field(i).Name, bad)
			}
		}
	}
}
