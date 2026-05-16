package currentpath

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/semanticeval"
)

const (
	defaultLimit     = 10
	maxLimit         = 50
	defaultTimeoutMS = 2000
	maxTimeoutMS     = 60000
)

// Mode identifies the bounded current Eshu query surface used for a case.
type Mode string

const (
	// ModeCodeSearch calls POST /api/v0/code/search.
	ModeCodeSearch Mode = "code_search"
	// ModeCodeTopic calls POST /api/v0/code/topics/investigate.
	ModeCodeTopic Mode = "code_topic"
	// ModeContentFileSearch calls POST /api/v0/content/files/search.
	ModeContentFileSearch Mode = "content_file_search"
	// ModeContentEntitySearch calls POST /api/v0/content/entities/search.
	ModeContentEntitySearch Mode = "content_entity_search"
)

// Suite is a current-path eval suite with request specs beside scoring cases.
type Suite struct {
	Cases []Case `json:"cases"`
}

// Case adds the current Eshu request to a semanticeval scoring case.
type Case struct {
	semanticeval.Case
	CurrentPath Request `json:"current_path"`
}

// Request describes one bounded current Eshu HTTP query.
type Request struct {
	Mode           Mode     `json:"mode"`
	Query          string   `json:"query,omitempty"`
	RepoID         string   `json:"repo_id,omitempty"`
	Language       string   `json:"language,omitempty"`
	Intent         string   `json:"intent,omitempty"`
	Terms          []string `json:"terms,omitempty"`
	Limit          int      `json:"limit,omitempty"`
	Exact          bool     `json:"exact,omitempty"`
	SearchType     string   `json:"search_type,omitempty"`
	TimeoutMS      int      `json:"timeout_ms,omitempty"`
	ExcludeHandles []string `json:"exclude_handles,omitempty"`
}

// LoadSuiteJSON decodes a strict current-path eval suite.
func LoadSuiteJSON(reader io.Reader) (Suite, error) {
	var suite Suite
	if err := decodeStrictJSON(reader, &suite); err != nil {
		return Suite{}, err
	}
	return suite, suite.Validate()
}

// Validate checks that the suite can be executed and scored deterministically.
func (suite Suite) Validate() error {
	if len(suite.Cases) == 0 {
		return fmt.Errorf("suite must include at least one case")
	}
	seenCases := map[string]struct{}{}
	for _, evalCase := range suite.Cases {
		if err := evalCase.Validate(); err != nil {
			return err
		}
		if _, ok := seenCases[evalCase.ID]; ok {
			return fmt.Errorf("duplicate case id %q", evalCase.ID)
		}
		seenCases[evalCase.ID] = struct{}{}
	}
	return nil
}

// EvalSuite returns the scorer-only suite for the current-path cases.
func (suite Suite) EvalSuite() semanticeval.Suite {
	cases := make([]semanticeval.Case, 0, len(suite.Cases))
	for _, evalCase := range suite.Cases {
		cases = append(cases, evalCase.Case)
	}
	return semanticeval.Suite{Cases: cases}
}

// Validate checks one current-path case.
func (evalCase Case) Validate() error {
	if err := evalCase.Case.Validate(); err != nil {
		return err
	}
	if err := evalCase.CurrentPath.Validate(); err != nil {
		return fmt.Errorf("case %q current_path: %w", evalCase.ID, err)
	}
	return nil
}

// Validate checks that a request is bounded and names a supported surface.
func (request Request) Validate() error {
	switch request.Mode {
	case ModeCodeSearch, ModeCodeTopic, ModeContentFileSearch, ModeContentEntitySearch:
	default:
		return fmt.Errorf("unsupported mode %q", request.Mode)
	}
	if request.Limit < 0 || request.Limit > maxLimit {
		return fmt.Errorf("limit must be between 0 and %d", maxLimit)
	}
	if request.TimeoutMS < 0 || request.TimeoutMS > maxTimeoutMS {
		return fmt.Errorf("timeout_ms must be between 0 and %d", maxTimeoutMS)
	}
	for _, handle := range request.ExcludeHandles {
		if strings.TrimSpace(handle) == "" {
			return fmt.Errorf("exclude_handles must not include blank handles")
		}
	}
	return nil
}

func (request Request) endpointPath() string {
	switch request.Mode {
	case ModeCodeSearch:
		return "/api/v0/code/search"
	case ModeCodeTopic:
		return "/api/v0/code/topics/investigate"
	case ModeContentFileSearch:
		return "/api/v0/content/files/search"
	case ModeContentEntitySearch:
		return "/api/v0/content/entities/search"
	default:
		return ""
	}
}

func (request Request) queryText(evalCase Case) string {
	if strings.TrimSpace(request.Query) != "" {
		return request.Query
	}
	return evalCase.Question
}

func (request Request) repoID(evalCase Case) string {
	if request.RepoID != "" {
		return request.RepoID
	}
	if evalCase.Scope == nil {
		return ""
	}
	if repoID := evalCase.Scope["repo_id"]; repoID != "" {
		return repoID
	}
	return evalCase.Scope["repo"]
}

func (request Request) limit() int {
	if request.Limit <= 0 {
		return defaultLimit
	}
	return request.Limit
}

func (request Request) timeoutMS() int {
	if request.TimeoutMS <= 0 {
		return defaultTimeoutMS
	}
	return request.TimeoutMS
}

func (request Request) body(evalCase Case) map[string]any {
	query := request.queryText(evalCase)
	body := map[string]any{
		"limit": request.limit(),
	}
	if repoID := request.repoID(evalCase); repoID != "" {
		body["repo_id"] = repoID
	}
	if request.Language != "" {
		body["language"] = request.Language
	}
	switch request.Mode {
	case ModeCodeSearch:
		body["query"] = query
		if request.Exact {
			body["exact"] = true
		}
		if request.SearchType != "" {
			body["search_type"] = request.SearchType
		}
	case ModeCodeTopic:
		body["topic"] = query
		if request.Intent != "" {
			body["intent"] = request.Intent
		}
		if len(request.Terms) > 0 {
			body["terms"] = request.Terms
		}
	case ModeContentFileSearch, ModeContentEntitySearch:
		body["query"] = query
	}
	return body
}

func decodeStrictJSON(reader io.Reader, target any) error {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing struct{}
	if err := decoder.Decode(&trailing); err == nil {
		return fmt.Errorf("json document contains trailing values")
	} else if err != io.EOF {
		return err
	}
	return nil
}
