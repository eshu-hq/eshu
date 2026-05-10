package terraformstate

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

type warningPayload struct {
	WarningKind string
	Reason      string
	Source      string
}

func (p *stateParser) addSourceWarnings(warnings []SourceWarning) {
	for _, warning := range warnings {
		payload := warningPayload{
			WarningKind: strings.TrimSpace(warning.WarningKind),
			Reason:      strings.TrimSpace(warning.Reason),
			Source:      strings.TrimSpace(warning.Source),
		}
		if payload.WarningKind == "" || payload.Reason == "" || payload.Source == "" {
			continue
		}
		p.warnings = append(p.warnings, payload)
	}
}

func (p *stateParser) recordRedaction(reason string) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "unknown"
	}
	if p.redactions == nil {
		p.redactions = map[string]int64{}
	}
	p.redactions[reason]++
}

func (p *stateParser) emitWarnings() {
	sort.Slice(p.warnings, func(i, j int) bool {
		left := p.warnings[i]
		right := p.warnings[j]
		return left.WarningKind+"\x00"+left.Source+"\x00"+left.Reason < right.WarningKind+"\x00"+right.Source+"\x00"+right.Reason
	})
	for _, warning := range p.warnings {
		payload := map[string]any{
			"warning_kind": warning.WarningKind,
			"reason":       warning.Reason,
			"source":       warning.Source,
		}
		key := "warning:" + warning.WarningKind + ":" + warning.Source + ":" + warning.Reason
		p.facts = append(p.facts, p.envelope(facts.TerraformStateWarningFactKind, key, payload, warning.Source))
	}
}
