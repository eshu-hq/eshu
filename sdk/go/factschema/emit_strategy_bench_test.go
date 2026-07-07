// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	"fmt"
	"strings"
	"testing"

	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
	azurev1 "github.com/eshu-hq/eshu/sdk/go/factschema/azure/v1"
	codegraphv1 "github.com/eshu-hq/eshu/sdk/go/factschema/codegraph/v1"
	gcpv1 "github.com/eshu-hq/eshu/sdk/go/factschema/gcp/v1"
	kuberneteslivev1 "github.com/eshu-hq/eshu/sdk/go/factschema/kuberneteslive/v1"
)

var benchmarkPayloadSink map[string]any

const benchmarkFactKindAWSDNSRecord = "aws_dns_record"

// Benchmark Evidence: issue #4785 measured emit strategy cost with
// `go test -run '^$' -bench 'BenchmarkEmitStrategy|BenchmarkEmitRuntimeDecodeValidation' -benchmem -count=5`
// on Apple M5 Max. Median results showed existing encodeToPayload materially
// slower than inline maps and direct-map prototypes: ~4.7 us/op small DNS,
// ~13.0 us/op AWS resource, and ~554 us/op large parsed_file_data. Direct-map
// prototypes stayed within noise of inline maps. No-Observability-Change:
// benchmark-only proof; no production runtime behavior or telemetry changed.

func BenchmarkEmitStrategy(b *testing.B) {
	b.Run("small_dns_record", func(b *testing.B) {
		record := benchmarkDNSRecord()
		b.Run("inline_map", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				benchmarkPayloadSink = benchmarkDNSRecordMap(record)
			}
		})
		b.Run("encode_existing", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				payload, err := EncodeAWSDNSRecord(record)
				if err != nil {
					b.Fatalf("EncodeAWSDNSRecord() error = %v", err)
				}
				benchmarkPayloadSink = payload
			}
		})
		b.Run("direct_map_prototype", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				benchmarkPayloadSink = encodeBenchmarkDNSRecordDirect(record)
			}
		})
	})

	b.Run("medium_aws_resource", func(b *testing.B) {
		resource := benchmarkAWSResource()
		b.Run("inline_map", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				benchmarkPayloadSink = benchmarkAWSResourceMap(resource)
			}
		})
		b.Run("encode_existing", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				payload, err := EncodeAWSResource(resource)
				if err != nil {
					b.Fatalf("EncodeAWSResource() error = %v", err)
				}
				benchmarkPayloadSink = payload
			}
		})
		b.Run("direct_map_prototype", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				benchmarkPayloadSink = encodeAWSResourceDirect(resource)
			}
		})
	})

	b.Run("large_parsed_file_data", func(b *testing.B) {
		file := benchmarkCodegraphFile()
		b.Run("inline_map", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				benchmarkPayloadSink = benchmarkCodegraphFileMap(file)
			}
		})
		b.Run("encode_existing", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				payload, err := EncodeCodegraphFile(file)
				if err != nil {
					b.Fatalf("EncodeCodegraphFile() error = %v", err)
				}
				benchmarkPayloadSink = payload
			}
		})
		b.Run("direct_map_prototype", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				benchmarkPayloadSink = encodeCodegraphFileDirect(file)
			}
		})
	})
}

func BenchmarkEmitRuntimeDecodeValidation(b *testing.B) {
	dnsPayload := benchmarkDNSRecordMap(benchmarkDNSRecord())
	b.Run("small_dns_record", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := decodeAndValidate[awsv1.DNSRecord](benchmarkFactKindAWSDNSRecord, dnsPayload); err != nil {
				b.Fatalf("decodeAndValidate() error = %v", err)
			}
		}
	})

	awsEnv := Envelope{FactKind: FactKindAWSResource, SchemaVersion: "1.0.0", Payload: benchmarkAWSResourceMap(benchmarkAWSResource())}
	b.Run("medium_aws_resource", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := DecodeAWSResource(awsEnv); err != nil {
				b.Fatalf("DecodeAWSResource() error = %v", err)
			}
		}
	})

	fileEnv := Envelope{FactKind: FactKindCodegraphFile, SchemaVersion: "1.0.0", Payload: benchmarkCodegraphFileMap(benchmarkCodegraphFile())}
	b.Run("large_parsed_file_data", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := DecodeCodegraphFile(fileEnv); err != nil {
				b.Fatalf("DecodeCodegraphFile() error = %v", err)
			}
		}
	})
}

