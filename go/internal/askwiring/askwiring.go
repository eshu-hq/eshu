package askwiring

import (
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/ask/catalog"
	"github.com/eshu-hq/eshu/go/internal/ask/engine"
	"github.com/eshu-hq/eshu/go/internal/ask/governance"
	"github.com/eshu-hq/eshu/go/internal/ask/provider"
	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
	"github.com/eshu-hq/eshu/go/internal/mcp"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/semanticprofile"
	"github.com/eshu-hq/eshu/go/internal/status"
)

// EnvAskEnabled is the environment variable that enables the Ask Eshu endpoint.
// Default is false (disabled). Set to "true" to enable.
const EnvAskEnabled = "ESHU_ASK_ENABLED"

// EnvAskNarrationEnabled is the environment variable that enables governed
// answer narration. Both ESHU_ASK_ENABLED and ESHU_ASK_NARRATION_ENABLED must
// be "true" for narration to be permitted; either alone is insufficient.
//
// Default: false (narration is default-closed).
const EnvAskNarrationEnabled = "ESHU_ASK_NARRATION_ENABLED"

// EnvAskMaxIterations is the environment variable that overrides the agent
// loop's maximum reasoning iterations (LLM completion / tool-call rounds).
// Weaker providers may need more rounds to converge; operators raise this knob
// instead of the feature returning empty partial answers. An unset, empty,
// non-numeric, zero, or negative value keeps the engine default. Values above
// MaxAskIterationsCeiling are clamped so a misconfiguration cannot create an
// unbounded provider-spend loop.
const EnvAskMaxIterations = "ESHU_ASK_MAX_ITERATIONS"

// EnvAskMaxToolCallsPerTurn is the environment variable that overrides the
// agent loop's maximum dispatched tool calls per completion turn. The same
// parse, default, and clamp rules as EnvAskMaxIterations apply.
const EnvAskMaxToolCallsPerTurn = "ESHU_ASK_MAX_TOOL_CALLS_PER_TURN"

// MaxAskIterationsCeiling bounds ESHU_ASK_MAX_ITERATIONS so an operator
// override cannot turn Ask into an unbounded provider-spend loop. The ceiling
// is generous enough for weak providers that need many tool-call rounds while
// keeping a hard safety cap.
const MaxAskIterationsCeiling = 32

// MaxAskToolCallsPerTurnCeiling bounds ESHU_ASK_MAX_TOOL_CALLS_PER_TURN for the
// same reason as MaxAskIterationsCeiling.
const MaxAskToolCallsPerTurnCeiling = 16

// HandlerResult bundles the HTTP handler with a setter for the governed
// narration posture. The setter is a no-op when the engine was not built
// (adapter or engine construction failed).
type HandlerResult struct {
	Handler    *query.AskHandler
	SetPosture func(func() status.AnswerNarrationStatus)
}

// AdapterReady reports whether the engine was successfully constructed. It is
// used by callers to derive the ProviderConfigured gate in the narration
// posture, ensuring the status endpoint reflects real runtime capability rather
// than mere profile presence.
func (r HandlerResult) AdapterReady() bool { return r.Handler.Asker != nil }

// BuildAskHandler constructs a [query.AskHandler] for POST /api/v0/ask.
//
// Returns a HandlerResult with a nil-Asker handler (default-off → 503
// unavailable) and a no-op SetPosture in any of the following cases:
//   - ESHU_ASK_ENABLED is not "true"
//   - No agent_reasoning provider profile is configured
//   - The provider adapter cannot be constructed (logged at WARN)
//   - The engine cannot be constructed (logged at WARN)
//
// When all conditions are met and the engine is built, returns a fully-wired
// handler whose SetPosture injects the governed posture into the engine.
// inProcessHandler must be the fully-mounted application mux; the engine's
// MCPRunner dispatches tool calls in-process through it.
//
// Callers MUST call SetPosture after BuildNarrationPosture so the engine
// narrates only when ResolvePosture returns Available (default-closed). The
// narration posture must be built using AdapterReady() from this result so that
// ProviderConfigured reflects whether the adapter was actually constructed.
func BuildAskHandler(
	getenv func(string) string,
	inProcessHandler http.Handler,
	apiKey string,
	logger *slog.Logger,
) HandlerResult {
	noop := func(func() status.AnswerNarrationStatus) {}
	h := &query.AskHandler{Logger: logger}

	if !IsAskEnabled(getenv) {
		return HandlerResult{Handler: h, SetPosture: noop}
	}

	profile, ok := ResolveAgentReasoningProfile(getenv, logger)
	if !ok {
		return HandlerResult{Handler: h, SetPosture: noop}
	}

	adapt, err := provider.NewAdapter(profile, getenv)
	if err != nil {
		if logger != nil {
			logger.Warn("ask: provider adapter construction failed; ask unavailable",
				"err_type", "provider_adapter")
		}
		return HandlerResult{Handler: h, SetPosture: noop}
	}

	cat, err := buildAskCatalog()
	if err != nil {
		if logger != nil {
			logger.Warn("ask: catalog build failed; ask unavailable",
				"err_type", "catalog_build")
		}
		return HandlerResult{Handler: h, SetPosture: noop}
	}

	tools := toolsetExcludingAsk(cat)

	// The bearer token used by the in-process runner to authorize MCP tool
	// calls on behalf of the system (shared-token path).
	authHeader := ""
	if apiKey != "" {
		authHeader = "Bearer " + apiKey
	}

	runner := engine.NewMCPRunner(inProcessHandler, authHeader, logger)

	opts := ResolveEngineOptions(getenv, logger)
	eng, err := engine.New(adapt, runner, tools, opts)
	if err != nil {
		if logger != nil {
			logger.Warn("ask: engine construction failed; ask unavailable",
				"err_type", "engine_construction")
		}
		return HandlerResult{Handler: h, SetPosture: noop}
	}
	eng.SetLogger(logger)
	if logger != nil {
		logger.Info("ask: engine budget resolved",
			"max_iterations", opts.MaxIterations,
			"max_tool_calls_per_turn", opts.MaxToolCallsPerTurn)
	}

	h.Asker = &engineAsker{eng: eng}
	// Expose a setter so the caller can inject the posture after it is built
	// from AdapterReady(). The engine is captured by closure; the setter is
	// safe to call exactly once before serving requests.
	return HandlerResult{
		Handler:    h,
		SetPosture: eng.SetNarrationPosture,
	}
}

