package main

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// supplyChainExplainEnvelope decodes the canonical envelope returned by the
// supply-chain impact explain route.
type supplyChainExplainEnvelope struct {
	Data  query.SupplyChainImpactExplanationResult `json:"data"`
	Truth *query.TruthEnvelope                     `json:"truth"`
	Error *query.ErrorEnvelope                     `json:"error"`
}

// investigationExportDeps lets tests inject the explain fetch without a live API.
type investigationExportDeps struct {
	FetchSupplyChainExplain func(client *APIClient, filter query.SupplyChainImpactExplanationFilter) (supplyChainExplainEnvelope, error)
}

func defaultInvestigationExportDeps() investigationExportDeps {
	return investigationExportDeps{FetchSupplyChainExplain: fetchSupplyChainExplain}
}

func init() {
	rootCmd.AddCommand(newInvestigationCommand())
}

// newInvestigationCommand groups the portable investigation evidence packet
// subcommands.
func newInvestigationCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "investigation",
		Short: "Emit portable, source-backed investigation evidence packets",
	}
	cmd.AddCommand(newInvestigationExportCommand())
	return cmd
}

// newInvestigationExportCommand builds the `eshu investigation export` command,
// which emits an investigation_evidence_packet.v2 artifact for a supported
// investigation family.
func newInvestigationExportCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export an investigation evidence packet (json, md, or html)",
		Long: `export emits a portable investigation_evidence_packet.v2 artifact for one
investigation. The packet separates raw source facts, reducer decisions,
graph/query truth, missing-evidence reasons, freshness, and optional semantic
observations. It is deterministic with no provider keys and bounded with explicit
truncation. An unknown family or unanswerable scope yields a valid refusal packet
rather than a fabricated answer.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          runInvestigationExport,
	}
	cmd.Flags().String("family", "", "Investigation family: supply_chain_impact")
	cmd.Flags().StringArray("subject", nil, "Scope key=value (repeatable), e.g. --subject advisory_id=GHSA-...")
	cmd.Flags().String("format", "json", "Artifact format: json, md, or html")
	cmd.Flags().String("out", "", "Write the artifact to this path instead of stdout")
	cmd.Flags().Int("max-source-facts", 0, "Override the source-facts cap (0 = contract default)")
	return cmd
}

func runInvestigationExport(cmd *cobra.Command, _ []string) error {
	rawFamily, _ := cmd.Flags().GetString("family")
	rawSubjects, _ := cmd.Flags().GetStringArray("subject")
	rawFormat, _ := cmd.Flags().GetString("format")
	out, _ := cmd.Flags().GetString("out")

	format, err := query.ParseInvestigationPacketFormat(rawFormat)
	if err != nil {
		return err
	}
	subject, err := parseSubjectFlags(rawSubjects)
	if err != nil {
		return err
	}
	family := query.InvestigationFamily(strings.TrimSpace(rawFamily))

	packet, err := buildInvestigationExportPacket(cmd, family, subject)
	if err != nil {
		return err
	}
	data, err := query.RenderInvestigationPacket(packet, format)
	if err != nil {
		return err
	}
	return writeInvestigationArtifact(cmd, out, data)
}

// buildInvestigationExportPacket dispatches by family. An unknown family yields a
// refusal packet (the contract treats it as a valid, share-safe artifact). A
// known-but-unwired family is a clear CLI error so the operator is never misled
// into thinking the artifact is empty truth.
func buildInvestigationExportPacket(cmd *cobra.Command, family query.InvestigationFamily, subject map[string]string) (query.InvestigationEvidencePacket, error) {
	if !query.ValidInvestigationFamily(family) {
		return query.NewInvestigationEvidencePacket(query.InvestigationPacketInput{
			Family:  family,
			Subject: subjectOrPlaceholder(subject),
			Refusal: query.PacketRefusalUnknownFamily,
		})
	}
	switch family {
	case query.InvestigationFamilySupplyChainImpact:
		return buildSupplyChainExportPacket(cmd, subject)
	default:
		return query.InvestigationEvidencePacket{}, fmt.Errorf(
			"investigation family %q is recognized but not yet available in this CLI build", family)
	}
}

func buildSupplyChainExportPacket(cmd *cobra.Command, subject map[string]string) (query.InvestigationEvidencePacket, error) {
	filter := supplyChainFilterFromSubject(subject)
	if !supplyChainFilterHasScope(filter) {
		return query.NewInvestigationEvidencePacket(query.InvestigationPacketInput{
			Family:  query.InvestigationFamilySupplyChainImpact,
			Subject: subjectOrPlaceholder(subject),
			Refusal: query.PacketRefusalScopeNotFound,
		})
	}
	client := apiClientFromCmd(cmd)
	envelope, err := investigationExportDepsValue.FetchSupplyChainExplain(client, filter)
	if err != nil {
		if refusal, ok := refusalFromFetchError(err); ok {
			return query.NewInvestigationEvidencePacket(query.InvestigationPacketInput{
				Family:  query.InvestigationFamilySupplyChainImpact,
				Subject: subjectOrPlaceholder(subject),
				Refusal: refusal,
			})
		}
		return query.InvestigationEvidencePacket{}, err
	}
	if envelope.Error != nil {
		if refusal, ok := refusalFromErrorCode(envelope.Error.Code); ok {
			return query.NewInvestigationEvidencePacket(query.InvestigationPacketInput{
				Family:  query.InvestigationFamilySupplyChainImpact,
				Subject: subjectOrPlaceholder(subject),
				Refusal: refusal,
			})
		}
		return query.InvestigationEvidencePacket{}, fmt.Errorf("explain failed: %s: %s", envelope.Error.Code, envelope.Error.Message)
	}
	return query.BuildSupplyChainImpactPacket(envelope.Data, envelope.Truth, packetBoundsFromCmd(cmd))
}

// packetBoundsFromCmd reads the --max-source-facts override into a bounds value,
// returning nil when the flag is unset so the contract defaults apply.
func packetBoundsFromCmd(cmd *cobra.Command) *query.PacketBounds {
	maxSourceFacts, _ := cmd.Flags().GetInt("max-source-facts")
	if maxSourceFacts <= 0 {
		return nil
	}
	return &query.PacketBounds{MaxSourceFacts: maxSourceFacts}
}

func fetchSupplyChainExplain(client *APIClient, filter query.SupplyChainImpactExplanationFilter) (supplyChainExplainEnvelope, error) {
	values := url.Values{}
	addQueryValue(values, "finding_id", filter.FindingID)
	addQueryValue(values, "advisory_id", filter.AdvisoryID)
	addQueryValue(values, "cve_id", filter.CVEID)
	addQueryValue(values, "package_id", filter.PackageID)
	addQueryValue(values, "repository_id", filter.RepositoryID)
	addQueryValue(values, "subject_digest", filter.SubjectDigest)
	path := "/api/v0/supply-chain/impact/explain?" + values.Encode()
	var envelope supplyChainExplainEnvelope
	if err := client.GetEnvelope(path, &envelope); err != nil {
		return supplyChainExplainEnvelope{}, err
	}
	return envelope, nil
}

func supplyChainFilterFromSubject(subject map[string]string) query.SupplyChainImpactExplanationFilter {
	return query.SupplyChainImpactExplanationFilter{
		FindingID:     subject["finding_id"],
		AdvisoryID:    subject["advisory_id"],
		CVEID:         subject["cve_id"],
		PackageID:     subject["package_id"],
		RepositoryID:  subject["repository_id"],
		SubjectDigest: subject["subject_digest"],
	}
}

func supplyChainFilterHasScope(filter query.SupplyChainImpactExplanationFilter) bool {
	if strings.TrimSpace(filter.FindingID) != "" {
		return true
	}
	hasAdvisory := strings.TrimSpace(filter.AdvisoryID) != "" || strings.TrimSpace(filter.CVEID) != ""
	hasTarget := strings.TrimSpace(filter.PackageID) != "" ||
		strings.TrimSpace(filter.RepositoryID) != "" ||
		strings.TrimSpace(filter.SubjectDigest) != ""
	return hasAdvisory && hasTarget
}

// refusalFromErrorCode maps an in-envelope error code to a packet refusal state
// when one applies. Codes without a refusal mapping return false so the caller
// surfaces them as a CLI error.
func refusalFromErrorCode(code query.ErrorCode) (query.PacketRefusalState, bool) {
	switch code {
	case query.ErrorCodeNotFound, query.ErrorCodeScopeNotFound, query.ErrorCodeServiceNotFound:
		return query.PacketRefusalScopeNotFound, true
	case query.ErrorCodeUnsupportedCapability, query.ErrorCodeCapabilityDegraded:
		return query.PacketRefusalProfileUnsupported, true
	case query.ErrorCodeBackendUnavailable, query.ErrorCodeIndexBuilding:
		return query.PacketRefusalBackendUnavailable, true
	default:
		return query.PacketRefusalNone, false
	}
}

// refusalFromFetchError maps a transport-level API error to a refusal state. A
// 404 becomes scope_not_found; a 503 becomes backend_unavailable. Other statuses
// are surfaced to the operator as a CLI error.
func refusalFromFetchError(err error) (query.PacketRefusalState, bool) {
	var httpErr *apiHTTPError
	if !errors.As(err, &httpErr) {
		return query.PacketRefusalNone, false
	}
	switch httpErr.StatusCode {
	case 404:
		return query.PacketRefusalScopeNotFound, true
	case 503:
		return query.PacketRefusalBackendUnavailable, true
	default:
		return query.PacketRefusalNone, false
	}
}

func parseSubjectFlags(raw []string) (map[string]string, error) {
	subject := map[string]string{}
	for _, entry := range raw {
		key, value, ok := strings.Cut(entry, "=")
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if !ok || key == "" || value == "" {
			return nil, fmt.Errorf("invalid --subject %q: expected key=value", entry)
		}
		subject[key] = value
	}
	return subject, nil
}

// subjectOrPlaceholder guarantees a non-empty scope so a refusal packet still
// names what the operator asked for and passes the contract's scope gate.
func subjectOrPlaceholder(subject map[string]string) map[string]string {
	if len(subject) > 0 {
		return subject
	}
	return map[string]string{"requested": "unspecified"}
}

func writeInvestigationArtifact(cmd *cobra.Command, out string, data []byte) error {
	if strings.TrimSpace(out) == "" {
		_, err := cmd.OutOrStdout().Write(data)
		return err
	}
	if err := os.WriteFile(out, data, 0o600); err != nil {
		return fmt.Errorf("write investigation packet: %w", err)
	}
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "wrote investigation packet to %s\n", out)
	return nil
}

func addQueryValue(values url.Values, key, value string) {
	if v := strings.TrimSpace(value); v != "" {
		values.Set(key, v)
	}
}

var investigationExportDepsValue = defaultInvestigationExportDeps()
