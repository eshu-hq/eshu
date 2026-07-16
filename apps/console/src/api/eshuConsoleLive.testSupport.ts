import type { EshuApiClient } from "./client";

export function fakeClient(): EshuApiClient {
  return {
    get: async (path: string) => {
      if (path.includes("/ecosystem/overview")) {
        return {
          data: { repo_count: 33, workload_count: 21, platform_count: 7, instance_count: 92 },
          error: null,
          truth: {
            profile: "production",
            level: "exact",
            capability: "x",
            freshness: { state: "fresh" },
          },
        };
      }
      if (path.includes("/repositories/language-inventory")) {
        return {
          data: {
            languages: [
              { language: "yaml", repository_count: 32 },
              { language: "go", repository_count: 5 },
            ],
          },
          error: null,
          truth: null,
        };
      }
      if (path.includes("/repositories/by-language")) {
        throw new Error("by-language requires ?language= and must not be used for the overview");
      }
      if (path.includes("/catalog")) {
        // The same workload appears as a service and a workload (and twice
        // across environments) — the adapter must dedup by id.
        return {
          data: {
            services: [
              {
                id: "workload:api",
                name: "api",
                kind: "deployment",
                repo_name: "api",
                environments: ["qa", "prod"],
              },
            ],
            workloads: [
              {
                id: "workload:api",
                name: "api",
                kind: "deployment",
                repo_name: "api",
                environments: ["qa", "prod"],
              },
              {
                id: "workload:lib-config",
                name: "lib-config",
                kind: "library",
                repo_name: "lib-config",
              },
              {
                id: "workload:lib-config",
                name: "lib-config",
                kind: "library",
                repo_name: "lib-config",
              },
            ],
          },
          error: null,
          truth: {
            profile: "production",
            level: "exact",
            capability: "x",
            freshness: { state: "fresh" },
          },
        };
      }
      if (path.includes("/api/v0/images")) {
        return {
          data: {
            images: [
              {
                id: "oci-image://reg/team/api@sha256:aaa",
                digest: "sha256:aaa",
                repository_id: "oci-registry://reg/team/api",
                registry: "reg",
                repository: "team/api",
                name: "api",
                tag: "1.2.3",
                media_type: "application/vnd.oci.image.manifest.v1+json",
                size_bytes: 1234567,
                source_system: "oci_registry",
              },
            ],
            count: 1,
            limit: 50,
            offset: 0,
            truncated: false,
          },
          error: null,
          truth: {
            profile: "production",
            level: "exact",
            capability: "platform_impact.container_image_list",
            freshness: { state: "fresh" },
          },
        };
      }
      if (path.includes("/iac/resources")) {
        return {
          data: {
            resources: [
              {
                id: "tf1",
                kind: "resource",
                name: 'module."api".aws_iam_role.this',
                type: "aws_iam_role",
                provider: "aws",
                resource_service: "aws.iam",
                module: "api",
                repo_id: "r_1",
                relative_path: "main.tf",
              },
              { id: "tf2", kind: "resource", name: "aws_s3_bucket.logs", type: "aws_s3_bucket" },
            ],
          },
          error: null,
          truth: {
            profile: "production",
            level: "exact",
            capability: "iac_inventory.resources.list",
            freshness: { state: "fresh" },
          },
        };
      }
      if (path.includes("/sbom-attestations/attachments/count")) {
        // The cheap count rollup requires no scope; the snapshot derives the
        // verified count from attached_verified and the per-kind splits.
        return {
          data: {
            total_attachments: 148,
            by_attachment_status: { attached_verified: 100, attached_parse_only: 48 },
            by_artifact_kind: { sbom: 120, attestation: 28 },
          },
          error: null,
          truth: {
            profile: "production",
            level: "exact",
            capability: "supply_chain.sbom_attestation_attachments.aggregate",
            freshness: { state: "fresh" },
          },
        };
      }
      if (path.includes("/api/v0/dependencies")) {
        return {
          data: {
            dependencies: [
              {
                direction: "forward",
                anchor_package: "@eshu/core",
                anchor_package_id: "npm://r/@eshu/core",
                declaring_version: "1.0.0",
                related_package: "left-pad",
                related_package_id: "npm://r/left-pad",
                related_ecosystem: "npm",
                dependency_range: "^1.3.0",
                dependency_type: "runtime",
                optional: false,
                edge_id: "edge-1",
              },
            ],
            direction: "forward",
            truncated: false,
          },
          error: null,
          truth: {
            profile: "production",
            level: "exact",
            capability: "dependencies.list",
            basis: "authoritative_graph",
            freshness: { state: "fresh" },
          },
        };
      }
      return { data: {}, error: null, truth: null };
    },
    getJson: async (path: string) => {
      if (path.includes("/index-status")) {
        return {
          status: "healthy",
          repository_count: 33,
          queue: { outstanding: 2, in_flight: 1, dead_letter: 0, succeeded: 333 },
          coordinator: {
            collector_instances: [
              {
                collector_kind: "grafana",
                instance_id: "remote-e2e-grafana",
                enabled: true,
                last_observed_at: "2026-06-07T05:00:00Z",
                deactivated_at: null,
              },
              {
                collector_kind: "aws",
                instance_id: "remote-e2e-aws",
                enabled: true,
                last_observed_at: "2026-06-07T05:00:00Z",
                deactivated_at: null,
              },
              {
                collector_kind: "loki",
                instance_id: "remote-e2e-loki",
                enabled: false,
                last_observed_at: null,
                deactivated_at: "2026-06-06T00:00:00Z",
              },
            ],
          },
        };
      }
      if (path.includes("/status/ingesters")) {
        return {
          ingesters: [{ name: "repository", health: "healthy", runtime_family: "ingester" }],
        };
      }
      return {};
    },
    post: async () => ({ data: {}, error: null, truth: null }),
  } as unknown as EshuApiClient;
}
