// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package semanticguard

const (
	// StateAllowed means the chunk or provider response passed every guard.
	StateAllowed = "allowed"
	// StateDeniedByPolicy means policy, source, provider, or budget state denied work.
	StateDeniedByPolicy = "denied_by_policy"
	// StateDeniedByACL means ACL state is not fresh and allowed.
	StateDeniedByACL = "denied_by_acl"
	// StateDeniedUnclassifiedSource means source content could not be classified safely.
	StateDeniedUnclassifiedSource = "denied_unclassified_source"
	// StateDeniedSensitiveData means sensitive content remained or was disallowed.
	StateDeniedSensitiveData = "denied_sensitive_data"
	// StateDeniedPromptInjectionRisk means prompt-control indicators were present.
	StateDeniedPromptInjectionRisk = "denied_prompt_injection_risk"
	// StateDeniedUnsupportedFormat means the extractor did not emit approved bounded text.
	StateDeniedUnsupportedFormat = "denied_unsupported_format"
	// StateDeniedOversizedChunk means byte or token budgets denied the chunk.
	StateDeniedOversizedChunk = "denied_oversized_chunk"
	// StateDeniedRetentionPolicy means retention settings exceeded metadata-only policy.
	StateDeniedRetentionPolicy = "denied_retention_policy"
	// StateRedactedEmpty means redaction removed all useful prompt content.
	StateRedactedEmpty = "redacted_empty"
	// StateResponseRejected means provider output failed schema, safety, or retention checks.
	StateResponseRejected = "response_rejected"
)

const (
	// ReasonAllowed marks an allowed guard decision.
	ReasonAllowed = "allowed"
	// ReasonPolicyNotAllowed marks a denied upstream policy decision.
	ReasonPolicyNotAllowed = "policy_not_allowed"
	// ReasonUnsupportedSourceClass marks a source class outside the semantic contract.
	ReasonUnsupportedSourceClass = "unsupported_source_class"
	// ReasonInvalidBudget marks missing or invalid policy limits.
	ReasonInvalidBudget = "invalid_budget"
	// ReasonACLNotAllowed marks stale, missing, partial, or denied ACL state.
	ReasonACLNotAllowed = "acl_not_allowed"
	// ReasonExtractorNotApproved marks an extractor state that cannot feed a prompt.
	ReasonExtractorNotApproved = "extractor_not_approved"
	// ReasonChunkTooLarge marks a byte budget violation.
	ReasonChunkTooLarge = "chunk_too_large"
	// ReasonTokenBudgetExceeded marks a token budget violation.
	ReasonTokenBudgetExceeded = "token_budget_exceeded" // #nosec G101 -- reason-code label for a budget decision, not a credential or token value
	// ReasonClassifierMissing marks a missing classifier version or result set.
	ReasonClassifierMissing = "classifier_missing"
	// ReasonUnknownDataClass marks a data class outside the approved taxonomy.
	ReasonUnknownDataClass = "unknown_data_class"
	// ReasonUnknownClassificationAction marks an unrecognized classification action.
	ReasonUnknownClassificationAction = "unknown_classification_action"
	// ReasonDataClassDenied marks sensitive content denied by default or policy.
	ReasonDataClassDenied = "data_class_denied"
	// ReasonRedactionIncomplete marks missing or unsafe redaction evidence.
	ReasonRedactionIncomplete = "redaction_incomplete"
	// ReasonPromptInjectionIndicator marks direct or indirect injection signal.
	ReasonPromptInjectionIndicator = "prompt_injection_indicator"
	// ReasonPromptSafetyMissing marks missing prompt-safety classifier metadata.
	ReasonPromptSafetyMissing = "prompt_safety_missing"
	// ReasonRetentionPostureDenied marks a non-metadata-only retention posture.
	ReasonRetentionPostureDenied = "retention_posture_denied"
	// ReasonRawPromptRetentionDenied marks raw prompt retention.
	ReasonRawPromptRetentionDenied = "raw_prompt_retention_denied"
	// ReasonRawResponseRetentionDenied marks raw provider response retention.
	ReasonRawResponseRetentionDenied = "raw_response_retention_denied"
	// ReasonEmptyAfterRedaction marks a chunk with no redacted prompt-safe text.
	ReasonEmptyAfterRedaction = "empty_after_redaction"
	// ReasonResponseSchemaInvalid marks provider output that failed schema parsing.
	ReasonResponseSchemaInvalid = "response_schema_invalid"
	// ReasonResponseSensitiveData marks sensitive data in provider output.
	ReasonResponseSensitiveData = "response_sensitive_data"
	// ReasonResponseHashMissing marks accepted output missing its audit hash.
	ReasonResponseHashMissing = "response_hash_missing"
)

