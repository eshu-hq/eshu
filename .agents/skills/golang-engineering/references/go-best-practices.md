# Go Best Practices

Use this file only when the repository does not already answer the question. These defaults are a pragmatic baseline aligned with Google's Go style and best-practice guidance, not a substitute for clear local conventions.

## Priority Order

1. Existing repository conventions
2. Existing package and test patterns near the change
3. These defaults

## Defaults

- Format changed files with `gofmt`. Use `goimports` only when the repo already requires it.
- Prefer readable, idiomatic Go over abstraction-heavy design.
- Prefer the standard library before introducing dependencies.
- Keep packages small, cohesive, and responsibility-oriented.
- Keep exported APIs narrow. Start unexported and widen only for real consumers.
- Keep names short, precise, and contextual. Avoid redundant prefixes and stutter.
- Prefer concrete types until multiple real consumers need an interface.
- Define interfaces where they are consumed.
- Keep functions focused. Split large functions when responsibility or branching becomes hard to follow.
- Make zero values usable when practical, but do not distort the API just to satisfy that rule.
- Use constructors only when invariants must be enforced or setup is non-trivial.
- Prefer explicit control flow and simple data structures over reflection or indirection.

## Package Design

- Organize by domain responsibility, not by generic layers such as `utils`, `helpers`, or `common`.
- Avoid giant packages with mixed concerns.
- Avoid tiny packages that exist only to wrap one type or helper without a real boundary.
- Keep public contracts close to the package purpose.
- If a package is hard to describe in one sentence, the boundary is probably weak.

Prefer this:

```go
package billing

type Client struct {
	httpClient *http.Client
	baseURL    string
}
```

Over this:

```go
package clients

type BillingClientService struct {
	Client *http.Client
	URL    string
}
```

## API Design

- Design the narrowest API that solves the current problem.
- Prefer functions and structs over interface-first designs.
- Keep parameter lists short. Use a config struct when positional parameters become hard to read.
- Return concrete results and explicit errors.
- Do not generalize for hypothetical reuse.
- Avoid getters and setters unless the repo or interoperability needs them.

## Naming

- Use names that make sense at the call site.
- Do not repeat package names in exported identifiers.
- Avoid abbreviations unless they are standard and obvious in the repo.
- Keep receiver names short and consistent.

Prefer this:

```go
package cache

type Store struct{}

func (s *Store) Get(key string) (Entry, bool) { /* ... */ }
```

Over this:

```go
package cache

type CacheStore struct{}

func (cacheStore *CacheStore) GetCacheEntry(cacheKey string) (Entry, bool) { /* ... */ }
```

## Interfaces

- Do not add interfaces only to make tests easier.
- Use small interfaces for behavior boundaries with multiple real implementations or consumers.
- Put interfaces at the consumer side when possible.
- Prefer direct testing of concrete types until an actual boundary requires indirection.

Prefer this:

```go
type Clock interface {
	Now() time.Time
}

type Scheduler struct {
	clock Clock
}
```

Only when a consumer actually needs to abstract time. Do not do this by default:

```go
type UserService interface {
	Create(context.Context, User) error
}

type userService struct{}
```

## Error Handling

- Return actionable errors with useful context.
- Wrap errors with `%w` when callers may inspect the cause.
- Do not wrap just to add noise. Add context that helps debugging.
- Prefer sentinel errors only when callers must branch on them.
- Use typed errors sparingly and only when the domain benefits from structured inspection.
- Never discard errors silently unless there is a deliberate best-effort path and the repo accepts it.

Prefer this:

```go
data, err := os.ReadFile(path)
if err != nil {
	return nil, fmt.Errorf("read config %q: %w", path, err)
}
```

Over this:

```go
data, err := os.ReadFile(path)
if err != nil {
	return nil, err
}
```

## Concurrency

- Keep concurrency explicit and justified.
- Do not introduce goroutines to look sophisticated.
- Use channels for coordination or ownership transfer, not as a replacement for plain function calls.
- Prefer `sync.Mutex`, `sync.WaitGroup`, `errgroup`, and straightforward worker patterns when they fit the problem.
- Pass `context.Context` through request, RPC, and I/O boundaries.
- Do not store `context.Context` in structs.
- If concurrency changes shared state or cancellation behavior, verify with `go test -race` when practical.

## Dependencies and Tooling

- Prefer standard packages such as `net/http`, `context`, `errors`, `slog`, `encoding/json`, and `testing` before external frameworks.
- Add dependencies only when they materially simplify the code or meet an established repo choice.
- Follow the repo's Go version and toolchain from `go.mod` or `go.work`.
- Match the repo's logging, config, and testing libraries instead of introducing parallel stacks.
