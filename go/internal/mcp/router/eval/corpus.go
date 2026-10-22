// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package eval

import (
	"bytes"
	_ "embed" // corpus.json ships inside the binary and the test.
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/eshu-hq/eshu/go/internal/mcp/router"
)

// SchemaVersion is the corpus document version Load accepts.
const SchemaVersion = 1

// Question families. The first six are the families in the epic's table; the
// rest cover the remaining tool groups so every registered group is measured.
const (
	FamilyCodeLookup         = "code_lookup"
	FamilyCodeGraph          = "code_graph"
	FamilyDeadCode           = "dead_code"
	FamilyChangeImpact       = "change_impact"
	FamilyServiceWorkload    = "service_workload"
	FamilyInfrastructure     = "infrastructure"
	FamilyDriftReplatforming = "drift_replatforming"
	FamilySupplyChain        = "supply_chain"
	FamilySecurityPosture    = "security_posture"
	FamilyRepositoryOverview = "repository_overview"
	FamilyDocumentation      = "documentation"
	FamilyOperations         = "operations"
	FamilyInvestigation      = "investigation"
)

// knownFamilies is the closed set of family labels a question may carry.
var knownFamilies = map[string]bool{
	FamilyCodeLookup: true, FamilyCodeGraph: true, FamilyDeadCode: true,
	FamilyChangeImpact: true, FamilyServiceWorkload: true, FamilyInfrastructure: true,
	FamilyDriftReplatforming: true, FamilySupplyChain: true, FamilySecurityPosture: true,
	FamilyRepositoryOverview: true, FamilyDocumentation: true, FamilyOperations: true,
	FamilyInvestigation: true,
}

// knownSources is the closed set of provenance labels a question may carry.
// It records where the wording came from, so a reviewer can see the corpus was
// not derived from the router's own intent phrases.
var knownSources = map[string]bool{
	"description":    true, // paraphrased from a tool description
	"playbook":       true, // drawn from a query playbook or investigation workflow
	"sweep":          true, // drawn from the catalog sweep arguments
	"starter_prompt": true, // drawn from the public starter prompts
	"hand_written":   true, // written for a gap the other sources leave
}

// routerOnlyTools are surfaces that exist but are never offered as a target,
// so no question may expect them.
var routerOnlyTools = map[string]bool{
	"find_tool": true,
	"call_tool": true,
	"ask":       true,
}

//go:embed testdata/corpus.json
var embeddedCorpus []byte

// Anchors are the optional entity identifiers a caller already holds when it
// asks find_tool. They mirror router.Anchors.
type Anchors struct {
	RepoID      string `json:"repo_id,omitempty"`
	ServiceID   string `json:"service_id,omitempty"`
	WorkloadID  string `json:"workload_id,omitempty"`
	Environment string `json:"environment,omitempty"`
}

// toRouter converts the corpus anchors to the router contract type.
func (a Anchors) toRouter() router.Anchors {
	return router.Anchors{
		RepoID: a.RepoID, ServiceID: a.ServiceID,
		WorkloadID: a.WorkloadID, Environment: a.Environment,
	}
}

// Question is one routing case: a plain-language ask and the tool an agent
// should be offered for it.
type Question struct {
	// ID is a stable, unique label such as "code-lookup-001".
	ID string `json:"id"`
	// Family groups the question for per-family accuracy.
	Family string `json:"family"`
	// Question is the text sent to the ranker as the intent.
	Question string `json:"question"`
	// Expected is the registered tool that best answers the question.
	Expected string `json:"expected"`
	// Alternates are other registered tools that also answer it acceptably.
	Alternates []string `json:"alternates,omitempty"`
	// Anchors are optional identifiers passed alongside the intent.
	Anchors Anchors `json:"anchors,omitempty"`
	// Source records where the wording came from.
	Source string `json:"source"`
}

// Corpus is the versioned set of routing questions.
type Corpus struct {
	// Version must equal SchemaVersion.
	Version int `json:"version"`
	// Questions are the cases, in a stable order that reports follow.
	Questions []Question `json:"questions"`
}

// Default loads the corpus embedded in the binary.
func Default() (*Corpus, error) {
	return Load(bytes.NewReader(embeddedCorpus))
}

// Load decodes one corpus document. It rejects unknown fields, trailing data,
// and an unsupported version, so a typo cannot silently drop a field.
func Load(r io.Reader) (*Corpus, error) {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	var c Corpus
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("decode corpus: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("decode corpus: unexpected data after the document")
	}
	if c.Version != SchemaVersion {
		return nil, fmt.Errorf("corpus version %d, want %d", c.Version, SchemaVersion)
	}
	return &c, nil
}
