// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package support

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/answerquality"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/packet"
	"github.com/eshu-hq/eshu/go/internal/reportbundle"
	"github.com/eshu-hq/eshu/go/internal/serviceintel"
)

// frozenCLISupportReadAdmission pins the exact method set behind the
// versioned #9669 W5 admission contract for the CLI-support derives
// (POST /api/v0/answer-quality/score,
// /api/v0/investigation/packet/render, /api/v0/report/bundle/capture,
// /api/v0/report/bundle/validate, /api/v0/service/report/compose):
// the ten static derives the five routes admit through. The entries
// re-declare the staying static signatures whole in this
// CLI-support-scoped shape instead of aliasing a wider derive
// surface, so no other family can cite this pin — a blanket pin is
// forbidden, and the widened shape needs arbiter review before
// satellite consumption. A renamed, added, removed, or re-signed
// method fails here until that list is deliberately updated; bump
// CLISupportReadAdmissionContractVersion alongside (manually — the
// bump is convention, the frozen list is the enforcement) so dest
// consumers can pin the shape, keeping the eshu-api extraction
// boundary from drifting silently.
//
// W-wave consumer: W5 CLI support (all 5 oracle CLISupport rows).
var frozenCLISupportReadAdmission = []string{
	"CaptureReportBundle func(reportbundle.CaptureInput) (reportbundle.Bundle, error)",
	"CheckReportTargetCredentials func(string) error",
	"ComposeServiceIntelReport func(serviceintel.ReportInput) serviceintel.Report",
	"ParseAnswerEvidence func([]uint8) (answerquality.Evidence, error)",
	"ParseInvestigationPacketFormat func(string) (packet.InvestigationPacketFormat, error)",
	"RenderInvestigationPacket func(packet.InvestigationEvidencePacket, packet.InvestigationPacketFormat) ([]uint8, error)",
	"ScoreAnswerEvidence func(answerquality.Evidence) answerquality.Verdict",
	"ServiceStoryInput func(map[string]interface {}, *querycontract.TruthEnvelope) serviceintel.ReportInput",
	"SupplyChainSectionInput func(map[string]interface {}, serviceintel.ReportSubject, *querycontract.TruthEnvelope) serviceintel.SectionInput",
	"ValidateReportBundle func(reportbundle.Bundle, reportbundle.ValidateOptions) error",
}

// cliSupportReadAdmissionWitness proves CLISupportReadAdmission is
// satisfiable with the minimal shape: the ten re-declared derives, no
// wider derive surface.
type cliSupportReadAdmissionWitness struct{}

func (cliSupportReadAdmissionWitness) RenderInvestigationPacket(packet.InvestigationEvidencePacket, packet.InvestigationPacketFormat) ([]byte, error) {
	return nil, nil
}

func (cliSupportReadAdmissionWitness) ParseInvestigationPacketFormat(string) (packet.InvestigationPacketFormat, error) {
	return packet.InvestigationPacketFormat(""), nil
}

func (cliSupportReadAdmissionWitness) ServiceStoryInput(map[string]any, *querycontract.TruthEnvelope) serviceintel.ReportInput {
	return serviceintel.ReportInput{}
}

func (cliSupportReadAdmissionWitness) SupplyChainSectionInput(map[string]any, serviceintel.ReportSubject, *querycontract.TruthEnvelope) serviceintel.SectionInput {
	return serviceintel.SectionInput{}
}

func (cliSupportReadAdmissionWitness) ComposeServiceIntelReport(serviceintel.ReportInput) serviceintel.Report {
	return serviceintel.Report{}
}

func (cliSupportReadAdmissionWitness) CheckReportTargetCredentials(string) error {
	return nil
}

func (cliSupportReadAdmissionWitness) CaptureReportBundle(reportbundle.CaptureInput) (reportbundle.Bundle, error) {
	return reportbundle.Bundle{}, nil
}

func (cliSupportReadAdmissionWitness) ValidateReportBundle(reportbundle.Bundle, reportbundle.ValidateOptions) error {
	return nil
}

func (cliSupportReadAdmissionWitness) ParseAnswerEvidence([]byte) (answerquality.Evidence, error) {
	return answerquality.Evidence{}, nil
}

