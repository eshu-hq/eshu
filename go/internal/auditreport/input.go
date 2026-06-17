package auditreport

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// AuditInput is the declarative competitive-audit spec. The generator is
// deliberately input-driven rather than scraping competitor repositories
// automatically: the operator records the competitor features and the files
// inspected (so evidence is source-backed, not docs-only), and the generator
// reconciles them against the capability catalog deterministically.
type AuditInput struct {
	// Competitors are the audited competitor repositories.
	Competitors []Competitor `yaml:"competitors"`
}

// Competitor is one audited competitor repository.
type Competitor struct {
	// Name is the competitor identifier (for example graphify).
	Name string `yaml:"name"`
	// Path is the local repository path inspected.
	Path string `yaml:"path"`
	// Findings are the per-feature audit findings.
	Findings []AuditFinding `yaml:"findings"`
}

// AuditFinding is one competitor feature reconciled against Eshu.
type AuditFinding struct {
	// Feature is the competitor capability observed.
	Feature string `yaml:"feature"`
	// CompetitorFiles are the competitor source files that evidence the feature.
	CompetitorFiles []string `yaml:"competitor_files"`
	// EshuCapability optionally names the catalog capability id to reconcile
	// against.
	EshuCapability string `yaml:"eshu_capability"`
	// EvidenceFiles are the Eshu source/doc/test files inspected.
	EvidenceFiles []string `yaml:"evidence_files"`
	// GapClass is the proposed gap classification (preflight taxonomy).
	GapClass string `yaml:"gap_class"`
	// OwnerSurface is the proposed owner surface (preflight taxonomy).
	OwnerSurface string `yaml:"owner_surface"`
	// Notes carries reviewer context.
	Notes string `yaml:"notes"`
}

// OpenIssue is a minimal open GitHub issue used for duplicate detection. Pass
// the output of `gh issue list --json number,title` so the generator stays
// offline and deterministic.
type OpenIssue struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
}

// LoadInput reads an audit input spec from a YAML file.
func LoadInput(path string) (AuditInput, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return AuditInput{}, fmt.Errorf("read audit input %s: %w", path, err)
	}
	var input AuditInput
	if err := yaml.Unmarshal(raw, &input); err != nil {
		return AuditInput{}, fmt.Errorf("parse audit input %s: %w", path, err)
	}
	return input, nil
}
