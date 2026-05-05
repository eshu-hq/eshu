package query

import (
	"reflect"
	"testing"
)

func TestBuildServiceAPISurfaceIncludesEndpointDetails(t *testing.T) {
	t.Parallel()

	surface := buildServiceAPISurface(ServiceQueryEvidence{
		DocsRoutes: []ServiceDocsRouteEvidence{
			{Route: "/_specs", RelativePath: "server/spec.js", Reason: "docs_route_reference"},
		},
		APISpecs: []ServiceAPISpecEvidence{
			{
				RelativePath:     "specs/index.yaml",
				Format:           "yaml",
				Parsed:           true,
				SpecVersion:      "3.0.3",
				APIVersion:       "v3",
				EndpointCount:    2,
				MethodCount:      3,
				OperationIDCount: 3,
				Hostnames:        []string{"sample-service-api.qa.example.test"},
				Endpoints: []ServiceAPIEndpointEvidence{
					{Path: "/v3/search", Methods: []string{"get", "post"}, OperationIDs: []string{"search", "postSearch"}},
					{Path: "/v3/listing/{id}", Methods: []string{"get"}, OperationIDs: []string{"getListing"}},
				},
			},
		},
	})

	endpoints := mapSliceValue(surface, "endpoints")
	if len(endpoints) != 2 {
		t.Fatalf("len(endpoints) = %d, want 2", len(endpoints))
	}
	if got, want := StringVal(endpoints[0], "path"), "/v3/listing/{id}"; got != want {
		t.Fatalf("endpoints[0].path = %q, want %q", got, want)
	}
	if got, want := StringSliceVal(endpoints[1], "methods"), []string{"get", "post"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("endpoints[1].methods = %#v, want %#v", got, want)
	}
}

func TestBuildServiceAPISurfaceMergesFrameworkRoutes(t *testing.T) {
	t.Parallel()

	surface := buildServiceAPISurface(ServiceQueryEvidence{
		FrameworkRoutes: []FrameworkRouteEvidence{
			{
				Framework:    "hapi",
				RelativePath: "src/routes/catalog.js",
				RoutePaths:   []string{"/elastic", "/alias/{index}/create", "/schema/{index}"},
				RouteMethods: []string{"GET", "POST", "PUT"},
				RouteEntries: []FrameworkRouteEntryEvidence{
					{Method: "GET", Path: "/elastic"},
					{Method: "POST", Path: "/alias/{index}/create"},
					{Method: "PUT", Path: "/schema/{index}"},
				},
			},
			{
				Framework:    "fastapi",
				RelativePath: "api/main.py",
				RoutePaths:   []string{"/catalog", "/catalog/{id}"},
				RouteMethods: []string{"GET", "POST"},
			},
		},
	})

	if surface == nil {
		t.Fatal("surface = nil, want non-nil when framework routes present")
	}
	endpoints := mapSliceValue(surface, "endpoints")
	if len(endpoints) != 5 {
		t.Fatalf("len(endpoints) = %d, want 5", len(endpoints))
	}
	// Verify framework_route_count is tracked
	if got, want := IntVal(surface, "framework_route_count"), 5; got != want {
		t.Fatalf("framework_route_count = %d, want %d", got, want)
	}
	// Verify frameworks list
	frameworks := StringSliceVal(surface, "frameworks")
	if len(frameworks) != 2 {
		t.Fatalf("frameworks = %v, want [fastapi hapi]", frameworks)
	}
	// Verify endpoints have source = "framework"
	for _, ep := range endpoints {
		if StringVal(ep, "source") != "framework" {
			t.Fatalf("endpoint source = %q, want \"framework\"", StringVal(ep, "source"))
		}
	}

	endpointsByPath := map[string]map[string]any{}
	for _, ep := range endpoints {
		endpointsByPath[StringVal(ep, "path")] = ep
	}
	if got, want := StringSliceVal(endpointsByPath["/elastic"], "methods"), []string{"get"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("/elastic methods = %#v, want %#v", got, want)
	}
	if got, want := StringSliceVal(endpointsByPath["/alias/{index}/create"], "methods"), []string{"post"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("/alias/{{index}}/create methods = %#v, want %#v", got, want)
	}
	if got, want := StringSliceVal(endpointsByPath["/schema/{index}"], "methods"), []string{"put"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("/schema/{{index}} methods = %#v, want %#v", got, want)
	}
}

func TestBuildServiceAPISurfaceCombinesSpecAndFrameworkRoutes(t *testing.T) {
	t.Parallel()

	surface := buildServiceAPISurface(ServiceQueryEvidence{
		APISpecs: []ServiceAPISpecEvidence{
			{
				RelativePath:  "specs/index.yaml",
				Format:        "yaml",
				Parsed:        true,
				SpecVersion:   "3.0.3",
				EndpointCount: 1,
				MethodCount:   1,
				Endpoints: []ServiceAPIEndpointEvidence{
					{Path: "/health", Methods: []string{"get"}},
				},
			},
		},
		FrameworkRoutes: []FrameworkRouteEvidence{
			{
				Framework:    "express",
				RelativePath: "src/app.js",
				RoutePaths:   []string{"/catalog", "/catalog/:id"},
				RouteMethods: []string{"GET", "POST"},
			},
		},
	})

	if surface == nil {
		t.Fatal("surface = nil, want non-nil")
	}
	endpoints := mapSliceValue(surface, "endpoints")
	if len(endpoints) != 3 {
		t.Fatalf("len(endpoints) = %d, want 3 (1 spec + 2 framework)", len(endpoints))
	}
	if got, want := IntVal(surface, "endpoint_count"), 1; got != want {
		t.Fatalf("endpoint_count (spec) = %d, want %d", got, want)
	}
	if got, want := IntVal(surface, "framework_route_count"), 2; got != want {
		t.Fatalf("framework_route_count = %d, want %d", got, want)
	}
}

