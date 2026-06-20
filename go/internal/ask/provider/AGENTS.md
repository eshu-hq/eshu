# Agent instructions: ask/provider

Scoped instructions for AI agents working in `go/internal/ask/provider`.

## Read these first

Before editing any file in this package, read in order:

1. `doc.go` — package declaration and one-line purpose.
2. `provider.go` — `Adapter` interface, shared types (`Message`, `Tool`, `ToolCall`, `TokenUsage`, `Completion`).
3. `factory.go` — `NewAdapter` entry point, routing table, default base URLs.
4. `anthropic.go` / `anthropic_types.go` — Anthropic Messages adapter and wire types.
5. `openaicompat.go` / `openaicompat_types.go` — OpenAI-compatible adapter and wire types.
6. `credential.go` — `resolveCredential` helper.
7. `transport.go` — shared HTTP doer, error scrubbing, `ProviderError`.

## Package invariants

- **Never leak prompt content, tool arguments, or credentials in errors.** The
  transport scrubs response bodies before wrapping them in `ProviderError`. Any
  new transport path or error path must follow the same scrubbing rule.
- **agent\_reasoning source class is required.** `NewAdapter` enforces this. Never
  bypass the check or add a silent default. The class requirement is the contract
  between the ask pipeline and the semanticprofile config.
- **No external SDK.** All provider communication is plain `net/http` JSON. Do
  not introduce `anthropic-go`, `openai-go`, or equivalent packages.
- **No real network calls in tests.** Inject a mock `httpDoer` or a map-backed
  `getenv`. Tests in `*_test.go` must not dial real provider endpoints.
- **Files stay under 500 lines.** If a file is approaching the limit, extract
  helpers into a sibling file before crossing 450 lines.

## Adding a new provider kind

1. Add the kind constant to `go/internal/semanticprofile/config.go` (and the
   `supportedProviderKinds` slice).
2. Determine the adapter family: Anthropic wire shape or OpenAI-compatible.
3. Add a `case` branch in `NewAdapter` (`factory.go`). Document the default base
   URL (if any) with a package-level constant and a comment explaining why.
4. Add a `TestNewAdapter_<Kind>` test in `factory_test.go` using `staticEnv` and
   a map-backed credential. Confirm the adapter is non-nil and `ModelID()` is
   correct. Do not make real network calls.
5. Update `README.md` to add the new kind to the endpoint behaviour table.

## Anti-patterns

- Do NOT route a profile whose `source_classes` does not include `agent_reasoning`
  through the ask pipeline. Bypass of the source-class check violates the
  contract between ask and semanticprofile.
- Do NOT introduce a real `http.Client` in test code. Use a `mockDoer` or return
  an adapter with `nil` doer and test only factory logic (routing, errors).
- Do NOT add a new provider kind without a corresponding `factory_test.go` entry.
- Do NOT add default base URLs for providers that require operator-supplied
  endpoints (`openai_compatible`, `gemini`, `azure_openai`, `ollama`,
  `internal_gateway`). Return an error and tell the operator to set
  `endpoint_profile_id`.
