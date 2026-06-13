export interface ReplatformingRollupsWire {
  readonly dimensions?: {
    readonly account?: readonly ReplatformingRollupBucketWire[];
    readonly environment?: readonly ReplatformingRollupBucketWire[];
    readonly service?: readonly ReplatformingRollupBucketWire[];
  };
  readonly limit?: number;
  readonly next_offset?: number | null;
  readonly offset?: number;
  readonly readiness_totals?: ReplatformingReadinessWire;
  readonly recommended_next_checks?: readonly string[];
  readonly rollup_findings_count?: number;
  readonly source_state_totals?: Record<string, number>;
  readonly story?: string;
  readonly total_findings_count?: number;
  readonly truncated?: boolean;
}

export interface ReplatformingRollupBucketWire {
  readonly key?: string;
  readonly readiness?: ReplatformingReadinessWire;
  readonly source_state_counts?: Record<string, number>;
  readonly total?: number;
}

export interface ReplatformingReadinessWire {
  readonly import_ready?: number;
  readonly needs_review?: number;
  readonly refused?: number;
}

export interface ReplatformingPlanWire {
  readonly blast_radius_summaries?: readonly ReplatformingBlastRadiusSummaryWire[];
  readonly items_count?: number;
  readonly limit?: number;
  readonly next_offset?: number | null;
  readonly offset?: number;
  readonly plan?: ReplatformingPlanBodyWire;
  readonly ready_import_count?: number;
  readonly refused_import_count?: number;
  readonly story?: string;
  readonly total_findings_count?: number;
  readonly truncated?: boolean;
  readonly wave_summaries?: readonly ReplatformingWaveSummaryWire[];
}

export interface ReplatformingPlanBodyWire {
  readonly blast_radius_groups?: readonly ReplatformingBlastRadiusGroupWire[];
  readonly contract_version?: string;
  readonly items?: readonly ReplatformingPlanItemWire[];
  readonly limitations?: readonly string[];
  readonly non_goals?: readonly string[];
  readonly scope?: Record<string, string>;
  readonly waves?: readonly ReplatformingWaveWire[];
}

export interface ReplatformingPlanItemWire {
  readonly blast_radius_group?: string;
  readonly confidence?: string;
  readonly finding_kind?: string;
  readonly import_candidate?: ReplatformingImportCandidateWire;
  readonly item_id?: string;
  readonly management_status?: string;
  readonly owner_candidates?: readonly ReplatformingOwnerCandidateWire[];
  readonly provider?: string;
  readonly resource_type?: string;
  readonly safety_gate?: ReplatformingSafetyGateWire;
  readonly source_state?: string;
  readonly stable_id?: string;
  readonly wave_id?: string;
}

export interface ReplatformingImportCandidateWire {
  readonly import_block?: string;
  readonly refusal_reasons?: readonly string[];
  readonly resource_type?: string;
  readonly status?: string;
}

export interface ReplatformingOwnerCandidateWire {
  readonly ambiguity_reasons?: readonly string[];
  readonly confidence?: string;
  readonly kind?: string;
  readonly value?: string;
}

export interface ReplatformingSafetyGateWire {
  readonly outcome?: string;
  readonly refused_actions?: readonly string[];
  readonly review_required?: boolean;
}

export interface ReplatformingWaveWire {
  readonly id?: string;
  readonly item_ids?: readonly string[];
  readonly order?: number;
  readonly rationale?: string;
}

export interface ReplatformingWaveSummaryWire {
  readonly item_count?: number;
  readonly order?: number;
  readonly wave_id?: string;
}

export interface ReplatformingBlastRadiusGroupWire {
  readonly id?: string;
  readonly item_ids?: readonly string[];
  readonly reason?: string;
  readonly severity?: string;
}

export interface ReplatformingBlastRadiusSummaryWire {
  readonly group_id?: string;
  readonly item_count?: number;
  readonly severity?: string;
}

export interface ReplatformingOwnershipWire {
  readonly ambiguous_count?: number;
  readonly limit?: number;
  readonly next_offset?: number | null;
  readonly offset?: number;
  readonly ownership_packets?: readonly ReplatformingOwnershipPacketWire[];
  readonly packets_count?: number;
  readonly rejected_count?: number;
  readonly story?: string;
  readonly total_findings_count?: number;
  readonly truncated?: boolean;
  readonly unattributed_count?: number;
}

export interface ReplatformingOwnershipPacketWire {
  readonly freshness?: { readonly state?: string };
  readonly item_id?: string;
  readonly missing_evidence?: readonly string[];
  readonly owner_candidates?: readonly ReplatformingOwnerCandidateWire[];
  readonly provider?: string;
  readonly resource_type?: string;
  readonly safety_gate?: ReplatformingSafetyGateWire;
  readonly source_state?: string;
  readonly stable_id?: string;
}
