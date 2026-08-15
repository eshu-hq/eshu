// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package investigation_test

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cli/investigation"
	"github.com/eshu-hq/eshu/go/internal/query"
)

// The packet contract advertises a share_safe_v2 redaction profile whose rules
// include no_transport_metadata. These tests check the two halves of that claim
// this package is actually responsible for:
//
//   - Server- and transport-supplied text (an envelope error message, a
//     transport error string) must never reach the rendered artifact. Only the
//     family, the scope the operator named, and the refusal state do.
//   - The scope the operator named DOES reach the artifact, by design. A packet
//     that refused has to say what it refused about.
//
// The sentinels are alphanumeric so neither HTML escaping nor JSON escaping can
// hide one: renderInvestigationPacketHTML escapes its markdown, and a value
// containing a quote or an angle bracket would otherwise be rewritten around
// the sentinel rather than removing it.
const (
	leakCanary    = "CANARY6059LEAKSENTINEL"
	presentCanary = "CANARY6059SCOPESENTINEL"
)

// carrierPrefixes vary the character immediately before the sentinel. A screen
// anchored on a word or segment boundary passes for one of these and fails for
// another, so a single placement proves nothing.
var carrierPrefixes = []struct{ name, prefix string }{
	{"segment start", ""},
	{"letter", "a"},
	{"space", " "},
	{"at", "@"},
	{"quote", `"`},
	{"colon", ":"},
	{"dot", "."},
	{"dash", "-"},
	{"slash", "/"},
}

var packetFormats = []query.InvestigationPacketFormat{
	query.InvestigationPacketFormatJSON,
	query.InvestigationPacketFormatMarkdown,
	query.InvestigationPacketFormatHTML,
}

// renderAndWrite runs the same path the CLI runs: render the packet, then write
// it through WriteArtifact to a real file, and return the bytes that landed on
// disk. Checking the file rather than the in-memory render catches a leak added
// by the writer itself.
func renderAndWrite(t *testing.T, packet query.InvestigationEvidencePacket, format query.InvestigationPacketFormat) string {
	t.Helper()

	data, err := query.RenderInvestigationPacket(packet, format)
	if err != nil {
		t.Fatalf("render %s: %v", format, err)
	}
	path := filepath.Join(t.TempDir(), "packet.out")
	var stdout, stderr strings.Builder
	if err := investigation.WriteArtifact(&stdout, &stderr, path, data); err != nil {
		t.Fatalf("write %s: %v", format, err)
	}
	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back %s: %v", format, err)
	}
	if string(onDisk) != string(data) {
		t.Fatalf("file bytes differ from the rendered bytes for %s", format)
	}
	return string(onDisk) + stdout.String()
}

// TestArtifactDropsServerAndTransportText is the absence half. The canary rides
// in an envelope error message and in a transport error string, across every
// family, every refusal path, and every format.
func TestArtifactDropsServerAndTransportText(t *testing.T) {
	t.Parallel()

	cases := 0
	for _, prefix := range carrierPrefixes {
		carrier := prefix.prefix + leakCanary

		for _, family := range []query.InvestigationFamily{
			query.InvestigationFamilySupplyChainImpact,
			query.InvestigationFamilyDeployableUnit,
			query.InvestigationFamilyDrift,
		} {
			for _, source := range []struct {
				name string
				deps investigation.Deps
			}{
				{
					name: "envelope error message",
					deps: depsReturningEnvelopeError(&query.ErrorEnvelope{
						Code: query.ErrorCodeNotFound, Message: "no match for " + carrier,
					}),
				},
				{
					name: "transport error text",
					deps: depsReturningFetchError(fmt.Errorf(
						"request failed: Get %q: connection refused: %w", "http://"+carrier+"/api", &statusError{code: 503})),
				},
			} {
				name := fmt.Sprintf("%s/%s/%s", prefix.name, family, source.name)
				packet, err := investigation.BuildPacket(nil, source.deps, investigation.Request{
					Family:  family,
					Subject: scopeFor(family),
				})
				if err != nil {
					t.Fatalf("%s: BuildPacket: %v", name, err)
				}
				for _, format := range packetFormats {
					cases++
					if out := renderAndWrite(t, packet, format); strings.Contains(out, leakCanary) {
						t.Errorf("%s/%s: artifact carries server/transport text:\n%s", name, format, out)
					}
				}
			}
		}
	}
	// A screen that examined nothing also reports clean. 9 prefixes x 3
	// families x 2 sources x 3 formats.
	if want := len(carrierPrefixes) * 3 * 2 * 3; cases != want {
		t.Fatalf("checked %d renderings, want %d", cases, want)
	}
}

