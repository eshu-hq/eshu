// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package support

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/answerquality"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/packet"
	"github.com/eshu-hq/eshu/go/internal/reportbundle"
	"github.com/eshu-hq/eshu/go/internal/serviceintel"
)

// CLISupportReadAdmissionContractVersion versions the #9669 W5
// admission-contract shape for the CLI-support derives (packet render,
// service-report compose, bundle capture, bundle validate, and
// answer-quality score). Canonical reads persist through explicit
// core-owned admission contracts: eshu-api keeps the HTTP shape and
// calls this versioned port, which the core-owned static derive
// satisfies. A breaking change to the derive shape fails the frozen
// method-set test until that list is deliberately updated; bump this
// version alongside (manually — the bump is convention, the frozen
// list is the enforcement) so dest consumers can pin the shape.
const CLISupportReadAdmissionContractVersion = "v0"

// CLISupportReadAdmission is the versioned admission-contract port
// the API handler will depend on instead of calling the static derive
// functions directly. It re-declares exactly the ten derives the five
// CLI-support routes admit through — packet format parse and render,
// service-story and supply-chain adapters plus report compose, bundle
// target screening plus capture plus validate, and answer-evidence
// parse plus score — so the boundary is held honest by the
// compile-time adapter assertion below plus the frozen method-set
// contract test. It will back the eshu-api extraction handoff for
// these reads. The seam is read-only on purpose: every declared
// method is a bounded pure derivation over caller-supplied bytes and
// the port carries no mutation surface, so a dest consumer
// implementing only this contract can serve the CLI-support derives
// but cannot acquire a write it did not bargain for through it. This
// pin lives in package support because the handlers live outside
// internal/query; the ruling's no-new-tree rule is met — the pin sits
// beside the handlers it covers, the same placement the query-tree
// pins use.
//
// W-wave consumer: W5 CLI support (all 5 oracle CLISupport rows).
//
// Narrow by construction, recorded not silently assumed: the methods
// re-declare the staying static signatures whole in this
// CLI-support-scoped shape instead of aliasing a wider derive
// surface, so no other family can cite this pin — a blanket pin is
// forbidden, and the widened shape needs arbiter review before
// satellite consumption.
//
// Explicitly OUT, recorded not silently assumed (each keeps its own
// standing until a deliberate pin covers it):
//
//   - The body caps, strict-JSON decode, required-field checks, and
//     the 400/413/422 mappings are handler behavior, not port shape:
//     there is no method set to freeze for them.
//   - The credential-screening predicate lives inside the capture
//     derive (CheckTargetCredentials), not as a separate gate: the
//     pin freezes the derive that enforces it.
//
// Rollback: revert this pin. The seam is unwired — no handler, route,
// or caller depends on it yet — so rollback moves no traffic and
// changes no data.
type CLISupportReadAdmission interface {
	RenderInvestigationPacket(packet.InvestigationEvidencePacket, packet.InvestigationPacketFormat) ([]byte, error)
	ParseInvestigationPacketFormat(string) (packet.InvestigationPacketFormat, error)
	ServiceStoryInput(map[string]any, *querycontract.TruthEnvelope) serviceintel.ReportInput
	SupplyChainSectionInput(map[string]any, serviceintel.ReportSubject, *querycontract.TruthEnvelope) serviceintel.SectionInput
	ComposeServiceIntelReport(serviceintel.ReportInput) serviceintel.Report
	CheckReportTargetCredentials(string) error
	CaptureReportBundle(reportbundle.CaptureInput) (reportbundle.Bundle, error)
	ValidateReportBundle(reportbundle.Bundle, reportbundle.ValidateOptions) error
	ParseAnswerEvidence([]byte) (answerquality.Evidence, error)
	ScoreAnswerEvidence(answerquality.Evidence) answerquality.Verdict
}

// cliSupportStaticDeriver is the minimal static satisfier behind the
// pin: it delegates each re-declared derive to the static function
// the handler legs call today. The delegation keeps the pin honest in
// one direction: removal, rename, or re-signing of any of the ten
// functions fails compilation here. A new derive in those packages
// does not widen this pin — adoption takes deliberate re-declaration
// plus a frozen-list update. It pins no shared port — the delegation
// names only the ten functions this pin mirrors, and no other family
// may cite this pin as coverage.
type cliSupportStaticDeriver struct{}

func (cliSupportStaticDeriver) RenderInvestigationPacket(p packet.InvestigationEvidencePacket, f packet.InvestigationPacketFormat) ([]byte, error) {
	rendered, err := packet.RenderInvestigationPacket(p, f)
	if err != nil {
		return nil, fmt.Errorf("cli support packet render: %w", err)
	}
	return rendered, nil
}

func (cliSupportStaticDeriver) ParseInvestigationPacketFormat(raw string) (packet.InvestigationPacketFormat, error) {
	format, err := packet.ParseInvestigationPacketFormat(raw)
	if err != nil {
		return format, fmt.Errorf("cli support packet format parse: %w", err)
	}
	return format, nil
}

func (cliSupportStaticDeriver) ServiceStoryInput(dossier map[string]any, truth *querycontract.TruthEnvelope) serviceintel.ReportInput {
	return serviceintel.FromServiceStory(dossier, truth)
}

func (cliSupportStaticDeriver) SupplyChainSectionInput(inventory map[string]any, subject serviceintel.ReportSubject, truth *querycontract.TruthEnvelope) serviceintel.SectionInput {
	return serviceintel.FromSupplyChainInventory(inventory, subject, truth)
}

func (cliSupportStaticDeriver) ComposeServiceIntelReport(in serviceintel.ReportInput) serviceintel.Report {
	return serviceintel.Compose(in)
}

func (cliSupportStaticDeriver) CheckReportTargetCredentials(target string) error {
	if err := reportbundle.CheckTargetCredentials(target); err != nil {
		return fmt.Errorf("cli support target screening: %w", err)
	}
	return nil
}

func (cliSupportStaticDeriver) CaptureReportBundle(in reportbundle.CaptureInput) (reportbundle.Bundle, error) {
	bundle, err := reportbundle.Capture(in)
	if err != nil {
		return bundle, fmt.Errorf("cli support bundle capture: %w", err)
	}
	return bundle, nil
}

func (cliSupportStaticDeriver) ValidateReportBundle(bundle reportbundle.Bundle, opts reportbundle.ValidateOptions) error {
	if err := reportbundle.Validate(bundle, opts); err != nil {
		return fmt.Errorf("cli support bundle validate: %w", err)
	}
	return nil
}

func (cliSupportStaticDeriver) ParseAnswerEvidence(raw []byte) (answerquality.Evidence, error) {
	evidence, err := answerquality.ParseEvidence(raw)
	if err != nil {
		return evidence, fmt.Errorf("cli support answer evidence parse: %w", err)
	}
	return evidence, nil
}

func (cliSupportStaticDeriver) ScoreAnswerEvidence(evidence answerquality.Evidence) answerquality.Verdict {
	return answerquality.Score(evidence)
}

var _ CLISupportReadAdmission = cliSupportStaticDeriver{}
