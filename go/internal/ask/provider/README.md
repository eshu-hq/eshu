# ask/provider

Package `provider` is the provider-neutral completion layer for the Eshu ask pipeline.
It defines a minimal `Adapter` interface and ships two concrete adapter families —
Anthropic Messages and OpenAI-compatible — driven entirely by
`semanticprofile.ProviderProfile` configuration. No SDK is used; every call is a
plain JSON HTTP request.

## Adapter families

### Anthropic adapter (`anthropic.go`)

Handles `provider_kind` values `anthropic` and `bedrock`. Both use the Anthropic
Messages API wire shape with native `tool_use` / `tool_result` content blocks.

When `endpoint_profile_id` is empty the adapter defaults to
`https://api.anthropic.com`. Bedrock endpoints must be supplied via
`endpoint_profile_id`.

### OpenAI-compatible adapter (`openaicompat.go`)

Handles all other supported provider kinds:
`openai_compatible`, `minimax`, `deepseek`, `gemini`, `azure_openai`, `ollama`,
`internal_gateway`.

All of these providers expose the same `POST /v1/chat/completions` contract, so
a single adapter covers them. The adapter sends `tool` / `function` definitions
and parses tool-call arguments from the response JSON.

## Profile-driven selection

Call `NewAdapter(profile, getenv)` to obtain an `Adapter` from a fully resolved
`semanticprofile.ProviderProfile`. The factory enforces two pre-conditions before
constructing any adapter:

1. **agent\_reasoning source class required.** The profile's `source_classes` slice
   must contain `agent_reasoning`. Profiles for code search, documentation lookup,
   or other source classes must not be routed through the ask pipeline.
2. **Credential resolution.** Credentials are resolved at construction time via
   `resolveCredential`. For `environment_variable` sources the factory calls
   `getenv(handle)`; for `cloud_workload_identity` sources the credential is empty
   (auth is handled out of band by the runtime). Construction fails fast if a
   required env var is absent.

## EndpointProfileID-as-base-URL convention

`profile.EndpointProfileID` is treated as a base URL override. The transport
appends the provider-specific path suffix (`/v1/messages` or
`/v1/chat/completions`). When `EndpointProfileID` is empty:

| Provider kind       | Factory behaviour                                      |
|---------------------|--------------------------------------------------------|
| `anthropic`         | Defaults to `https://api.anthropic.com`               |
| `bedrock`           | Requires an explicit endpoint; error if absent         |
| `minimax`           | Defaults to `https://api.minimax.io`                   |
| `deepseek`          | Defaults to `https://api.deepseek.com`                 |
| `openai_compatible` | Error: `endpoint_profile_id` is required               |
| `gemini`            | Error: `endpoint_profile_id` is required               |
| `azure_openai`      | Error: `endpoint_profile_id` is required               |
| `ollama`            | Error: `endpoint_profile_id` is required               |
| `internal_gateway`  | Error: `endpoint_profile_id` is required               |

## Leak-free invariant

The transport (`transport.go`) wraps every non-2xx response into a
`ProviderError` value that contains only the HTTP status code (never the
response body). Prompt content, tool arguments, and credential material
are never included in returned errors.

## How to add a new provider kind

1. Add the kind constant to `go/internal/semanticprofile/config.go`.
2. Determine which adapter family the provider belongs to (Anthropic wire shape
   vs. OpenAI-compatible).
3. Extend the `switch` in `NewAdapter` (`factory.go`) with a new `case` branch.
   Supply a documented default base URL when the provider has a stable public
   endpoint, or require `EndpointProfileID` and return an error when empty.
4. Add a `factory_test.go` test for the new kind using a map-backed `getenv`
   stub. No real network calls in tests.