const (
	// SourceDocumentation is documentation text eligible for semantic observations.
	SourceDocumentation = "documentation"
	// SourceDiagramsImages is deterministic text extracted from diagrams or images.
	SourceDiagramsImages = "diagrams_images"
	// SourceTicketsChat is ticket, chat, support, or incident material.
	SourceTicketsChat = "tickets_chat"
	// SourceCodeHints is source text considered for non-canonical code hints.
	SourceCodeHints = "code_hints"
)

const (
	// ACLAllowed means source ACLs permit semantic content egress.
	ACLAllowed = "allowed"
	// ACLDenied means source ACLs deny semantic content egress.
	ACLDenied = "denied"
	// ACLPartial means the ACL check was incomplete and must fail closed.
	ACLPartial = "partial"
	// ACLMissing means no ACL decision was available.
	ACLMissing = "missing"
	// ACLStale means the ACL decision is not current for the source revision.
	ACLStale = "stale"
)

const (
	// ExtractorApproved means deterministic extraction emitted bounded text.
	ExtractorApproved = "approved"
)

const (
	// DataClassSemanticContent is ordinary semantic input after source allowlisting.
	DataClassSemanticContent = "semantic_content"
	// DataClassCredential covers API keys, tokens, private keys, and cloud credentials.
	DataClassCredential = "credential"
	// DataClassSecretReference covers secret names, env handles, and Vault paths.
	DataClassSecretReference = "secret_reference"
	// DataClassPrivateURL covers private URLs, local paths, and private remotes.
	DataClassPrivateURL = "private_url"
	// DataClassPersonalData covers person-identifying data.
	DataClassPersonalData = "personal_data"
	// DataClassCustomerData covers tenant, customer, account, and support data.
	DataClassCustomerData = "customer_data"
	// DataClassProprietaryCode covers private source snippets and algorithms.
	DataClassProprietaryCode = "proprietary_code"
	// DataClassIncidentTicketChat covers incidents, tickets, chats, and support notes.
	DataClassIncidentTicketChat = "incident_ticket_chat"
	// DataClassRawLogsTraces covers logs, traces, dashboards, dumps, and profiles.
	DataClassRawLogsTraces = "raw_logs_traces"
	// DataClassPromptControl covers prompts, guardrails, and tool instructions.
	DataClassPromptControl = "prompt_control"
	// DataClassActiveOrHiddenContent covers macros, hidden text, scripts, and includes.
	DataClassActiveOrHiddenContent = "active_or_hidden_content"
	// DataClassBinaryOrArchive covers raw binary or archive content before extraction.
	DataClassBinaryOrArchive = "binary_or_archive"
	// DataClassUnknownSensitive covers sensitive material without a stable class.
	DataClassUnknownSensitive = "unknown_sensitive"
)

const (
	// ActionAllow preserves a classified class as prompt-safe.
	ActionAllow = "allow"
	// ActionRedact replaces a classified class with a safe marker.
	ActionRedact = "redact"
	// ActionFingerprint replaces a classified class with a safe fingerprint.
	ActionFingerprint = "fingerprint"
	// ActionDrop removes a classified class from prompt input.
	ActionDrop = "drop"
	// ActionDeny blocks the chunk.
	ActionDeny = "deny"
	// ActionNeedsReview blocks the chunk until human or security review.
	ActionNeedsReview = "needs_review"
)

