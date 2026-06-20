package main

import (
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/ask/catalog"
	"github.com/eshu-hq/eshu/go/internal/ask/engine"
	"github.com/eshu-hq/eshu/go/internal/ask/provider"
	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
	"github.com/eshu-hq/eshu/go/internal/mcp"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/semanticprofile"
	"github.com/eshu-hq/eshu/go/internal/status"
)

// envAskEnabled is the environment variable that enables the Ask Eshu endpoint.
// Default is false (disabled). Set to "true" to enable.
const envAskEnabled = "ESHU_ASK_ENABLED"

// engineAsker adapts *engine.Engine to query.Asker. It lives in cmd/api to
// avoid a cycle: ask/engine imports query; query must not import ask/engine.
type engineAsker struct {
	eng *engine.Engine
}

// Ask implements query.Asker. It forwards the question to the engine using
// the request context (carries deadline + cancellation).
func (a *engineAsker) Ask(r *http.Request, question string) (query.AskAnswer, error) {
	ans, err := a.eng.Ask(r.Context(), question)
	if err != nil {
		return query.AskAnswer{}, err
	}
	return convertAnswer(ans), nil
}

// convertAnswer maps engine.Answer to query.AskAnswer without any import of
// query in ask/engine (the conversion is one-way, caller-side only).
func convertAnswer(ans engine.Answer) query.AskAnswer {
	trace := make([]query.AskTraceEntry, len(ans.Trace))
	for i, t := range ans.Trace {
		trace[i] = query.AskTraceEntry{
			Tool:       t.Tool,
			Args:       t.Args,
			Supported:  t.Supported,
			TruthClass: query.AnswerTruthClass(t.TruthClass),
			Err:        t.Err,
		}
	}
	return query.AskAnswer{
		Prose:       ans.Prose,
		Narrated:    ans.Narrated,
		Packets:     ans.Packets,
		Trace:       trace,
		Partial:     ans.Partial,
		Limitations: ans.Limitations,
	}
}

// buildAskHandler constructs an AskHandler for POST /api/v0/ask.
//
// Returns an AskHandler with a nil Asker (default-off → 503 unavailable) in
// any of the following cases:
//   - ESHU_ASK_ENABLED is not "true"
//   - No agent_reasoning provider profile is configured
//   - The provider adapter cannot be constructed (logged at WARN)
//   - The engine cannot be constructed (logged at WARN)
//
// When all conditions are met and the engine is built, returns a fully-wired
// AskHandler. apiHandler must be the fully-mounted API mux; the engine's
// MCPRunner dispatches tool calls in-process through it.
//
// narrationPosture is the governed posture func built by buildNarrationPosture.
// It is injected into the engine via SetNarrationPosture so the engine narrates
// only when ResolvePosture returns Available (default-closed).
func buildAskHandler(
	getenv func(string) string,
	apiHandler http.Handler,
	apiKey string,
	narrationPosture func() status.AnswerNarrationStatus,
	logger *slog.Logger,
) *query.AskHandler {
	h := &query.AskHandler{Logger: logger}

	if !isAskEnabled(getenv) {
		return h // nil Asker → default-off
	}

	profile, ok := resolveAgentReasoningProfile(getenv, logger)
	if !ok {
		return h
	}

	adapt, err := provider.NewAdapter(profile, getenv)
	if err != nil {
		if logger != nil {
			logger.Warn("ask: provider adapter construction failed; ask unavailable",
				"err_type", "provider_adapter")
		}
		return h
	}

	cat, err := buildAskCatalog()
	if err != nil {
		if logger != nil {
			logger.Warn("ask: catalog build failed; ask unavailable",
				"err_type", "catalog_build")
		}
		return h
	}

	tools := engine.Toolset(cat, mcp.ReadOnlyTools())

	// The bearer token used by the in-process runner to authorize MCP tool
	// calls on behalf of the system (shared-token path).
	authHeader := ""
	if apiKey != "" {
		authHeader = "Bearer " + apiKey
	}

	runner := engine.NewMCPRunner(apiHandler, authHeader, logger)

	eng, err := engine.New(adapt, runner, tools, engine.DefaultOptions())
	if err != nil {
		if logger != nil {
			logger.Warn("ask: engine construction failed; ask unavailable",
				"err_type", "engine_construction")
		}
		return h
	}

	// Wire the governed narration posture so the engine narrates only when
	// ResolvePosture returns Available. A nil narrationPosture leaves the
	// engine in the safe Unavailable default.
	if narrationPosture != nil {
		eng.SetNarrationPosture(narrationPosture)
	}

	h.Asker = &engineAsker{eng: eng}
	return h
}

// isAskEnabled reports whether ESHU_ASK_ENABLED is set to "true".
func isAskEnabled(getenv func(string) string) bool {
	return strings.EqualFold(strings.TrimSpace(getenv(envAskEnabled)), "true")
}

// resolveAgentReasoningProfile finds the first agent_reasoning provider
// profile from the JSON config in the environment. Returns (profile, true)
// when found, (zero, false) otherwise.
func resolveAgentReasoningProfile(
	getenv func(string) string,
	logger *slog.Logger,
) (semanticprofile.ProviderProfile, bool) {
	raw := strings.TrimSpace(getenv(semanticprofile.EnvProviderProfilesJSON))
	if raw == "" {
		return semanticprofile.ProviderProfile{}, false
	}
	profiles, err := semanticprofile.ParseProfilesJSON(raw)
	if err != nil {
		if logger != nil {
			logger.Warn("ask: cannot parse provider profiles; ask unavailable",
				"err_type", "profile_parse")
		}
		return semanticprofile.ProviderProfile{}, false
	}
	for _, p := range profiles {
		if slices.Contains(p.SourceClasses, semanticprofile.SourceAgentReasoning) {
			return p, true
		}
	}
	return semanticprofile.ProviderProfile{}, false
}

// buildAskCatalog parses the embedded surface-inventory artifact into the
// ask/catalog planner format and annotates it.
func buildAskCatalog() (*catalog.Catalog, error) {
	raw := capabilitycatalog.RawSurfaceArtifact()
	cat, err := catalog.Parse(raw)
	if err != nil {
		return nil, err
	}
	cat.Annotate()
	return cat, nil
}