func (cliSupportReadAdmissionWitness) ScoreAnswerEvidence(answerquality.Evidence) answerquality.Verdict {
	return answerquality.Verdict{}
}

var _ CLISupportReadAdmission = cliSupportReadAdmissionWitness{}

// cliSupportReadAdmissionMethodSet reflects the live
// CLISupportReadAdmission method set into sorted "name signature"
// strings for comparison with the frozen list. The iface parameter
// forces every caller — including the seeded-drift RED proof below —
// through this same guard comparison, so a passing suite proves the
// guard itself can see drift rather than proving a private copy can.
func cliSupportReadAdmissionMethodSet(t *testing.T, iface any) []string {
	t.Helper()
	rt := reflect.TypeOf(iface).Elem()
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	return got
}

// TestCLISupportReadAdmissionContractIsVersioned pins the #9669 W5
// admission-contract version for the CLI-support derives. The frozen
// method set below is the enforcement: an added, removed, renamed, or
// re-signed method fails this test until the frozen list is
// deliberately updated. Bump CLISupportReadAdmissionContractVersion
// alongside (manually — the bump is convention, the frozen list is
// the enforcement) so dest consumers can pin the shape.
func TestCLISupportReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if CLISupportReadAdmissionContractVersion != "v0" {
		t.Errorf("CLISupportReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", CLISupportReadAdmissionContractVersion, "v0")
	}
	got := cliSupportReadAdmissionMethodSet(t, (*CLISupportReadAdmission)(nil))
	if len(got) != len(frozenCLISupportReadAdmission) {
		t.Fatalf("CLISupportReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenCLISupportReadAdmission, len(frozenCLISupportReadAdmission), CLISupportReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenCLISupportReadAdmission[i] {
			t.Errorf("CLISupportReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenCLISupportReadAdmission[i], CLISupportReadAdmissionContractVersion)
		}
	}
}

// TestCLISupportReadAdmissionWireShapesAreSealed proves the seal on
// the pin: exactly the ten re-declared derives, with no offline
// command, executor, or ledger surface riding along. The frozen
// method-set contract test pins the exact names; this test pins the
// exclusion the arbiter ruling demands.
func TestCLISupportReadAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*CLISupportReadAdmission)(nil)).Elem()
	if rt.NumMethod() != 10 {
		t.Fatalf("CLISupportReadAdmission has %d methods, want exactly 10: the seal forbids any wider surface", rt.NumMethod())
	}
	for i := range rt.NumMethod() {
		name := rt.Method(i).Name
		for _, banned := range []string{
			"Execute", "Command", "Delete", "Write", "Snapshot", "Ledger", "Publish",
		} {
			if strings.Contains(name, banned) {
				t.Errorf("CLISupportReadAdmission.%s contains %q: that leg stays out of this pin", name, banned)
			}
		}
	}
}

// seededCLISupportDrift is a deliberately widened copy of the pin:
// the ten frozen derives plus a report-publish derive the
// CLI-support lane never issues. It never ships; it proves the
// frozen-set comparison below can see a one-method widening.
type seededCLISupportDrift interface {
	CLISupportReadAdmission
	PublishServiceReport(serviceintel.Report) error
}

// TestCLISupportReadAdmissionSeededDriftIsDetected is the RED half of
// the seeded-drift pair: the same reflect comparison the contract
// test runs must report a mismatch on the planted widening. It passes
// on a clean tree precisely because the guard fires on drift — a
// guard that cannot see the seeded method is no guard.
func TestCLISupportReadAdmissionSeededDriftIsDetected(t *testing.T) {
	t.Parallel()

	got := cliSupportReadAdmissionMethodSet(t, (*seededCLISupportDrift)(nil))
	if reflect.DeepEqual(got, frozenCLISupportReadAdmission) {
		t.Fatal("seeded widening (PublishServiceReport) matches the frozen set: the comparison cannot see drift")
	}
	if len(got) != len(frozenCLISupportReadAdmission)+1 {
		t.Fatalf("seeded drift method set len = %d, want frozen len + 1 (%d): the seed is malformed", len(got), len(frozenCLISupportReadAdmission)+1)
	}
}