const (
	// RedactionStrict requires deterministic redaction before provider use.
	RedactionStrict = "strict"
	// RedactionStandard allows the standard semantic redaction policy.
	RedactionStandard = "standard"
	// RedactionComplete means redaction finished and re-scan found no residue.
	RedactionComplete = "complete"
	// RedactionIncomplete means redaction evidence is missing or unsafe.
	RedactionIncomplete = "incomplete"
)

const (
	// RetentionMetadataOnly means raw prompt and response bodies are not retained.
	RetentionMetadataOnly = "metadata_only"
	// RetentionNone means the material is not retained.
	RetentionNone = "none"
	// RetentionHashOnly means only a hash or fingerprint is retained.
	RetentionHashOnly = "hash_only"
	// RetentionBoundedExcerpt means a reviewed redacted excerpt may be retained.
	RetentionBoundedExcerpt = "bounded_excerpt"
)

const (
	// ResponseSchemaValid means provider output matched the expected schema.
	ResponseSchemaValid = "valid"
	// ResponseSchemaInvalid means provider output failed schema validation.
	ResponseSchemaInvalid = "invalid"
)

// PolicyGate carries the already-evaluated provider and source-policy result.
type PolicyGate struct {
	Allowed           bool
	Reason            string
	PolicyID          string
	RuleID            string
	ProviderProfileID string
	SourceClass       string
}

// Limits bounds one semantic chunk before prompt construction.
type Limits struct {
	MaxChunkBytes     int64
	MaxTokensPerChunk int64
}

// Extractor records deterministic text extraction state for a source chunk.
type Extractor struct {
	State       string
	Version     string
	BoundedText bool
}

// Chunk carries redacted text metadata for one source chunk.
type Chunk struct {
	RedactedText  string
	SourceHash    string
	ChunkHash     string
	ByteCount     int64
	TokenEstimate int64
}

// Classification records one low-cardinality data class and its safe action.
type Classification struct {
	Class           string
	Action          string
	Count           int
	AllowedByPolicy bool
}

// RedactionSummary records audit-safe redaction evidence for one chunk.
type RedactionSummary struct {
	PolicyVersion              string
	Mode                       string
	State                      string
	DataClassesSeen            []string
	RedactedCountsByClass      map[string]int
	FingerprintedCountsByClass map[string]int
	DroppedCountsByReason      map[string]int
	PromptInjectionIndicators  []string
	UnsafeReason               string
	SourceHash                 string
	ChunkHash                  string
	Truncated                  bool
	RetentionPosture           string
}

// PromptSafety records prompt-injection classifier metadata.
type PromptSafety struct {
	Version    string
	Indicators []string
}

// Retention records the prompt and response retention posture requested.
type Retention struct {
	Posture  string
	Prompt   string
	Response string
}

// Assessment is the side-effect-free guard input for one source chunk.
type Assessment struct {
	Policy            PolicyGate
	ACLState          string
	ActorClass        string
	ClassifierVersion string
	Limits            Limits
	Extractor         Extractor
	Chunk             Chunk
	Classifications   []Classification
	Redaction         RedactionSummary
	PromptSafety      PromptSafety
	Retention         Retention
}

// ResponseAssessment is the side-effect-free guard input for provider output.
type ResponseAssessment struct {
	SchemaState       string
	ResponseHash      string
	ClassifierVersion string
	Classifications   []Classification
	PromptSafety      PromptSafety
	Retention         Retention
}

// Decision records an audit-safe guard decision for a chunk or response.
type Decision struct {
	Allowed           bool
	State             string
	Reason            string
	Detail            string
	PolicyID          string
	RuleID            string
	ProviderProfileID string
	SourceClass       string
	ActorClass        string
	ACLState          string
	ClassifierVersion string
	SourceHash        string
	ChunkHash         string
	ResponseHash      string
	PromptSafeText    string
	RedactionSummary  RedactionSummary
}
