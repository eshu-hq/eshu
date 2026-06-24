// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package doctruth

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	// ClaimTypeCLICommand identifies documentation claims about Eshu CLI commands.
	ClaimTypeCLICommand = "cli_command"
	// ClaimTypeHTTPEndpoint identifies documentation claims about HTTP API endpoints.
	ClaimTypeHTTPEndpoint = "http_endpoint"
	// ClaimTypeEnvironmentVariable identifies documentation claims about Eshu env vars.
	ClaimTypeEnvironmentVariable = "environment_variable"
	// ClaimTypeLocalPath identifies documentation claims about local repo paths.
	ClaimTypeLocalPath = "local_path"
	// ClaimTypeContainerImageRef identifies documentation claims about explicit container image refs.
	ClaimTypeContainerImageRef = "container_image_ref"
	// ClaimTypeTerraformAddress identifies documentation claims about explicit Terraform block addresses.
	ClaimTypeTerraformAddress = "terraform_address"
	// ClaimTypeShellCommand identifies a shell command outside this verifier slice.
	ClaimTypeShellCommand = "shell_command"

	// VerificationStatusValid means the claim matched a concrete truth source.
	VerificationStatusValid = "valid"
	// VerificationStatusContradicted means the claim conflicts with a truth source.
	VerificationStatusContradicted = "contradicted"
	// VerificationStatusMissingEvidence means the verifier had no matching truth source.
	VerificationStatusMissingEvidence = "missing_evidence"
	// VerificationStatusUnsupportedClaimType means extraction found a claim family that is not checked yet.
	VerificationStatusUnsupportedClaimType = "unsupported_claim_type"

	documentationClaimVerificationFindingType = "documentation_claim_verification"
	defaultMaxDocuments                       = 50
	defaultMaxDocumentBytes                   = 256 * 1024
)

var (
	backtickPattern     = regexp.MustCompile("`([^`]+)`")
	httpEndpointPattern = regexp.MustCompile(`\b(GET|POST|PUT|PATCH|DELETE)\s+(/[A-Za-z0-9{}_.:/?=&%-]+)`)
	envVarPattern       = regexp.MustCompile(`\bESHU_[A-Z0-9_]*[A-Z0-9]\b`)
)

// CommandTruth describes one supported Eshu CLI command path.
type CommandTruth struct {
	Path       []string
	AllowsArgs bool
}

// HTTPEndpointTruth describes one supported HTTP endpoint.
type HTTPEndpointTruth struct {
	Method string
	Path   string
}

// DocumentInput is one bounded documentation document revision to verify.
type DocumentInput struct {
	Path             string
	SourceURI        string
	RevisionID       string
	Content          string
	ContentTruncated bool
	ObservedAt       time.Time
}

// VerifierOptions configures documentation verification truth sources and bounds.
type VerifierOptions struct {
	Commands               []CommandTruth
	HTTPEndpoints          []HTTPEndpointTruth
	EnvironmentVariables   []string
	LocalPathResolver      LocalPathResolver
	ContainerImageResolver ContainerImageResolver
	TerraformResolver      TerraformAddressResolver
	MaxDocuments           int
	MaxDocumentBytes       int
	ScopeID                string
	GenerationID           string
	SourceSystem           string
	Now                    func() time.Time
}

// VerificationSummary reports bounded verification counters.
type VerificationSummary struct {
	DocumentsScanned      int `json:"documents_scanned"`
	BytesScanned          int `json:"bytes_scanned"`
	ClaimsChecked         int `json:"claims_checked"`
	Valid                 int `json:"valid"`
	Contradicted          int `json:"contradicted"`
	MissingEvidence       int `json:"missing_evidence"`
	UnsupportedClaimType  int `json:"unsupported_claim_type"`
	EvidencePackets       int `json:"evidence_packets"`
	DocumentationFindings int `json:"documentation_findings"`
}

