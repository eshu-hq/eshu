package terraformstate

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

type warningPayload struct {
	WarningKind string
	Reason      string
	Source      string
}

func (p *stateParser) addSourceWarnings(warnings []SourceWarning) error {
	for _, warning := range warnings {
		payload := warningPayload{
			WarningKind: strings.TrimSpace(warning.WarningKind),
			Reason:      strings.TrimSpace(warning.Reason),
			Source:      strings.TrimSpace(warning.Source),
		}
		if payload.WarningKind == "" || payload.Reason == "" || payload.Source == "" {
			continue
		}
		if err := p.emitWarning(payload); err != nil {
			return err
		}
	}
	return nil
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

func (p *stateParser) emitWarning(warning warningPayload) error {
	if warning.WarningKind == "" || warning.Reason == "" || warning.Source == "" {
		return nil
	}
	payload := map[string]any{
		"warning_kind": warning.WarningKind,
		"reason":       warning.Reason,
		"source":       warning.Source,
	}
	key := "warning:" + warning.WarningKind + ":" + warning.Source + ":" + warning.Reason
	if err := p.emitBodyFact(p.envelope(facts.TerraformStateWarningFactKind, key, payload, warning.Source)); err != nil {
		return err
	}
	if p.warningsByKind == nil {
		p.warningsByKind = map[string]int64{}
	}
	p.warningsByKind[warning.WarningKind]++
	return nil
}
