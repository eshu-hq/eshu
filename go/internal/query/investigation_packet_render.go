package query

import (
	"encoding/json"
	"fmt"
	"html"
	"sort"
	"strings"
)

// InvestigationPacketFormat is a supported render format for a packet artifact.
type InvestigationPacketFormat string

const (
	// InvestigationPacketFormatJSON renders the canonical machine-readable packet.
	InvestigationPacketFormatJSON InvestigationPacketFormat = "json"
	// InvestigationPacketFormatMarkdown renders a human-readable summary.
	InvestigationPacketFormatMarkdown InvestigationPacketFormat = "md"
	// InvestigationPacketFormatHTML renders a self-contained HTML summary.
	InvestigationPacketFormatHTML InvestigationPacketFormat = "html"
)

// ParseInvestigationPacketFormat normalizes a requested render format. An empty
// value defaults to JSON, the canonical machine-readable form.
func ParseInvestigationPacketFormat(raw string) (InvestigationPacketFormat, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", string(InvestigationPacketFormatJSON):
		return InvestigationPacketFormatJSON, nil
	case string(InvestigationPacketFormatMarkdown), "markdown":
		return InvestigationPacketFormatMarkdown, nil
	case string(InvestigationPacketFormatHTML):
		return InvestigationPacketFormatHTML, nil
	default:
		return "", fmt.Errorf("unsupported packet format %q: supported formats are json, md, html", raw)
	}
}

// RenderInvestigationPacket renders a packet in the requested format. JSON is the
// canonical form; markdown and HTML are deterministic human-readable views over
// the same data and add no field beyond the packet.
func RenderInvestigationPacket(packet InvestigationEvidencePacket, format InvestigationPacketFormat) ([]byte, error) {
	switch format {
	case InvestigationPacketFormatJSON:
		return renderInvestigationPacketJSON(packet)
	case InvestigationPacketFormatMarkdown:
		return []byte(renderInvestigationPacketMarkdown(packet)), nil
	case InvestigationPacketFormatHTML:
		return []byte(renderInvestigationPacketHTML(packet)), nil
	default:
		return nil, fmt.Errorf("unsupported packet format %q", format)
	}
}

func renderInvestigationPacketJSON(packet InvestigationEvidencePacket) ([]byte, error) {
	data, err := json.MarshalIndent(packet, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode investigation packet: %w", err)
	}
	return append(data, '\n'), nil
}

// renderInvestigationPacketMarkdown produces a deterministic markdown summary
// covering every evidence layer so a reader can audit the packet without parsing
// JSON. Sections appear in a fixed order and empty layers are stated explicitly.
func renderInvestigationPacketMarkdown(packet InvestigationEvidencePacket) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Investigation Evidence Packet — %s\n\n", packet.Identity.Family)
	fmt.Fprintf(&b, "- packet_id: `%s`\n", packet.PacketID)
	fmt.Fprintf(&b, "- schema: `%s`\n", packet.Schema)
	fmt.Fprintf(&b, "- basis: `%s`\n", packet.Identity.Basis)
	if packet.Refusal != PacketRefusalNone {
		fmt.Fprintf(&b, "- refusal: `%s`\n", packet.Refusal)
	}
	b.WriteString("\n## Scope\n\n")
	for _, key := range sortedKeys(packet.Identity.Subject) {
		fmt.Fprintf(&b, "- %s: `%s`\n", key, packet.Identity.Subject[key])
	}
	if q := strings.TrimSpace(packet.Identity.Question); q != "" {
		fmt.Fprintf(&b, "- question: %s\n", q)
	}

	b.WriteString("\n## Answer\n\n")
	fmt.Fprintf(&b, "- truth_class: `%s`\n", packet.Answer.TruthClass)
	fmt.Fprintf(&b, "- supported: %t\n", packet.Answer.Supported)
	fmt.Fprintf(&b, "- partial: %t\n", packet.Answer.Partial)
	fmt.Fprintf(&b, "- freshness: `%s`\n", packet.Freshness.State)
	if s := strings.TrimSpace(packet.Answer.Summary); s != "" {
		fmt.Fprintf(&b, "\n%s\n", s)
	}
	writeMarkdownReasons(&b, "Unsupported / partial reasons", packet.Answer.UnsupportedReasons)

	writeMarkdownSourceFacts(&b, packet.SourceFacts)
	writeMarkdownDecisions(&b, packet.ReducerDecisions)
	writeMarkdownGraphAnswers(&b, packet.GraphAnswers)
	writeMarkdownMissing(&b, packet.MissingEvidence)
	writeMarkdownSemantic(&b, packet.SemanticObservations)

	b.WriteString("\n## Provenance\n\n")
	fmt.Fprintf(&b, "- redaction: `%s` v%s\n", packet.Redaction.Profile, packet.Redaction.Version)
	fmt.Fprintf(&b, "- validation: `%s`\n", packet.Validation.Status)
	if packet.Bounds.Truncated {
		fmt.Fprintf(&b, "- truncated layers: %s\n", strings.Join(packet.Bounds.TruncatedLayers, ", "))
	}
	writeMarkdownReasons(&b, "Limitations", packet.Limitations)
	return b.String()
}