// BuildNarrationPosture constructs a func that resolves the current governed
// answer-narration posture from runtime configuration. The returned func is
// safe to call concurrently and reads only from the closed-over values, so it
// can be shared between the engine and the status endpoint.
//
// adapterReady must reflect whether the ask provider adapter was ACTUALLY
// successfully constructed (i.e. provider.NewAdapter succeeded). A profile
// that is present in JSON but whose credential env var is unset will fail
// adapter construction; in that case adapterReady must be false so the status
// endpoint reports ProviderUnavailable rather than Available.
//
// Gate derivation (v1):
//   - ProviderConfigured     = adapterReady (adapter was actually built).
//   - ProviderTrafficEnabled = ESHU_ASK_ENABLED=true AND ESHU_ASK_NARRATION_ENABLED=true.
//   - PolicyAllowed          = same as ProviderTrafficEnabled (v1 conservative default).
//   - BudgetAvailable        = same as ProviderTrafficEnabled (v1 conservative default).
//   - PublishSafetyEnabled   = same as ProviderTrafficEnabled (v1 conservative default).
//
// The posture is default-CLOSED: if any gate is false, ResolvePosture returns
// a non-Available state and the engine will not narrate.
func BuildNarrationPosture(
	getenv func(string) string,
	adapterReady bool,
) func() status.AnswerNarrationStatus {
	trafficEnabled := IsAskEnabled(getenv) && IsNarrationEnabled(getenv)

	return func() status.AnswerNarrationStatus {
		in := governance.PostureInputs{
			ProviderConfigured:     adapterReady,
			ProviderTrafficEnabled: trafficEnabled,
			// v1 conservative wiring: policy, budget, and publish safety are
			// gated to the same bool as traffic. They are documented as
			// defaulting to false unless traffic is open, which satisfies the
			// default-closed requirement while giving operators a single pair
			// of flags (ESHU_ASK_ENABLED + ESHU_ASK_NARRATION_ENABLED) to
			// open narration in v1 deployments.
			PolicyAllowed:        trafficEnabled,
			BudgetAvailable:      trafficEnabled,
			PublishSafetyEnabled: trafficEnabled,
		}
		return governance.ResolvePosture(in, time.Now().UTC())
	}
}

// IsAskEnabled reports whether ESHU_ASK_ENABLED is set to "true".
func IsAskEnabled(getenv func(string) string) bool {
	return strings.EqualFold(strings.TrimSpace(getenv(EnvAskEnabled)), "true")
}

// IsNarrationEnabled reports whether ESHU_ASK_NARRATION_ENABLED is "true".
func IsNarrationEnabled(getenv func(string) string) bool {
	return strings.EqualFold(strings.TrimSpace(getenv(EnvAskNarrationEnabled)), "true")
}

// ResolveEngineOptions builds the engine [engine.Options] from the budget
// environment variables, starting from the safe engine defaults. It never
// loosens the safety bounds silently: an unset, empty, non-numeric, zero, or
// negative override keeps the default, and an override above the documented
// ceiling is clamped (and logged at WARN). This lets operators widen the budget
// for weaker providers (issue #3356) without removing the hard cap.
func ResolveEngineOptions(getenv func(string) string, logger *slog.Logger) engine.Options {
	opts := engine.DefaultOptions()
	opts.MaxIterations = resolveBudget(
		getenv, EnvAskMaxIterations, opts.MaxIterations, MaxAskIterationsCeiling, logger)
	opts.MaxToolCallsPerTurn = resolveBudget(
		getenv, EnvAskMaxToolCallsPerTurn, opts.MaxToolCallsPerTurn, MaxAskToolCallsPerTurnCeiling, logger)
	return opts
}

