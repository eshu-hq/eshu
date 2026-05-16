package currentpath

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/semanticeval"
)

const envelopeMediaType = "application/eshu.envelope+json"

// HTTPClient is the subset of http.Client used by Runner.
type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

// Runner executes current-path eval cases against an Eshu HTTP API.
type Runner struct {
	BaseURL string
	Client  HTTPClient
	Timeout time.Duration
}

type responseEnvelope struct {
	Data  json.RawMessage `json:"data"`
	Truth *truthEnvelope  `json:"truth"`
	Error *errorEnvelope  `json:"error"`
}

type truthEnvelope struct {
	Level string `json:"level"`
}

type errorEnvelope struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Run executes every case and returns a scorer-compatible run.
func (runner Runner) Run(ctx context.Context, suite Suite) (semanticeval.Run, error) {
	if err := suite.Validate(); err != nil {
		return semanticeval.Run{}, err
	}
	baseURL, err := url.Parse(runner.BaseURL)
	if err != nil {
		return semanticeval.Run{}, fmt.Errorf("parse base url: %w", err)
	}
	if baseURL.Scheme == "" || baseURL.Host == "" {
		return semanticeval.Run{}, fmt.Errorf("base url must include scheme and host")
	}
	client := runner.Client
	if client == nil {
		client = http.DefaultClient
	}

	run := semanticeval.Run{Results: make([]semanticeval.CaseResult, 0, len(suite.Cases))}
	for _, evalCase := range suite.Cases {
		result, err := runner.runCase(ctx, client, baseURL, evalCase)
		if err != nil {
			return semanticeval.Run{}, err
		}
		run.Results = append(run.Results, result)
	}
	return run, run.Validate()
}

func (runner Runner) runCase(ctx context.Context, client HTTPClient, baseURL *url.URL, evalCase Case) (semanticeval.CaseResult, error) {
	caseTimeout := runner.Timeout
	if evalCase.CurrentPath.TimeoutMS > 0 || caseTimeout <= 0 {
		caseTimeout = time.Duration(evalCase.CurrentPath.timeoutMS()) * time.Millisecond
	}
	caseCtx, cancel := context.WithTimeout(ctx, caseTimeout)
	defer cancel()

	endpoint := *baseURL
	endpoint.Path = strings.TrimRight(baseURL.Path, "/") + evalCase.CurrentPath.endpointPath()

	body, err := json.Marshal(evalCase.CurrentPath.body(evalCase))
	if err != nil {
		return semanticeval.CaseResult{}, fmt.Errorf("case %q marshal request: %w", evalCase.ID, err)
	}
	request, err := http.NewRequestWithContext(caseCtx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return semanticeval.CaseResult{}, fmt.Errorf("case %q build request: %w", evalCase.ID, err)
	}
	request.Header.Set("Accept", envelopeMediaType)
	request.Header.Set("Content-Type", "application/json")

	start := time.Now()
	response, err := client.Do(request)
	latencyMS := float64(time.Since(start).Microseconds()) / 1000
	if err != nil {
		return semanticeval.CaseResult{}, fmt.Errorf("case %q request failed: %w", evalCase.ID, err)
	}
	defer func() {
		_ = response.Body.Close()
	}()

	var envelope responseEnvelope
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		return semanticeval.CaseResult{}, fmt.Errorf("case %q decode response: %w", evalCase.ID, err)
	}
	result := semanticeval.CaseResult{CaseID: evalCase.ID, LatencyMS: latencyMS}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		if envelope.isUnsupported() {
			result.Candidates = unsupportedCandidates(evalCase.ID)
			return result, nil
		}
		return semanticeval.CaseResult{}, fmt.Errorf("case %q returned HTTP %d: %s", evalCase.ID, response.StatusCode, envelope.errorMessage())
	}
	if envelope.Error != nil {
		if envelope.isUnsupported() {
			result.Candidates = unsupportedCandidates(evalCase.ID)
			return result, nil
		}
		return semanticeval.CaseResult{}, fmt.Errorf("case %q returned envelope error: %s", evalCase.ID, envelope.errorMessage())
	}
	truth := mapTruthLevel(envelope.Truth)
	candidates, err := extractCandidates(envelope.Data, truth)
	if err != nil {
		return semanticeval.CaseResult{}, fmt.Errorf("case %q extract candidates: %w", evalCase.ID, err)
	}
	result.Candidates = filterExcludedCandidates(candidates, evalCase.CurrentPath.ExcludeHandles)
	return result, nil
}

func (envelope responseEnvelope) isUnsupported() bool {
	return envelope.Error != nil && envelope.Error.Code == "unsupported_capability"
}

func (envelope responseEnvelope) errorMessage() string {
	if envelope.Error == nil {
		return ""
	}
	if envelope.Error.Message != "" {
		return envelope.Error.Message
	}
	return envelope.Error.Code
}

func unsupportedCandidates(caseID string) []semanticeval.Candidate {
	return []semanticeval.Candidate{{
		Handle: "unsupported://" + caseID,
		Truth:  semanticeval.TruthClassUnsupported,
	}}
}

func filterExcludedCandidates(candidates []semanticeval.Candidate, excludeHandles []string) []semanticeval.Candidate {
	if len(candidates) == 0 || len(excludeHandles) == 0 {
		return candidates
	}
	excluded := make(map[string]struct{}, len(excludeHandles))
	for _, handle := range excludeHandles {
		excluded[handle] = struct{}{}
	}
	filtered := candidates[:0]
	for _, candidate := range candidates {
		if _, ok := excluded[candidate.Handle]; ok {
			continue
		}
		filtered = append(filtered, candidate)
	}
	return filtered
}