func benchmarkDNSRecord() awsv1.DNSRecord {
	ttl := int64(300)
	return awsv1.DNSRecord{
		AccountID:            "123456789012",
		Region:               "us-east-1",
		ServiceKind:          stringPtr("route53"),
		CollectorInstanceID:  stringPtr("collector-1"),
		HostedZoneID:         "Z0123456789",
		HostedZoneName:       stringPtr("example.com."),
		HostedZonePrivate:    boolPtr(false),
		RecordName:           "api.example.com.",
		NormalizedRecordName: "api.example.com",
		RecordType:           "A",
		TTL:                  &ttl,
		Values:               []string{"203.0.113.10"},
		CorrelationAnchors:   []string{"api.example.com.", "api.example.com", "203.0.113.10"},
		HasAliasTarget:       boolPtr(false),
		SourceHostedZoneName: stringPtr("example.com."),
	}
}

func BenchmarkW1bEncodeNoRegression(b *testing.B) {
	b.Run("azure_cloud_resource", func(b *testing.B) {
		resource := benchmarkAzureCloudResource()
		b.ReportAllocs()
		for b.Loop() {
			payload, err := EncodeAzureCloudResource(resource)
			if err != nil {
				b.Fatalf("EncodeAzureCloudResource() error = %v", err)
			}
			benchmarkPayloadSink = payload
		}
	})

	b.Run("gcp_cloud_resource", func(b *testing.B) {
		resource := benchmarkGCPCloudResource()
		b.ReportAllocs()
		for b.Loop() {
			payload, err := EncodeGCPCloudResource(resource)
			if err != nil {
				b.Fatalf("EncodeGCPCloudResource() error = %v", err)
			}
			benchmarkPayloadSink = payload
		}
	})

	b.Run("kubernetes_live_pod_template", func(b *testing.B) {
		template := benchmarkKubernetesLivePodTemplate()
		b.ReportAllocs()
		for b.Loop() {
			payload, err := EncodeKubernetesLivePodTemplate(template)
			if err != nil {
				b.Fatalf("EncodeKubernetesLivePodTemplate() error = %v", err)
			}
			benchmarkPayloadSink = payload
		}
	})
}

func benchmarkDNSRecordMap(record awsv1.DNSRecord) map[string]any {
	payload := map[string]any{
		"account_id":             record.AccountID,
		"region":                 record.Region,
		"hosted_zone_id":         record.HostedZoneID,
		"record_name":            record.RecordName,
		"normalized_record_name": record.NormalizedRecordName,
		"record_type":            record.RecordType,
	}
	addBenchmarkStringPtr(payload, "service_kind", record.ServiceKind)
	addBenchmarkStringPtr(payload, "collector_instance_id", record.CollectorInstanceID)
	addBenchmarkStringPtr(payload, "hosted_zone_name", record.HostedZoneName)
	addBenchmarkBoolPtr(payload, "hosted_zone_private", record.HostedZonePrivate)
	addBenchmarkStringPtr(payload, "set_identifier", record.SetIdentifier)
	addBenchmarkInt64Ptr(payload, "ttl", record.TTL)
	addBenchmarkStringSlice(payload, "values", record.Values)
	if record.AliasTarget != nil {
		payload["alias_target"] = encodeDNSAliasTarget(*record.AliasTarget)
	}
	if record.RoutingPolicy != nil {
		payload["routing_policy"] = encodeDNSRoutingPolicy(*record.RoutingPolicy)
	}
	addBenchmarkStringSlice(payload, "correlation_anchors", record.CorrelationAnchors)
	addBenchmarkBoolPtr(payload, "has_alias_target", record.HasAliasTarget)
	addBenchmarkStringPtr(payload, "source_hosted_zone_name", record.SourceHostedZoneName)
	return payload
}

func encodeBenchmarkDNSRecordDirect(record awsv1.DNSRecord) map[string]any {
	payload := map[string]any{
		"account_id":             record.AccountID,
		"region":                 record.Region,
		"hosted_zone_id":         record.HostedZoneID,
		"record_name":            record.RecordName,
		"normalized_record_name": record.NormalizedRecordName,
		"record_type":            record.RecordType,
	}
	addBenchmarkStringPtr(payload, "service_kind", record.ServiceKind)
	addBenchmarkStringPtr(payload, "collector_instance_id", record.CollectorInstanceID)
	addBenchmarkStringPtr(payload, "hosted_zone_name", record.HostedZoneName)
	addBenchmarkBoolPtr(payload, "hosted_zone_private", record.HostedZonePrivate)
	addBenchmarkStringPtr(payload, "set_identifier", record.SetIdentifier)
	addBenchmarkInt64Ptr(payload, "ttl", record.TTL)
	addBenchmarkStringSlice(payload, "values", record.Values)
	if record.AliasTarget != nil {
		payload["alias_target"] = encodeDNSAliasTarget(*record.AliasTarget)
	}
	if record.RoutingPolicy != nil {
		payload["routing_policy"] = encodeDNSRoutingPolicy(*record.RoutingPolicy)
	}
	addBenchmarkStringSlice(payload, "correlation_anchors", record.CorrelationAnchors)
	addBenchmarkBoolPtr(payload, "has_alias_target", record.HasAliasTarget)
	addBenchmarkStringPtr(payload, "source_hosted_zone_name", record.SourceHostedZoneName)
	return payload
}