// VerificationFinding is one documentation claim verification finding.
type VerificationFinding struct {
	FindingID        string `json:"finding_id"`
	FindingVersion   string `json:"finding_version"`
	FindingType      string `json:"finding_type"`
	Status           string `json:"status"`
	TruthLevel       string `json:"truth_level"`
	FreshnessState   string `json:"freshness_state"`
	SourceID         string `json:"source_id"`
	DocumentID       string `json:"document_id"`
	SectionID        string `json:"section_id"`
	ClaimID          string `json:"claim_id"`
	ClaimType        string `json:"claim_type"`
	ClaimText        string `json:"claim_text"`
	NormalizedClaim  string `json:"normalized_claim"`
	Summary          string `json:"summary"`
	EvidencePacketID string `json:"evidence_packet_id"`
	// ClaimByteOffset is the document-absolute byte offset of the first byte of
	// ClaimText in the source document. ClaimByteLength is len(ClaimText). Both
	// are zero when extraction could not determine the byte position (for example
	// when the claim text was not located after line splitting). Callers must
	// treat a zero ClaimByteLength as "byte window absent" and must never
	// fabricate an offset.
	ClaimByteOffset int `json:"claim_byte_offset,omitempty"`
	ClaimByteLength int `json:"claim_byte_length,omitempty"`
}

// VerificationEvidencePacket is an immutable packet supporting one finding.
type VerificationEvidencePacket struct {
	PacketID      string         `json:"packet_id"`
	PacketVersion string         `json:"packet_version"`
	FindingID     string         `json:"finding_id"`
	Payload       map[string]any `json:"payload"`
}

// VerificationResult contains generated findings, packets, and durable fact envelopes.
type VerificationResult struct {
	Findings        []VerificationFinding        `json:"findings"`
	EvidencePackets []VerificationEvidencePacket `json:"evidence_packets"`
	Envelopes       []facts.Envelope             `json:"-"`
	Summary         VerificationSummary          `json:"summary"`
	Truncated       bool                         `json:"truncated"`
}

// Verifier actively checks documentation claims against supplied truth sources.
type Verifier struct {
	commands          map[string]struct{}
	argCommands       map[string]struct{}
	endpoints         map[string]struct{}
	endpointTemplates []endpointTemplate
	envVars           map[string]struct{}
	localPaths        LocalPathResolver
	containerImages   ContainerImageResolver
	terraform         TerraformAddressResolver
	maxDocuments      int
	maxBytes          int
	scopeID           string
	generationID      string
	sourceSystem      string
	now               func() time.Time
}

type endpointTemplate struct {
	method string
	regex  *regexp.Regexp
}

// NewVerifier constructs a bounded documentation verifier.
func NewVerifier(options VerifierOptions) *Verifier {
	now := options.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	v := &Verifier{
		commands:        map[string]struct{}{},
		argCommands:     map[string]struct{}{},
		endpoints:       map[string]struct{}{},
		envVars:         map[string]struct{}{},
		localPaths:      options.LocalPathResolver,
		containerImages: options.ContainerImageResolver,
		terraform:       options.TerraformResolver,
		maxDocuments:    options.MaxDocuments,
		maxBytes:        options.MaxDocumentBytes,
		scopeID:         firstNonEmpty(options.ScopeID, "documentation-verify-local"),
		generationID:    firstNonEmpty(options.GenerationID, defaultGenerationID(now())),
		sourceSystem:    firstNonEmpty(options.SourceSystem, "local_docs"),
		now:             now,
	}
	if v.maxDocuments <= 0 {
		v.maxDocuments = defaultMaxDocuments
	}
	if v.maxBytes <= 0 {
		v.maxBytes = defaultMaxDocumentBytes
	}
	for _, command := range options.Commands {
		key := commandKey(command.Path)
		if key != "" {
			v.commands[key] = struct{}{}
			if command.AllowsArgs {
				v.argCommands[key] = struct{}{}
			}
		}
	}
	for _, endpoint := range options.HTTPEndpoints {
		key := endpointKey(endpoint.Method, endpoint.Path)
		if key != "" {
			v.endpoints[key] = struct{}{}
		}
		if template := newEndpointTemplate(endpoint.Method, endpoint.Path); template.regex != nil {
			v.endpointTemplates = append(v.endpointTemplates, template)
		}
	}
	for _, envVar := range options.EnvironmentVariables {
		envVar = strings.TrimSpace(envVar)
		if envVar != "" {
			v.envVars[envVar] = struct{}{}
		}
	}
	return v
}