func TestBuildServiceEntrypointsSeparatesPublicAndInternalSignals(t *testing.T) {
	t.Parallel()

	workloadContext := map[string]any{
		"instances": []map[string]any{
			{
				"instance_id":   "instance:sample-service-api:qa",
				"platform_name": "sample-eks",
				"platform_kind": "eks",
				"environment":   "qa",
			},
		},
	}
	entrypoints := buildServiceEntrypoints(
		workloadContext,
		ServiceQueryEvidence{
			Hostnames: []ServiceHostnameEvidence{
				{
					Hostname:     "sample-service-api.qa.example.test",
					Environment:  "qa",
					RelativePath: "config/qa.json",
					Reason:       "content_hostname_reference",
				},
			},
			DocsRoutes: []ServiceDocsRouteEvidence{
				{
					Route:        "/_specs",
					RelativePath: "server/spec.js",
					Reason:       "docs_route_reference",
				},
			},
		},
	)

	if len(entrypoints) != 2 {
		t.Fatalf("len(entrypoints) = %d, want 2", len(entrypoints))
	}
	if got, want := StringVal(entrypoints[0], "type"), "docs_route"; got != want {
		t.Fatalf("entrypoints[0].type = %q, want %q", got, want)
	}
	if got := StringVal(entrypoints[0], "environment"); got != "" {
		t.Fatalf("entrypoints[0].environment = %q, want empty without explicit evidence", got)
	}
	if got, want := StringVal(entrypoints[1], "visibility"), "public"; got != want {
		t.Fatalf("entrypoints[1].visibility = %q, want %q", got, want)
	}
}

func TestBuildServiceNetworkPathsConnectsEntrypointsToObservedRuntimeTargets(t *testing.T) {
	t.Parallel()

	workloadContext := map[string]any{
		"name": "sample-service-api",
		"instances": []map[string]any{
			{
				"instance_id":   "instance:sample-service-api:qa",
				"platform_name": "sample-eks",
				"platform_kind": "eks",
				"environment":   "qa",
			},
		},
	}
	entrypoints := []map[string]any{
		{
			"type":        "hostname",
			"target":      "sample-service-api.qa.example.test",
			"environment": "qa",
			"visibility":  "public",
			"reason":      "content_hostname_reference",
		},
		{
			"type":        "docs_route",
			"target":      "/_specs",
			"environment": "qa",
			"visibility":  "internal",
			"reason":      "docs_route_reference",
		},
	}

	paths := buildServiceNetworkPaths(workloadContext, entrypoints)
	if len(paths) != 2 {
		t.Fatalf("len(network_paths) = %d, want 2", len(paths))
	}
	if got, want := StringVal(paths[0], "path_type"), "docs_route_to_runtime"; got != want {
		t.Fatalf("paths[0].path_type = %q, want %q", got, want)
	}
	if got, want := StringVal(paths[1], "to"), "sample-eks"; got != want {
		t.Fatalf("paths[1].to = %q, want %q", got, want)
	}
}

func TestBuildServiceNetworkPathsDoesNotFallBackToFirstRuntimeWithoutEnvironmentMatch(t *testing.T) {
	t.Parallel()

	workloadContext := map[string]any{
		"name": "sample-service-api",
		"instances": []map[string]any{
			{
				"instance_id":   "instance:sample-service-api:qa",
				"platform_name": "sample-eks-qa",
				"platform_kind": "eks",
				"environment":   "qa",
			},
			{
				"instance_id":   "instance:sample-service-api:prod",
				"platform_name": "sample-eks-prod",
				"platform_kind": "eks",
				"environment":   "prod",
			},
		},
	}
	entrypoints := []map[string]any{
		{
			"type":        "docs_route",
			"target":      "/_specs",
			"environment": "",
			"visibility":  "internal",
			"reason":      "docs_route_reference",
		},
	}

	paths := buildServiceNetworkPaths(workloadContext, entrypoints)
	if len(paths) != 0 {
		t.Fatalf("len(network_paths) = %d, want 0 without an environment match", len(paths))
	}
}

func TestBuildGraphDependentsUsesRelationshipCandidates(t *testing.T) {
	t.Parallel()

	dependents := buildGraphDependents([]provisioningRepositoryCandidate{
		{
			RepoID:              "repo-consumer-a",
			RepoName:            "consumer-a",
			RelationshipTypes:   []string{"DEPLOYS_FROM", "PROVISIONS_DEPENDENCY_FOR"},
			RelationshipReasons: []string{"helm_values_reference"},
		},
	})

	if len(dependents) != 1 {
		t.Fatalf("len(dependents) = %d, want 1", len(dependents))
	}
	if got, want := StringVal(dependents[0], "repository"), "consumer-a"; got != want {
		t.Fatalf("dependents[0].repository = %q, want %q", got, want)
	}
	if got, want := StringSliceVal(dependents[0], "relationship_types"), []string{"DEPLOYS_FROM", "PROVISIONS_DEPENDENCY_FOR"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("dependents[0].relationship_types = %#v, want %#v", got, want)
	}
}