// TestArtifactKeepsTheOperatorScope is the positive control, and it uses the
// same scanner as the absence test above. If this goes green while the absence
// test also goes green, the scanner can find a sentinel that is really there.
//
// It also pins a real limit: a secret an operator puts in a --subject value
// lands in the artifact verbatim. That is deliberate -- a refusal packet has to
// name the scope it refused about -- but it means --subject is not a safe place
// for a credential.
func TestArtifactKeepsTheOperatorScope(t *testing.T) {
	t.Parallel()

	cases := 0
	for _, prefix := range carrierPrefixes {
		carrier := prefix.prefix + presentCanary
		packet, err := investigation.BuildPacket(nil, investigation.Deps{}, investigation.Request{
			Family:  query.InvestigationFamily("unknown_family_for_control"),
			Subject: map[string]string{"advisory_id": carrier},
		})
		if err != nil {
			t.Fatalf("%s: BuildPacket: %v", prefix.name, err)
		}
		for _, format := range packetFormats {
			cases++
			if out := renderAndWrite(t, packet, format); !strings.Contains(out, presentCanary) {
				t.Errorf("%s/%s: control sentinel missing; the scanner cannot see this carrier:\n%s",
					prefix.name, format, out)
			}
		}
	}
	if want := len(carrierPrefixes) * 3; cases != want {
		t.Fatalf("checked %d renderings, want %d", cases, want)
	}
}

// TestSubjectValuesReachTheRequestURL pins the other place an operator-supplied
// value travels: the query string of the API request, which net/http then
// quotes back inside a transport error. A password in --service-url does not
// survive that far (net/http replaces the userinfo password with ***), but a
// secret in a --subject value does.
func TestSubjectValuesReachTheRequestURL(t *testing.T) {
	t.Parallel()

	client := &recordClient{}
	if _, err := investigation.DefaultDeps().FetchSupplyChainExplain(client,
		investigation.SupplyChainFilterFromSubject(map[string]string{"advisory_id": presentCanary})); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if !strings.Contains(client.path, url.QueryEscape(presentCanary)) {
		t.Fatalf("path = %q, want the subject value in the query string", client.path)
	}
}

func scopeFor(family query.InvestigationFamily) map[string]string {
	switch family {
	case query.InvestigationFamilyDeployableUnit:
		return map[string]string{"scope_id": "s1", "generation_id": "g1"}
	case query.InvestigationFamilyDrift:
		return map[string]string{"scope_id": "acct1"}
	case query.InvestigationFamilySupplyChainImpact:
		return map[string]string{"advisory_id": "GHSA-x", "package_id": "pkg:npm/y"}
	default:
		return map[string]string{"advisory_id": "GHSA-x", "package_id": "pkg:npm/y"}
	}
}

func depsReturningEnvelopeError(errEnv *query.ErrorEnvelope) investigation.Deps {
	return investigation.Deps{
		FetchSupplyChainExplain: func(investigation.Client, query.SupplyChainImpactExplanationFilter) (investigation.SupplyChainExplainEnvelope, error) {
			return investigation.SupplyChainExplainEnvelope{Error: errEnv}, nil
		},
		FetchAdmissionDecisions: func(investigation.Client, url.Values) (investigation.AdmissionDecisionsEnvelope, error) {
			return investigation.AdmissionDecisionsEnvelope{Error: errEnv}, nil
		},
		FetchDriftFindings: func(investigation.Client, map[string]any) (investigation.DriftFindingsEnvelope, error) {
			return investigation.DriftFindingsEnvelope{Error: errEnv}, nil
		},
	}
}

func depsReturningFetchError(err error) investigation.Deps {
	return investigation.Deps{
		FetchSupplyChainExplain: func(investigation.Client, query.SupplyChainImpactExplanationFilter) (investigation.SupplyChainExplainEnvelope, error) {
			return investigation.SupplyChainExplainEnvelope{}, err
		},
		FetchAdmissionDecisions: func(investigation.Client, url.Values) (investigation.AdmissionDecisionsEnvelope, error) {
			return investigation.AdmissionDecisionsEnvelope{}, err
		},
		FetchDriftFindings: func(investigation.Client, map[string]any) (investigation.DriftFindingsEnvelope, error) {
			return investigation.DriftFindingsEnvelope{}, err
		},
	}
}
