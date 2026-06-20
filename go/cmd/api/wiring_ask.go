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

// askHandlerResult bundles the HTTP handler with a setter for the governed
// narration posture. The setter is a no-op when the engine was not built
// (adapter or engine construction failed).
type askHandlerResult struct {
	handler    *query.AskHandler
	setPosture func(func() status.AnswerNarrationStatus)
}

// adapterReady reports whether the engine was successfully constructed. It is
// used by callers to derive the ProviderConfigured gate in the narration
// posture, ensuring the status endpoint reflects real runtime capability rather
// than mere profile presence.
func (r askHandlerResult) adapterReady() bool { return r.handler.Asker != nil }

// buildAskHandler constructs an AskHandler for POST /api/v0/ask.
//
// Returns an askHandlerResult with a nil-Asker handler (default-off → 503
// unavailable) and a no-op setPosture in any of the following cases:
//   - ESHU_ASK_ENABLED is not "true"
//   - No agent_reasoning provider profile is configured
//   - The provider adapter cannot be constructed (logged at WARN)
//   - The engine cannot be constructed (logged at WARN)
//
// When all conditions are met and the engine is built, returns a fully-wired
// handler whose setPosture injects the governed posture into the engine.
// apiHandler must be the fully-mounted API mux; the engine's MCPRunner
// dispatches tool calls in-process through it.
//
// Callers MUST call setPosture after buildNarrationPosture so the engine
// narrates only when ResolvePosture returns Available (default-closed). The
// narration posture must be built using adapterReady() from this result so that
// ProviderConfigured reflects whether the adapter was actually constructed.
func buildAskHandler(
	getenv func(string) string,
	apiHandler http.Handler,
	apiKey string,
	logger *slog.Logger,
) askHandlerResult {
	noop := func(func() status.AnswerNarrationStatus) {}
	h := &query.AskHandler{Logger: logger}

	if !isAskEnabled(getenv) {
		return askHandlerResult{handler: h, setPosture: noop}
	}

	profile, ok := resolveAgentReasoningProfile(getenv, logger)
	if !ok {
		return askHandlerResult{handler: h, setPosture: noop}
	}

	adapt, err := provider.NewAdapter(profile, getenv)
	if err != nil {
		if logger != nil {
			logger.Warn("ask: provider adapter construction failed; ask unavailable",
				"err_type", "provider_adapter")
		}
		return askHandlerResult{handler: h, setPosture: noop}
	}

	cat, err := buildAskCatalog()
	if err != nil {
		if logger != nil {
			logger.Warn("ask: catalog build failed; ask unavailable",
				"err_type", "catalog_build")
		}
		return askHandlerResult{handler: h, setPosture: noop}
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
		return askHandlerResult{handler: h, setPosture: noop}
	}

	h.Asker = &engineAsker{eng: eng}
	// Expose a setter so the caller can inject the posture after it is built
	// from adapterReady(). The engine is captured by closure; the setter is
	// safe to call exactly once before serving requests.
	return askHandlerResult{
		handler:    h,
		setPosture: eng.SetNarrationPosture,
	}
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
