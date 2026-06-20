package evidencebundle

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

const (
	defaultProfile   = "local_authoritative"
	defaultCreatedAt = "2026-06-20T00:00:00Z"
)

var (
	privateEndpointPattern = regexp.MustCompile(`https?://[^/"\s]*(internal|localhost|127\.0\.0\.1|10\.|192\.168\.|172\.(1[6-9]|2[0-9]|3[0-1])\.)`)
	credentialPattern      = regexp.MustCompile(`(?i)(authorization:\s*bearer|api[_-]?key|password|secret|\\?"?token\\?"?\s*[:=]|gh[pousr]_[A-Za-z0-9_]{8,}|-----BEGIN [A-Z ]*PRIVATE KEY-----)`)
	rawPromptPattern       = regexp.MustCompile(`(?i)(raw_prompt|provider_response|raw provider response|prompt transcript)`)
	localPathPattern       = regexp.MustCompile(`(^|["\s])(/Users/|/home/|/workspace/|/workspaces/|/tmp/|/private/|/var/|/opt/|/srv/|/mnt/|/Volumes/|[A-Za-z]:\\)`)
)

// BuildDemoBundle builds a deterministic share-safe fixture bundle.
func BuildDemoBundle(opts DemoBundleOptions) Bundle {
	scopeID := strings.TrimSpace(opts.ScopeID)
	if scopeID == "" {
		scopeID = "repo:demo/service"
	}
	bundle := Bundle{
		SchemaVersion: SchemaVersion,
		Identity: Identity{
			ScopeID:   scopeID,
			Profile:   defaultProfile,
			CreatedAt: defaultCreatedAt,
		},
		Source: SourceIdentity{
			Repository: "repo:demo/service",
			Deployment: "deployment:demo/service",
		},
		Redaction: RedactionProfile{
			Profile: "share_safe_v1",
			Rules: []string{
				"handles_only",
				"no_private_endpoints",
				"no_credentials",
				"no_model_inputs_or_outputs",
			},
		},
		Contents: Contents{
			AnswerPackets: []PacketSummary{
				{
					Family:     "ask_eshu",
					Schema:     "answer_packet.v1",
					TruthClass: "derived",
					Summary:    "Ask Eshu answer references capability, freshness, and packet handles.",
					EvidenceHandles: []string{
						"answer:ask-eshu:demo",
						"capability:ask.eshu",
					},
					NextCalls: []string{"POST /api/v0/ask"},
				},
				{
					Family:     "pre_change_impact",
					Schema:     "answer_packet.v1",
					TruthClass: "derived",
					Summary:    "Pre-change impact answer preserves changed-file status and missing evidence.",
					EvidenceHandles: []string{
						"impact:pre-change:demo",
						"source:file:service/main.go",
					},
					NextCalls: []string{"POST /api/v0/impact/pre-change", "eshu change impact"},
				},
			},
			InvestigationPackets: []PacketSummary{{
				Family:     "supply_chain_impact",
				Schema:     "investigation_evidence_packet.v2",
				TruthClass: "exact",
				Summary:    "Supply-chain packet links advisory, package, workload, and service handles.",
				EvidenceHandles: []string{
					"advisory:GHSA-demo",
					"package:pkg:golang/example.com/demo",
					"service:demo",
				},
				NextCalls: []string{"GET /api/v0/investigations/supply-chain/impact/packet"},
			}},
			CapabilityCatalog: CatalogSnapshot{
				Schema:     "capability_catalog.v1",
				EntryCount: 4,
				Handles: []string{
					"capability:ask.eshu",
					"capability:platform.impact.pre_change",
					"capability:supply_chain.impact_explain",
				},
			},
			SurfaceInventory: CatalogSnapshot{
				Schema:       "surface_inventory.v1",
				SurfaceCount: 4,
				Handles: []string{
					"cli:eshu change impact",
					"mcp:analyze_pre_change_impact",
					"route:POST /api/v0/impact/pre-change",
				},
			},
			OperatorState: []OperatorStateItem{
				{Kind: "freshness", State: "fresh"},
				{Kind: "readiness", State: "ready_with_findings"},
			},
		},
		Missing: []MissingEvidence{{
			Family: "pre_change_impact",
			Reason: "deleted_path_requires_prior_generation",
		}},
		Reproduce: []ReproduceCall{
			{
				Kind:   "cli",
				Target: "eshu change impact",
				Args:   map[string]string{"repo_id": scopeID},
			},
			{
				Kind:   "api",
				Target: "POST /api/v0/impact/pre-change",
				Args:   map[string]string{"repo_id": scopeID},
			},
			{
				Kind:   "mcp",
				Target: "analyze_pre_change_impact",
				Args:   map[string]string{"repo_id": scopeID},
			},
		},
		Bounds: Bounds{
			MaxAnswerPackets:        25,
			MaxInvestigationPackets: 25,
			MaxHandles:              200,
		},
		Validation: Validation{
			Status: "passed",
			Checks: []string{
				"schema",
				"redaction",
				"private_data_canaries",
				"reproduce_handles",
			},
		},
	}
	sortBundle(&bundle)
	bundle.BundleID = bundleID(bundle)
	return bundle
}

