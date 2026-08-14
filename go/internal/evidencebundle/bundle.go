// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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

// The IPv4 fragments the private-address rules are built from. They are
// constants rather than repeated literals because the first version of these
// rules spelled an octet as "\d{1,3}" with nothing after it, and that is two
// bugs in one: "\d{1,3}" accepts 300, and with no boundary after the last octet
// it matches the first eight characters of "version:10.0.5.300" and rejects a
// share-safe bundle over a version string. An address is four octets and then
// something that is not another digit.
const (
	// ipv4Octet is one decimal octet, 0-255, and nothing else.
	ipv4Octet = `(?:25[0-5]|2[0-4][0-9]|1[0-9]{2}|[1-9]?[0-9])`
	// ipv4End is the boundary after the final octet. It excludes a digit but
	// deliberately allows a dot, because a sentence ends "unreachable at
	// 10.0.5.3." and that address is still an address.
	ipv4End = `(?:[^0-9]|$)`
	// ipv4Private is the RFC1918 and link-local space, without the boundary, for
	// rules that already have one.
	ipv4Private = `10\.` + ipv4Octet + `\.` + ipv4Octet + `\.` + ipv4Octet +
		`|192\.168\.` + ipv4Octet + `\.` + ipv4Octet +
		`|172\.(?:1[6-9]|2[0-9]|3[0-1])\.` + ipv4Octet + `\.` + ipv4Octet
)

var (
	privateEndpointPattern = regexp.MustCompile(`(?i)https?://[^/"\s]*(internal|localhost|127\.0\.0\.1|169\.254\.|\.cluster\.local|10\.|192\.168\.|172\.(1[6-9]|2[0-9]|3[0-1])\.)`)
	// Go network errors report a bare "host:port" with no scheme, so the
	// scheme-anchored pattern above misses them entirely. Requiring the port
	// keeps this from firing on ordinary dotted text such as a version string.
	//
	// The left boundary rules out a host spelled as the tail of a longer token
	// ("notlocalhost:80"), so it excludes the characters that can continue a
	// hostname: letters, digits, dot, hyphen. A colon cannot appear in a
	// hostname label, and excluding it let every colon-delimited shape through
	// -- a labelled diagnostic ("upstream:db.internal:5432"), a scope handle
	// ("repo:db.internal"), and an IPv4-mapped IPv6 address
	// ("::ffff:10.0.5.3"). The pre-existing cases all read "dial tcp <host>",
	// so the character before the host was always a space and the gap never
	// showed.
	// The quads here need no trailing boundary of their own: the required
	// ":port" is one, which is why "version:10.0.5.300:5432" stops matching once
	// the octets are range-checked.
	privateHostPortPattern = regexp.MustCompile(`(?i)(^|[^0-9A-Za-z.-])(localhost|127\.0\.0\.1|` + ipv4Private +
		`|[A-Za-z0-9.-]*internal[A-Za-z0-9.-]*|[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)*\.cluster\.local` +
		`|\[(?:::1|fc[0-9a-f]{2}:[0-9a-fA-F:]*|fd[0-9a-f]{2}:[0-9a-fA-F:]*|fe[89ab][0-9a-f]:[0-9a-fA-F:]*)\]):\d{2,5}`)
	// Addresses that locate a stack even without a port. The host:port rule
	// above cannot cover these: a collector reason reads "instance 10.0.5.3 is
	// unreachable" with no port at all, and a Kubernetes service name is a
	// locating hostname in its own right. Kept separate so the port-bearing rule
	// stays narrow enough not to fire on ordinary dotted text. Same left
	// boundary as above, and for the same reason.
	//
	// This is the rule that has no port to bound it on the right, so each quad
	// carries ipv4End. Without both halves -- the octet range AND that boundary
	// -- an IP-shaped version string such as "backend:nornicdb version:10.0.5.300"
	// matches on its first eight characters and the whole bundle is refused.
	privateAddressPattern = regexp.MustCompile(`(?i)(^|[^0-9A-Za-z.-])(` +
		`10\.` + ipv4Octet + `\.` + ipv4Octet + `\.` + ipv4Octet + ipv4End +
		`|192\.168\.` + ipv4Octet + `\.` + ipv4Octet + ipv4End +
		`|172\.(?:1[6-9]|2[0-9]|3[0-1])\.` + ipv4Octet + `\.` + ipv4Octet + ipv4End +
		`|169\.254\.` + ipv4Octet + `\.` + ipv4Octet + ipv4End +
		`|[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)*\.cluster\.local)`)
	// Unique-local IPv6, split out of the rule above because it is the one
	// alternative whose left boundary must keep excluding the colon. A hextet
	// is separated by colons, so allowing one before "fd12" would reject the
	// public address 2001:db8:fd12::1 on the strength of a middle hextet.
	// Bracketed unique-local addresses with a port are covered by
	// privateHostPortPattern instead.
	privateULAv6Pattern = regexp.MustCompile(`(?i)(^|[^0-9A-Za-z.:-])f[cd][0-9a-f]{2}:[0-9a-f:]*[0-9a-f]`)
	// Credential-bearing URL, by shape rather than by known value. The shared
	// registry only matches its own synthetic canaries, so a real secret an
	// operator's status text happens to carry passed straight through. Requires
	// userinfo with a password before the host, so an ordinary URL — including
	// one with a port such as https://host:443/x — does not match. The user and
	// password classes also exclude ? and #, which keeps a pathless URL whose
	// query happens to contain a colon and an @ from being reported as a
	// credential.
	credentialURLPattern = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9+.-]*://[^/\s:@"?#]+:[^/\s@"?#]+@`)
	// Credential keywords only count when something is actually being assigned
	// to them, and the discriminator is the value, not the keyword.
	//
	// Matching the bare word rejected honest content: "secrets_iam_trust_chain"
	// and "secrets_iam_graph_projection" are real materialization domains and
	// reach domain_backlogs. Other real identifiers end in the keyword --
	// "appflow_connector_profile_uses_secret" is an AWS relationship type and
	// "aws_appsync_api_key" a resource type, both of which can appear in free
	// text -- so a reason reading "<identifier>: 5 blocked" looks exactly like
	// an assignment unless the value is examined.
	//
	//   - As a JSON key ("password":, \"api_key\":) the keyword is quoted on
	//     both sides, which a domain appearing as a JSON value never is. Any
	//     value counts here, including a purely numeric one.
	//   - Anywhere else, with or without a suffix (SECRET_KEY=, api_key_id=,
	//     secret: ), the assigned value must contain something other than
	//     digits. That is what separates a secret from a count.
	//
	// Known gap, accepted: a plain-text "password=123456" outside JSON is not
	// matched. Screening is best-effort by design, and rejecting every real
	// export from a secrets/IAM stack is the worse failure.
	credentialPattern = regexp.MustCompile(`(?i)(authorization:\s*(bearer|basic)|\\?"(api[_-]?key|password|passwd|secret|token)\\?"\s*:|(api[_-]?key|password|passwd|secret|token)[a-z0-9_-]*\\?"?\s*[:=]\s*\\?"?[^\s",]*[A-Za-z/+_-]|gh[pousr]_[A-Za-z0-9_]{8,}|-----BEGIN [A-Z ]*PRIVATE KEY-----)`)
	rawPromptPattern  = regexp.MustCompile(`(?i)(raw_prompt|provider_response|raw provider response|prompt transcript)`)
	// Filesystem roots, not "any absolute path": this bundle's own reproduce
	// calls carry bare API routes such as "GET /api/v0/status/index", which a
	// general absolute-path rule would reject as a local path. Selectivity comes
	// from the root list, so the preceding character only has to rule out a root
	// spelled mid-word ("example.com/usr/x"); anything else may precede it,
	// because real diagnostics write "cwd:/Users/...", "config_path=/etc/...",
	// and "file:///etc/...", none of which follow a quote or a space.
	localPathPattern = regexp.MustCompile(`(?i)(^|[^A-Za-z0-9._~-])(/Users/|/home/|/root/|/etc/|/usr/|/workspace/|/workspaces/|/tmp/|/private/|/var/|/opt/|/srv/|/mnt/|/media/|/snap/|/data/|/Volumes/|/Library/|~/|[A-Za-z]:\\)`)
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
				"screened_private_endpoints",
				"screened_credentials",
				"screened_model_inputs_or_outputs",
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
			// Built unvalidated on purpose: a builder that stamps "passed"
			// certifies a check it never ran. StampValidation applies "passed"
			// after Validate returns nil.
			Status: unvalidatedStatus,
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
	sortPipelineState(bundle.Contents.PipelineState)
	sortSemanticProviderState(bundle.Contents.SemanticProviderState)
}