// resolveBudget parses one budget env var. It returns def when the value is
// unset, empty, non-numeric, zero, or negative, and clamps any value above
// ceiling to ceiling. Clamping is logged at WARN so an operator can see that
// their override was bounded.
func resolveBudget(
	getenv func(string) string,
	key string,
	def int,
	ceiling int,
	logger *slog.Logger,
) int {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		if logger != nil {
			logger.Warn("ask: ignoring invalid budget override; using default",
				"env", key, "default", def)
		}
		return def
	}
	if n > ceiling {
		if logger != nil {
			logger.Warn("ask: budget override above ceiling; clamping",
				"env", key, "requested", n, "ceiling", ceiling)
		}
		return ceiling
	}
	return n
}

// ResolveAgentReasoningProfile finds the first agent_reasoning provider
// profile from the JSON config in the environment. Returns (profile, true)
// when found, (zero, false) otherwise.
func ResolveAgentReasoningProfile(
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

// toolsetExcludingAsk builds the engine tool slice from all read-only MCP
// tools, excluding the "ask" tool itself.
//
// Excluding "ask" prevents a recursive engine invocation: the MCP server
// advertises "ask" as a client-facing tool, and that same tool dispatches POST
// /api/v0/ask back to the in-process handler. If the engine's toolset includes
// "ask", a model that selects the tool during an Ask session recursively calls
// the engine — burning provider calls until the context deadline or a depth
// limit stops it. Removing "ask" from the engine toolset makes the recursion
// structurally impossible.
func toolsetExcludingAsk(cat *catalog.Catalog) []provider.Tool {
	defs := mcp.ReadOnlyTools()
	filtered := make([]mcp.ToolDefinition, 0, len(defs))
	for _, d := range defs {
		if d.Name == "ask" {
			continue
		}
		filtered = append(filtered, d)
	}
	return engine.Toolset(cat, filtered)
}

// engineAsker adapts *engine.Engine to query.Asker. It lives in askwiring to
// avoid a cycle: ask/engine imports query; query must not import ask/engine.
type engineAsker struct {
	eng *engine.Engine
}

// Ask implements query.Asker. It forwards the question to the engine using
// the request context (carries deadline + cancellation).
func (a *engineAsker) Ask(r *http.Request, question string) (query.AskAnswer, error) {
	// Thread the caller's Authorization header into the engine context so the
	// in-process runner authorizes every inner tool call as the caller (scoped
	// or shared) rather than always as the shared admin token. Combined with
	// routing the runner through the scoped-auth-wrapped handler, this confines
	// a scoped caller's Ask to scoped-safe routes.
	ctx := engine.ContextWithCallerAuthHeader(r.Context(), r.Header.Get("Authorization"))
	ans, err := a.eng.Ask(ctx, question)
	if err != nil {
		return query.AskAnswer{}, err
	}
	return convertAnswer(ans), nil
}

// AskStream implements query.Asker. It drives engine.AskStream, mapping engine
// StreamEvents to query.AskStreamEvents and forwarding them to emit. If the
// engine adapter does not support streaming, it returns query.ErrNoStreaming so
// the SSE handler falls back to the synchronous Ask path.
func (a *engineAsker) AskStream(r *http.Request, question string, emit func(query.AskStreamEvent)) (query.AskAnswer, error) {
	// Thread the caller's Authorization header into the engine context so the
	// streaming path enforces the caller's scope on every inner tool call,
	// exactly as Ask does. Without this, a scoped streaming request would fall
	// back to the baked-in shared token and leak cross-scope data.
	ctx := engine.ContextWithCallerAuthHeader(r.Context(), r.Header.Get("Authorization"))
	ans, err := a.eng.AskStream(ctx, question, func(ev engine.StreamEvent) {
		switch ev.Kind {
		case engine.KindToken:
			emit(query.AskStreamEvent{Kind: "token", TextDelta: ev.TextDelta})
		case engine.KindToolCallStarted:
			emit(query.AskStreamEvent{
				Kind:       "tool_call_started",
				ToolCallID: ev.ToolCallID,
				ToolName:   ev.ToolName,
			})
		case engine.KindTraceEntry:
			if ev.TraceEntry != nil {
				te := &query.AskTraceEntry{
					Tool:       ev.TraceEntry.Tool,
					Args:       ev.TraceEntry.Args,
					Supported:  ev.TraceEntry.Supported,
					TruthClass: query.AnswerTruthClass(ev.TraceEntry.TruthClass),
					Err:        ev.TraceEntry.Err,
				}
				emit(query.AskStreamEvent{Kind: "trace_entry", TraceEntry: te})
			}
		}
	})
	if err != nil {
		if err == engine.ErrNoStreaming {
			return query.AskAnswer{}, query.ErrNoStreaming
		}
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
