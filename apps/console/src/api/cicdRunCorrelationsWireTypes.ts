export interface CICDCountWire {
  readonly by_environment?: Record<string, number>;
  readonly by_outcome?: Record<string, number>;
  readonly by_provider?: Record<string, number>;
  readonly scope?: Record<string, string>;
  readonly total_correlations?: number;
}

export interface CICDInventoryWire {
  readonly buckets?: readonly CICDBucketWire[];
  readonly count?: number;
  readonly group_by?: "environment" | "outcome" | "provider" | "repository_id";
  readonly limit?: number;
  readonly next_offset?: number | null;
  readonly offset?: number;
  readonly scope?: Record<string, string>;
  readonly truncated?: boolean;
}

export interface CICDBucketWire {
  readonly count?: number;
  readonly dimension?: string;
  readonly value?: string;
}

export interface CICDListWire {
  readonly correlations?: readonly CICDRunCorrelationWire[];
  readonly count?: number;
  readonly evidence_summary?: CICDEvidenceSummaryWire;
  readonly limit?: number;
  readonly next_cursor?: {
    readonly after_correlation_id?: string;
  };
  readonly truncated?: boolean;
}

export interface CICDRunCorrelationWire {
  readonly artifact_digest?: string;
  readonly canonical_target?: string;
  readonly canonical_writes?: number;
  readonly commit_sha?: string;
  readonly correlation_id?: string;
  readonly correlation_kind?: string;
  readonly environment?: string;
  readonly evidence_fact_ids?: readonly string[];
  readonly image_ref?: string;
  readonly outcome?: string;
  readonly provider?: string;
  readonly provenance_only?: boolean;
  readonly reason?: string;
  readonly repository_id?: string;
  readonly run_attempt?: string;
  readonly run_id?: string;
}

export interface CICDEvidenceSummaryWire {
  readonly live_run_correlations?: CICDEvidenceBlockWire;
  readonly missing_evidence?: readonly string[];
  readonly reason?: string;
  readonly run_artifact_evidence?: CICDRunArtifactEvidenceWire;
  readonly static_workflow_artifacts?: CICDStaticWorkflowArtifactsWire;
}

export interface CICDEvidenceBlockWire {
  readonly count?: number;
  readonly reason?: string;
  readonly state?: string;
  readonly truncated?: boolean;
}

export interface CICDRunArtifactEvidenceWire extends CICDEvidenceBlockWire {
  readonly ambiguous_count?: number;
  readonly artifact_digest_count?: number;
  readonly image_ref_count?: number;
}

export interface CICDStaticWorkflowArtifactsWire extends CICDEvidenceBlockWire {
  readonly ambiguous_count?: number;
  readonly evidence_class?: string;
  readonly image_ref_count?: number;
  readonly paths?: readonly string[];
  readonly unresolved_count?: number;
}