func benchmarkAWSResource() awsv1.Resource {
	tags := map[string]string{"env": "prod", "service": "supply-chain-demo"}
	return awsv1.Resource{
		AccountID:          "123456789012",
		ResourceID:         "arn:aws:ecs:us-east-1:123456789012:service/supply-chain-demo/supply-chain-demo",
		Region:             "us-east-1",
		ResourceType:       "ecs.service",
		ARN:                stringPtr("arn:aws:ecs:us-east-1:123456789012:service/supply-chain-demo/supply-chain-demo"),
		Name:               stringPtr("supply-chain-demo"),
		State:              stringPtr("active"),
		ServiceKind:        stringPtr("ecs"),
		CorrelationAnchors: []string{"arn:aws:ecs:us-east-1:123456789012:service/supply-chain-demo/supply-chain-demo"},
		Tags:               &tags,
		Attributes: map[string]any{
			"attributes": map[string]any{
				"cluster_arn":   "arn:aws:ecs:us-east-1:123456789012:cluster/supply-chain-demo",
				"desired_count": 2,
				"running_count": 2,
			},
		},
	}
}

func benchmarkAzureCloudResource() azurev1.CloudResource {
	normalized := "/subscriptions/sub-1/resourcegroups/rg/providers/microsoft.compute/virtualmachines/vm-1"
	name := "vm-1"
	provider := "microsoft.compute"
	return azurev1.CloudResource{
		ARMResourceID:        "/subscriptions/sub-1/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm-1",
		ResourceType:         "microsoft.compute/virtualmachines",
		SubscriptionID:       "sub-1",
		Location:             "eastus",
		NormalizedResourceID: &normalized,
		ResourceName:         &name,
		ProviderNamespace:    &provider,
		Attributes:           benchmarkCloudAttributes(),
	}
}

func benchmarkGCPCloudResource() gcpv1.Resource {
	projectID := "demo-project"
	location := "us-central1"
	displayName := "instance-1"
	state := "RUNNING"
	assetTypeFamily := "compute"
	return gcpv1.Resource{
		FullResourceName:   "//compute.googleapis.com/projects/demo-project/zones/us-central1-a/instances/instance-1",
		AssetType:          "compute.googleapis.com/Instance",
		ProjectID:          &projectID,
		Location:           &location,
		DisplayName:        &displayName,
		State:              &state,
		AssetTypeFamily:    &assetTypeFamily,
		CorrelationAnchors: []string{"instance-1", "demo-project"},
		Attributes:         benchmarkCloudAttributes(),
	}
}

func benchmarkKubernetesLivePodTemplate() kuberneteslivev1.PodTemplate {
	clusterID := "cluster-1"
	namespace := "default"
	name := "api"
	uid := "uid-1"
	gvr := "apps/v1/deployments"
	serviceAccount := "api"
	image := "registry.example.com/api@sha256:111"
	init := false
	envFromSecret := true
	containerName := "api"
	return kuberneteslivev1.PodTemplate{
		ObjectID:             "cluster-1/default/apps/v1/deployments/api/uid-1",
		ClusterID:            &clusterID,
		Namespace:            &namespace,
		Name:                 &name,
		WorkloadUID:          &uid,
		GroupVersionResource: &gvr,
		ServiceAccount:       &serviceAccount,
		Containers: []kuberneteslivev1.PodTemplateContainer{
			{
				Name:          &containerName,
				Image:         &image,
				Init:          &init,
				Ports:         []int32{8080},
				EnvKeys:       []string{"DATABASE_URL", "LOG_LEVEL"},
				EnvFromSecret: &envFromSecret,
			},
		},
		ImageRefs:          []string{image},
		Selector:           map[string]string{"app": "api"},
		Labels:             map[string]string{"app": "api", "tier": "backend"},
		CorrelationAnchors: []string{"cluster-1/default/apps/v1/deployments/api/uid-1", image},
	}
}

func benchmarkCloudAttributes() map[string]any {
	return map[string]any{
		"collector_instance_id": "collector-1",
		"source_lane":           "resource_graph",
		"labels":                map[string]any{"env": "prod", "service": "api"},
		"extension":             map[string]any{"schema_version": "1.0.0", "has_identity": true},
	}
}