// Verify extracts supported claims and emits documentation finding facts.
func (v *Verifier) Verify(ctx context.Context, documents []DocumentInput) (VerificationResult, error) {
	result := VerificationResult{}
	limit := v.maxDocuments
	if len(documents) < limit {
		limit = len(documents)
	}
	if len(documents) > limit {
		result.Truncated = true
	}
	for i := 0; i < limit; i++ {
		select {
		case <-ctx.Done():
			return VerificationResult{}, ctx.Err()
		default:
		}
		doc := documents[i]
		content := doc.Content
		if len(content) > v.maxBytes {
			content = string([]byte(content)[:v.maxBytes])
			result.Truncated = true
		}
		if doc.ContentTruncated {
			result.Truncated = true
		}
		result.Summary.DocumentsScanned++
		result.Summary.BytesScanned += len(content)
		for _, claim := range extractClaims(content) {
			finding, packet, envelopes := v.verifyClaim(doc, claim)
			result.Findings = append(result.Findings, finding)
			result.EvidencePackets = append(result.EvidencePackets, packet)
			result.Envelopes = append(result.Envelopes, envelopes...)
			result.Summary.add(finding.Status)
		}
	}
	result.Summary.EvidencePackets = len(result.EvidencePackets)
	result.Summary.DocumentationFindings = len(result.Findings)
	return result, nil
}

func (v *Verifier) verifyClaim(doc DocumentInput, claim extractedClaim) (VerificationFinding, VerificationEvidencePacket, []facts.Envelope) {
	status := VerificationStatusUnsupportedClaimType
	truthLevel := string(TruthLevelDerived)
	switch claim.claimType {
	case ClaimTypeCLICommand:
		if v.commandMatches(claim.normalized) {
			status = VerificationStatusValid
			truthLevel = string(TruthLevelExact)
		} else {
			status = VerificationStatusContradicted
		}
	case ClaimTypeHTTPEndpoint:
		if _, ok := v.endpoints[claim.normalized]; ok || v.endpointTemplateMatches(claim.normalized) {
			status = VerificationStatusValid
			truthLevel = string(TruthLevelExact)
		} else {
			status = VerificationStatusContradicted
		}
	case ClaimTypeEnvironmentVariable:
		if _, ok := v.envVars[claim.normalized]; ok {
			status = VerificationStatusValid
			truthLevel = string(TruthLevelExact)
		} else {
			status = VerificationStatusMissingEvidence
		}
	case ClaimTypeLocalPath:
		if v.localPaths == nil {
			status = VerificationStatusUnsupportedClaimType
			break
		}
		resolution := v.localPaths(doc, claim.normalized)
		if !resolution.Supported {
			status = VerificationStatusMissingEvidence
		} else if resolution.Exists {
			status = VerificationStatusValid
			truthLevel = string(TruthLevelExact)
		} else {
			status = VerificationStatusContradicted
		}
	case ClaimTypeContainerImageRef:
		if v.containerImages == nil {
			status = VerificationStatusUnsupportedClaimType
			break
		}
		resolution := v.containerImages(doc, claim.normalized)
		if !resolution.Supported {
			status = VerificationStatusMissingEvidence
		} else if resolution.Exists {
			status = VerificationStatusValid
			truthLevel = string(TruthLevelExact)
		} else {
			status = VerificationStatusContradicted
		}
	case ClaimTypeTerraformAddress:
		if v.terraform == nil {
			status = VerificationStatusUnsupportedClaimType
			break
		}
		resolution := v.terraform(doc, claim.normalized)
		if !resolution.Supported {
			status = VerificationStatusMissingEvidence
		} else if resolution.Exists {
			status = VerificationStatusValid
			truthLevel = string(TruthLevelExact)
		} else {
			status = VerificationStatusContradicted
		}
	}
	version := v.version()
	canonicalURI := canonicalDocumentURI(v.sourceSystem, doc)
	sourceID := "doc-source:" + facts.StableID("documentation-source", map[string]any{
		"source_system": v.sourceSystem,
		"canonical_uri": canonicalURI,
	})
	documentID := "doc:" + facts.StableID("documentation-document", map[string]any{
		"source_id":     sourceID,
		"canonical_uri": canonicalURI,
		"path":          doc.Path,
	})
	sectionID := "line:" + facts.StableID("documentation-line", map[string]any{"document_id": documentID, "line": claim.line})
	claimID := "claim:" + facts.StableID("documentation-claim", map[string]any{
		"document_id": documentID,
		"line":        claim.line,
		"type":        claim.claimType,
		"claim":       claim.normalized,
	})
	findingID := "finding:" + facts.StableID(documentationClaimVerificationFindingType, map[string]any{
		"document_id": documentID,
		"claim_id":    claimID,
		"claim":       claim.normalized,
	})
	packetID := "doc-packet:" + facts.StableID(documentationClaimVerificationFindingType, map[string]any{
		"finding_id": findingID,
		"version":    version,
	})
	finding := VerificationFinding{
		FindingID:        findingID,
		FindingVersion:   version,
		FindingType:      documentationClaimVerificationFindingType,
		Status:           status,
		TruthLevel:       truthLevel,
		FreshnessState:   string(FreshnessFresh),
		SourceID:         sourceID,
		DocumentID:       documentID,
		SectionID:        sectionID,
		ClaimID:          claimID,
		ClaimType:        claim.claimType,
		ClaimText:        claim.text,
		NormalizedClaim:  claim.normalized,
		Summary:          verificationSummaryText(claim, status),
		EvidencePacketID: packetID,
		ClaimByteOffset:  claim.byteOffset,
		ClaimByteLength:  claim.byteLength,
	}
	packetPayload := v.evidencePacketPayload(doc, claim, finding, packetID, canonicalURI)
	packet := VerificationEvidencePacket{
		PacketID:      packetID,
		PacketVersion: version,
		FindingID:     findingID,
		Payload:       packetPayload,
	}
	findingPayload := findingPayload(finding)
	return finding, packet, []facts.Envelope{
		v.envelope(facts.DocumentationFindingFactKind, facts.DocumentationFindingStableID(findingID, version), findingPayload),
		v.envelope(facts.DocumentationEvidencePacketFactKind, facts.DocumentationEvidencePacketStableID(packetID, version), packetPayload),
	}
}