// RenderJSON serializes a bundle as stable, indented JSON.
func RenderJSON(bundle Bundle) ([]byte, error) {
	sortBundle(&bundle)
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(bundle); err != nil {
		return nil, fmt.Errorf("encode evidence bundle: %w", err)
	}
	return buf.Bytes(), nil
}

func sortBundle(bundle *Bundle) {
	sort.Strings(bundle.Redaction.Rules)
	sortPacketSummaries(bundle.Contents.AnswerPackets)
	sortPacketSummaries(bundle.Contents.InvestigationPackets)
	sort.Strings(bundle.Contents.CapabilityCatalog.Handles)
	sort.Strings(bundle.Contents.SurfaceInventory.Handles)
	sort.Slice(bundle.Contents.OperatorState, func(i, j int) bool {
		if bundle.Contents.OperatorState[i].Kind != bundle.Contents.OperatorState[j].Kind {
			return bundle.Contents.OperatorState[i].Kind < bundle.Contents.OperatorState[j].Kind
		}
		return bundle.Contents.OperatorState[i].State < bundle.Contents.OperatorState[j].State
	})
	sort.Slice(bundle.Missing, func(i, j int) bool {
		if bundle.Missing[i].Family != bundle.Missing[j].Family {
			return bundle.Missing[i].Family < bundle.Missing[j].Family
		}
		return bundle.Missing[i].Reason < bundle.Missing[j].Reason
	})
	sort.Slice(bundle.Reproduce, func(i, j int) bool {
		if bundle.Reproduce[i].Kind != bundle.Reproduce[j].Kind {
			return bundle.Reproduce[i].Kind < bundle.Reproduce[j].Kind
		}
		return bundle.Reproduce[i].Target < bundle.Reproduce[j].Target
	})
	sort.Strings(bundle.Bounds.TruncatedLayers)
	sort.Strings(bundle.Validation.Checks)
}

func sortPacketSummaries(packets []PacketSummary) {
	for i := range packets {
		sort.Strings(packets[i].EvidenceHandles)
		sort.Strings(packets[i].NextCalls)
	}
	sort.Slice(packets, func(i, j int) bool {
		if packets[i].Family != packets[j].Family {
			return packets[i].Family < packets[j].Family
		}
		if packets[i].Schema != packets[j].Schema {
			return packets[i].Schema < packets[j].Schema
		}
		if packets[i].TruthClass != packets[j].TruthClass {
			return packets[i].TruthClass < packets[j].TruthClass
		}
		if packets[i].Summary != packets[j].Summary {
			return packets[i].Summary < packets[j].Summary
		}
		if evidenceHandles := strings.Compare(strings.Join(packets[i].EvidenceHandles, "\x00"), strings.Join(packets[j].EvidenceHandles, "\x00")); evidenceHandles != 0 {
			return evidenceHandles < 0
		}
		return strings.Join(packets[i].NextCalls, "\x00") < strings.Join(packets[j].NextCalls, "\x00")
	})
}

func bundleID(bundle Bundle) string {
	bundle.BundleID = ""
	raw, _ := json.Marshal(bundle)
	sum := sha256.Sum256(raw)
	return "evidence-bundle:" + hex.EncodeToString(sum[:16])
}
