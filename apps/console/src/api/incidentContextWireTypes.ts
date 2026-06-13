export interface IncidentContextResponse {
  readonly ambiguous_evidence?: readonly IncidentEvidenceEdgeRecord[];
  readonly answer_metadata?: IncidentAnswerMetadataRecord;
  readonly evidence_path?: readonly IncidentEvidenceEdgeRecord[];
  readonly incident?: IncidentRecordWire;
  readonly missing_evidence?: readonly IncidentMissingEvidenceRecord[];
  readonly query?: IncidentContextQueryRecord;
  readonly related_changes?: readonly IncidentRelatedChangeRecord[];
  readonly timeline?: readonly IncidentTimelineEventRecord[];
  readonly truncated?: boolean;
}

export interface IncidentContextQueryRecord {
  readonly limit?: number;
  readonly provider?: string;
  readonly provider_incident_id?: string;
  readonly scope_id?: string;
  readonly service_id?: string;
  readonly since?: string;
  readonly until?: string;
}

export interface IncidentRecordWire {
  readonly created_at?: string;
  readonly evidence_fact_id?: string;
  readonly incident_number?: number;
  readonly observed_at?: string;
  readonly priority?: IncidentReferenceRecord;
  readonly provider?: string;
  readonly provider_incident_id?: string;
  readonly resolved_at?: string;
  readonly scope_id?: string;
  readonly service?: IncidentReferenceRecord;
  readonly source_confidence?: string;
  readonly source_url?: string;
  readonly status?: string;
  readonly teams?: readonly IncidentReferenceRecord[];
  readonly title?: string;
  readonly updated_at?: string;
  readonly urgency?: string;
}

export interface IncidentReferenceRecord {
  readonly id?: string;
  readonly summary?: string;
  readonly type?: string;
  readonly url?: string;
}

export interface IncidentEvidenceEdgeRecord {
  readonly candidates?: readonly IncidentEvidenceCandidateRecord[];
  readonly evidence?: readonly IncidentEvidenceRefRecord[];
  readonly explanation?: string;
  readonly slot?: string;
  readonly truth_label?: string;
  readonly value?: Record<string, string>;
}

export interface IncidentEvidenceCandidateRecord {
  readonly id?: string;
  readonly label?: string;
  readonly reason?: string;
  readonly url?: string;
}

export interface IncidentEvidenceRefRecord {
  readonly confidence?: string;
  readonly fact_id?: string;
  readonly kind?: string;
  readonly observed_at?: string;
  readonly record_id?: string;
  readonly source?: string;
  readonly url?: string;
}

export interface IncidentMissingEvidenceRecord {
  readonly reason?: string;
  readonly slot?: string;
}

export interface IncidentRelatedChangeRecord {
  readonly change_id?: string;
  readonly evidence_fact_id?: string;
  readonly explanation?: string;
  readonly services?: readonly IncidentReferenceRecord[];
  readonly source?: string;
  readonly source_confidence?: string;
  readonly source_url?: string;
  readonly summary?: string;
  readonly timestamp?: string;
  readonly truth_label?: string;
}

export interface IncidentTimelineEventRecord {
  readonly created_at?: string;
  readonly event_id?: string;
  readonly event_type?: string;
  readonly summary?: string;
}

export interface IncidentAnswerMetadataRecord {
  readonly coverage?: {
    readonly limit?: number;
    readonly query_shape?: string;
  };
  readonly partial_reasons?: readonly string[];
  readonly recommended_next_calls?: readonly IncidentRecommendedNextCallRecord[];
  readonly truncated?: boolean;
}

export interface IncidentRecommendedNextCallRecord {
  readonly args?: Record<string, unknown>;
  readonly reason?: string;
  readonly route?: string;
  readonly tool?: string;
}