func (v *Verifier) commandMatches(normalized string) bool {
	if _, ok := v.commands[normalized]; ok {
		return true
	}
	for command := range v.argCommands {
		if strings.HasPrefix(normalized, command+" ") {
			return true
		}
	}
	return false
}

func (v *Verifier) evidencePacketPayload(
	doc DocumentInput,
	claim extractedClaim,
	finding VerificationFinding,
	packetID string,
	canonicalURI string,
) map[string]any {
	version := finding.FindingVersion
	excerptHash := facts.StableID("documentation-excerpt", map[string]any{"claim": claim.text})
	canonicalEvidence := finding.Canonical(excerptHash)
	return map[string]any{
		"packet_id":      packetID,
		"packet_version": version,
		"generated_at":   version,
		"finding_id":     finding.FindingID,
		"finding":        findingPayload(finding),
		"unified_evidence": map[string]any{
			"kind":       canonicalEvidence.Kind,
			"confidence": canonicalEvidence.Confidence,
			"citation":   canonicalEvidenceCitationMap(canonicalEvidence.Citation),
			"provenance": map[string]any{
				"basis":     string(canonicalEvidence.Provenance.Basis),
				"rationale": canonicalEvidence.Provenance.Rationale,
				"source":    canonicalEvidence.Provenance.Source,
			},
		},
		"document": map[string]any{
			"source_id":     finding.SourceID,
			"document_id":   finding.DocumentID,
			"canonical_uri": canonicalURI,
			"revision_id":   doc.RevisionID,
			"title":         doc.Path,
			"truncated":     doc.ContentTruncated,
		},
		"section": map[string]any{
			"section_id":       finding.SectionID,
			"source_start_ref": finding.SectionID,
			"source_end_ref":   finding.SectionID,
		},
		"bounded_excerpt": map[string]any{
			"text":             claim.text,
			"text_hash":        excerptHash,
			"source_start_ref": finding.SectionID,
			"source_end_ref":   finding.SectionID,
		},
		"linked_entities": []any{},
		"current_truth": map[string]any{
			"claim_key":        claim.claimType,
			"documented_value": claim.normalized,
			"truth_level":      finding.TruthLevel,
			"freshness_state":  finding.FreshnessState,
		},
		"evidence_refs": []any{map[string]any{
			"source_system":    v.sourceSystem,
			"source_uri":       canonicalURI,
			"source_record_id": doc.Path,
		}},
		"truth": map[string]any{
			"label":     finding.TruthLevel,
			"basis":     "documentation verifier truth source comparison",
			"ambiguity": []any{},
		},
		"permissions": visibilityPayload(),
		"states": map[string]any{
			"finding_state":       finding.Status,
			"freshness_state":     finding.FreshnessState,
			"permission_decision": "allowed",
		},
	}
}