func writeMarkdownSourceFacts(b *strings.Builder, facts []PacketSourceFact) {
	fmt.Fprintf(b, "\n## Source facts (%d)\n\n", len(facts))
	if len(facts) == 0 {
		b.WriteString("_none_\n")
		return
	}
	for _, fact := range facts {
		id := packetFirstNonEmpty(fact.FactID, fact.StableKey, "(unkeyed)")
		fmt.Fprintf(b, "- `%s` [%s] %s\n", id, fact.EvidenceFamily, strings.TrimSpace(fact.Summary))
	}
}

func writeMarkdownDecisions(b *strings.Builder, decisions []PacketReducerDecision) {
	fmt.Fprintf(b, "\n## Reducer decisions (%d)\n\n", len(decisions))
	if len(decisions) == 0 {
		b.WriteString("_none_\n")
		return
	}
	for _, d := range decisions {
		fmt.Fprintf(b, "- **%s** %s — %s\n", d.State, strings.TrimSpace(d.Subject), strings.TrimSpace(d.Reason))
	}
}

func writeMarkdownGraphAnswers(b *strings.Builder, answers []PacketGraphAnswer) {
	fmt.Fprintf(b, "\n## Graph answers (%d)\n\n", len(answers))
	if len(answers) == 0 {
		b.WriteString("_none_\n")
		return
	}
	for _, a := range answers {
		state := "missing"
		if a.Present {
			state = "present"
		}
		label := strings.TrimSpace(a.Hop)
		if rel := strings.TrimSpace(a.Relationship); rel != "" {
			label = fmt.Sprintf("%s %s→%s", rel, strings.TrimSpace(a.From), strings.TrimSpace(a.To))
		}
		fmt.Fprintf(b, "- [%s] %s\n", state, label)
	}
}

func writeMarkdownMissing(b *strings.Builder, missing []PacketMissingHop) {
	fmt.Fprintf(b, "\n## Missing evidence (%d)\n\n", len(missing))
	if len(missing) == 0 {
		b.WriteString("_none_\n")
		return
	}
	for _, m := range missing {
		fmt.Fprintf(b, "- **%s**: %s\n", strings.TrimSpace(m.Hop), strings.TrimSpace(m.Reason))
	}
}

func writeMarkdownSemantic(b *strings.Builder, observations []PacketSemanticObservation) {
	if len(observations) == 0 {
		return
	}
	fmt.Fprintf(b, "\n## Semantic observations (%d, optional)\n\n", len(observations))
	for _, o := range observations {
		fmt.Fprintf(b, "- _[%s]_ %s\n", strings.TrimSpace(o.Provider), strings.TrimSpace(o.Observation))
	}
}

func writeMarkdownReasons(b *strings.Builder, title string, reasons []string) {
	if len(reasons) == 0 {
		return
	}
	fmt.Fprintf(b, "\n### %s\n\n", title)
	for _, r := range reasons {
		fmt.Fprintf(b, "- %s\n", strings.TrimSpace(r))
	}
}

// renderInvestigationPacketHTML wraps the markdown summary in a minimal,
// self-contained HTML document with every value HTML-escaped, so a packet can be
// shared as a standalone page without leaking markup from evidence text.
func renderInvestigationPacketHTML(packet InvestigationEvidencePacket) string {
	var b strings.Builder
	b.WriteString("<!DOCTYPE html>\n<html lang=\"en\">\n<head>\n<meta charset=\"utf-8\">\n")
	fmt.Fprintf(&b, "<title>Investigation Evidence Packet — %s</title>\n", html.EscapeString(string(packet.Identity.Family)))
	b.WriteString("</head>\n<body>\n<pre>\n")
	b.WriteString(html.EscapeString(renderInvestigationPacketMarkdown(packet)))
	b.WriteString("\n</pre>\n</body>\n</html>\n")
	return b.String()
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