func benchmarkAWSResourceMap(resource awsv1.Resource) map[string]any {
	payload := encodeAWSResourceDirect(resource)
	payload["collector_instance_id"] = "collector-1"
	return payload
}

func encodeAWSResourceDirect(resource awsv1.Resource) map[string]any {
	payload := map[string]any{
		"account_id":    resource.AccountID,
		"resource_id":   resource.ResourceID,
		"region":        resource.Region,
		"resource_type": resource.ResourceType,
	}
	addBenchmarkStringPtr(payload, "arn", resource.ARN)
	addBenchmarkStringPtr(payload, "name", resource.Name)
	addBenchmarkStringPtr(payload, "state", resource.State)
	addBenchmarkStringPtr(payload, "service_kind", resource.ServiceKind)
	addBenchmarkStringSlice(payload, "correlation_anchors", resource.CorrelationAnchors)
	if resource.Tags != nil {
		payload["tags"] = *resource.Tags
	}
	for key, value := range resource.Attributes {
		if _, known := resourceKnownPayloadKeys[key]; known {
			continue
		}
		if _, exists := payload[key]; !exists {
			payload[key] = value
		}
	}
	return payload
}

func benchmarkCodegraphFile() codegraphv1.File {
	return codegraphv1.File{
		RepoID:         "repo-example",
		RelativePath:   "src/service.go",
		ParsedFileData: largeParsedFileData(),
		GraphID:        stringPtr("repo-example:src/service.go"),
		GraphKind:      stringPtr("file"),
		IsDependency:   boolPtr(false),
		Language:       stringPtr("go"),
	}
}

func benchmarkCodegraphFileMap(file codegraphv1.File) map[string]any {
	return map[string]any{
		"repo_id":          file.RepoID,
		"relative_path":    file.RelativePath,
		"parsed_file_data": file.ParsedFileData,
		"graph_id":         *file.GraphID,
		"graph_kind":       *file.GraphKind,
		"is_dependency":    *file.IsDependency,
		"language":         *file.Language,
	}
}

func encodeCodegraphFileDirect(file codegraphv1.File) map[string]any {
	payload := map[string]any{
		"repo_id":          file.RepoID,
		"relative_path":    file.RelativePath,
		"parsed_file_data": file.ParsedFileData,
	}
	addBenchmarkStringPtr(payload, "graph_id", file.GraphID)
	addBenchmarkStringPtr(payload, "graph_kind", file.GraphKind)
	if file.IsDependency != nil {
		payload["is_dependency"] = *file.IsDependency
	}
	addBenchmarkStringPtr(payload, "language", file.Language)
	return payload
}

func largeParsedFileData() map[string]any {
	imports := make([]any, 0, 80)
	functions := make([]any, 0, 120)
	calls := make([]any, 0, 240)
	for i := 0; i < 80; i++ {
		imports = append(imports, map[string]any{"source": fmt.Sprintf("pkg/%03d", i), "alias": fmt.Sprintf("p%d", i)})
	}
	for i := 0; i < 120; i++ {
		name := fmt.Sprintf("Function%03d", i)
		functions = append(functions, map[string]any{
			"name": name, "line": i + 10, "signature": "func " + name + "(context.Context) error",
			"annotations": []any{"handler", "reducer", "materializer"},
		})
	}
	for i := 0; i < 240; i++ {
		calls = append(calls, map[string]any{
			"caller": fmt.Sprintf("Function%03d", i%120),
			"callee": fmt.Sprintf("Dependency%03d", i),
			"line":   i + 20,
			"args":   []any{"ctx", "row", strings.Repeat("x", 24)},
		})
	}
	return map[string]any{
		"path":           "src/service.go",
		"language":       "go",
		"imports":        imports,
		"functions":      functions,
		"function_calls": calls,
		"classes":        []any{},
		"variables":      []any{},
	}
}

var resourceKnownPayloadKeys = map[string]struct{}{
	"account_id": {}, "resource_id": {}, "region": {}, "resource_type": {},
	"arn": {}, "name": {}, "state": {}, "service_kind": {},
	"correlation_anchors": {}, "tags": {},
}

func addBenchmarkBoolPtr(payload map[string]any, key string, value *bool) {
	if value != nil {
		payload[key] = *value
	}
}

func addBenchmarkStringPtr(payload map[string]any, key string, value *string) {
	if value != nil {
		payload[key] = *value
	}
}

func addBenchmarkInt64Ptr(payload map[string]any, key string, value *int64) {
	if value != nil {
		payload[key] = *value
	}
}

func addBenchmarkStringSlice(payload map[string]any, key string, value []string) {
	if len(value) > 0 {
		payload[key] = value
	}
}

func stringPtr(value string) *string { return &value }

func boolPtr(value bool) *bool { return &value }
