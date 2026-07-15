import type { EshuApiClient } from "./client";
import type { EntityKind } from "./mockData";
import type { ServiceContextResponse } from "./serviceSpotlight";
import type { ConsoleMode } from "../config/environment";

export interface StoryResponse {
  readonly deployment_overview?: {
    readonly delivery_paths?: readonly DeliveryPath[];
    readonly direct_story?: readonly string[];
    readonly infrastructure_families?: readonly string[];
    readonly topology_story?: readonly string[];
    readonly workload_count?: number;
    readonly workloads?: readonly string[];
  };
  readonly drilldowns?: {
    readonly context_path?: string;
    readonly coverage_path?: string;
    readonly stats_path?: string;
  };
  readonly infrastructure_overview?: {
    readonly artifact_family_counts?: Record<string, number>;
    readonly entity_type_counts?: Record<string, number>;
    readonly families?: readonly string[];
  };
  readonly limitations?: readonly string[];
  readonly repository?: StoryRepository;
  readonly semantic_overview?: {
    readonly entity_count?: number;
    readonly entity_type_counts?: Record<string, number>;
    readonly language_counts?: Record<string, number>;
  };
  readonly story_sections?: readonly StorySection[];
  readonly story?: string;
  readonly service_identity?: { readonly repo_name?: string; readonly service_name?: string };
  readonly service_name?: string;
  readonly support_overview?: {
    readonly dependency_count?: number;
    readonly language_count?: number;
    readonly languages?: readonly string[];
    readonly topology_signal_count?: number;
  };
  readonly subject?: string | StorySubject;
}

export interface DeliveryPath {
  readonly artifact_family?: string;
  readonly artifact_type?: string;
  readonly delivery_command_families?: readonly string[];
  readonly environments?: readonly string[];
  readonly evidence_kind?: string;
  readonly kind?: string;
  readonly path?: string;
  readonly relative_path?: string;
  readonly signals?: readonly string[];
  readonly trigger_events?: readonly string[];
  readonly workflow_name?: string;
}

interface StoryRepository {
  readonly id?: string;
  readonly local_path?: string;
  readonly name?: string;
}

interface StorySubject {
  readonly id?: string;
  readonly name?: string;
  readonly type?: string;
}

export interface StorySection {
  readonly summary?: string;
  readonly title?: string;
}

export interface ContextResponse {
  readonly consumers?: readonly ContextConsumer[];
  readonly consumer_repositories?: readonly ContextConsumer[];
  readonly dependency_count?: number;
  readonly deployment_evidence?: {
    readonly artifact_count?: number;
    readonly artifact_families?: readonly string[];
    readonly artifacts?: readonly DeploymentEvidenceArtifact[];
    readonly relationship_types?: readonly string[];
  };
  readonly api_surface?: ServiceContextResponse["api_surface"];
  readonly deployment_lanes?: ServiceContextResponse["deployment_lanes"];
  readonly file_count?: number;
  readonly graph_dependents?: ServiceContextResponse["graph_dependents"];
  readonly infrastructure?: readonly InfrastructureItem[];
  readonly repository?: StoryRepository;
}

export interface ContextConsumer {
  readonly consumer_kinds?: readonly string[];
  readonly evidence_kinds?: readonly string[];
  readonly graph_relationship_types?: readonly string[];
  readonly id?: string;
  readonly matched_values?: readonly string[];
  readonly name?: string;
  readonly repo_name?: string;
  readonly repository?: string;
  readonly relationship_types?: readonly string[];
  readonly sample_paths?: readonly string[];
}

export interface DeploymentEvidenceArtifact {
  readonly artifact_family?: string;
  readonly confidence?: number;
  readonly direction?: string;
  readonly environment?: string;
  readonly evidence_kind?: string;
  readonly name?: string;
  readonly path?: string;
  readonly relationship_type?: string;
  readonly source_location?: {
    readonly path?: string;
    readonly repo_id?: string;
    readonly repo_name?: string;
  };
  readonly source_repo_name?: string;
  readonly target_repo_name?: string;
}

interface InfrastructureItem {
  readonly file_path?: string;
  readonly kind?: string;
  readonly name?: string;
  readonly type?: string;
}

export interface LoadWorkspaceStoryOptions {
  readonly client?: EshuApiClient;
  readonly entityId: string;
  readonly entityKind: EntityKind;
  readonly mode: ConsoleMode;
}