func sortPipelineState(state *PipelineStateSnapshot) {
	if state == nil {
		return
	}
	sort.Strings(state.HealthReasons)
	sort.Slice(state.StageSummaries, func(i, j int) bool {
		return state.StageSummaries[i].Stage < state.StageSummaries[j].Stage
	})
	sort.Slice(state.DomainBacklogs, func(i, j int) bool {
		return state.DomainBacklogs[i].Domain < state.DomainBacklogs[j].Domain
	})
	sort.Slice(state.Collectors, func(i, j int) bool {
		if state.Collectors[i].CollectorKind != state.Collectors[j].CollectorKind {
			return state.Collectors[i].CollectorKind < state.Collectors[j].CollectorKind
		}
		if state.Collectors[i].StatusCategory != state.Collectors[j].StatusCategory {
			return state.Collectors[i].StatusCategory < state.Collectors[j].StatusCategory
		}
		// sort.Slice is not stable, so every field needs to participate in the
		// key or two rows differing only by Health could swap between runs and
		// change bundle_id.
		return state.Collectors[i].Health < state.Collectors[j].Health
	})
}

func sortSemanticProviderState(state *SemanticProviderStateSnapshot) {
	if state == nil {
		return
	}
	sort.Slice(state.ProviderProfiles, func(i, j int) bool {
		return state.ProviderProfiles[i].ProfileID < state.ProviderProfiles[j].ProfileID
	})
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
