# MCP Cookbook

Practical examples of MCP tool usage. Each entry shows the natural-language question, the tool to use, and the JSON arguments.

If you want shorter, role-based prompts before you drop into tool names and JSON payloads, start with [Starter Prompts](../guides/starter-prompts.md).

## Contents

- [Code topic investigation](#code-topic-investigation)
- [Finding code](#finding-code)
- [Call graph analysis](#call-graph-analysis)
- [Code quality](#code-quality)
- [Class hierarchy](#class-hierarchy)
- [Service dossier](#service-dossier)
- [Repository management](#repository-management)
- [Advanced Cypher queries](#advanced-cypher-queries)
- [Security analysis](#security-analysis)

---

## Service Dossier

### Explain a service in one call

> "Tell me what `payments-api` is, how it is deployed, what it depends on, and who consumes it."

**Tool:** `get_service_story`

```json
{ "workload_id": "payments-api" }
```

Read `service_identity`, `api_surface`, `deployment_lanes`,
`upstream_dependencies`, `downstream_consumers`, `evidence_graph`, and
`investigation` first. Use `build_evidence_citation_packet` when the answer
needs exact source, docs, manifest, or deployment proof behind returned file or
entity handles. Use `get_relationship_evidence` when the proof is a
`resolved_id` relationship pointer.

### Investigate coverage before answering

> "Scan the related repos, deployment sources, and indexed docs for `payments-api`, then tell me what you found."

**Tool:** `investigate_service`

```json
{
  "service_name": "payments-api",
  "intent": "onboarding"
}
```

Read `repositories_considered`, `repositories_with_evidence`,
`evidence_families_found`, `coverage_summary`, `investigation_findings`, and
`recommended_next_calls`. Use those next-call handles only when the final answer
needs a deeper proof point.

---

## Code Topic Investigation

### Find code paths for a behavior

> "Find the code paths responsible for repo sync authentication and explain how GitHub App auth is resolved."

**Tool:** `investigate_code_topic`

```json
{
  "topic": "repo sync authentication and GitHub App auth resolution",
  "repo_id": "eshu",
  "intent": "explain_auth_flow",
  "limit": 25
}
```

Read `evidence_groups`, `matched_symbols`, `coverage`, and
`recommended_next_calls` before answering. Use `get_file_lines` or
`get_code_relationship_story` only for the exact files or symbols returned in
the first packet.

### Find all code involved in a subsystem

> "Find all code involved in clone, fetch, default-branch resolution, and workspace locking."

**Tool:** `investigate_code_topic`

```json
{
  "topic": "clone fetch default branch resolution workspace locking",
  "repo_id": "eshu",
  "intent": "implementation_map",
  "limit": 50
}
```

Use `offset` when `truncated` is true. If the response is empty or ambiguous,
read `coverage.searched_terms` and `recommended_next_calls`; do not fall back to
raw Cypher or repeat broad content searches without narrowing the topic.

---

## Finding Code

### Find a function by name

> "Where is the function `foo` defined?"

**Tool:** `find_symbol`

```json
{ "symbol": "foo", "match_mode": "exact", "limit": 25 }
```

### Find all imports of a module

> "Where is the `math` module imported?"

**Tool:** `get_code_relationship_story`

```json
{ "target": "math", "relationship_type": "IMPORTS", "direction": "incoming", "limit": 25 }
```

### Find functions with a decorator

> "Find all functions with the `log_decorator`."

**Tool:** `analyze_code_relationships`

```json
{ "query_type": "find_functions_by_decorator", "target": "log_decorator" }
```

### Find functions by argument name

> "Find all functions that take `self` as an argument."

**Tool:** `analyze_code_relationships`

```json
{ "query_type": "find_functions_by_argument", "target": "self" }
```

### Find all dataclasses

> "Find all dataclasses."

**Tool:** `execute_cypher_query`

```json
{ "cypher_query": "MATCH (c:Class) WHERE 'dataclass' IN c.decorators RETURN c.name, c.path", "limit": 50 }
```

---

## Call Graph Analysis

### Find all callers of a function

> "Find all calls to the `helper` function."

**Tool:** `get_code_relationship_story`

```json
{ "target": "helper", "relationship_type": "CALLS", "direction": "incoming", "limit": 25 }
```

This maps to the Go `code/relationships/story` route. The route resolves
`helper` first, returns bounded ambiguity candidates if more than one entity
matches, and only queries the graph after it has an entity anchor.

### Find what a function calls

> "What functions are called inside `foo`?"

**Tool:** `get_code_relationship_story`

```json
{ "target": "foo", "relationship_type": "CALLS", "direction": "outgoing", "limit": 25 }
```

Use `offset` to page beyond the first bounded relationship window.

### Find indirect callers

> "Show me all functions that eventually call `helper`."

**Tool:** `get_code_relationship_story`

```json
{ "target": "helper", "relationship_type": "CALLS", "direction": "incoming", "include_transitive": true, "max_depth": 7, "limit": 50 }
```

Transitive story reads are bounded breadth-first traversals. They stop at
`max_depth` or when the requested `limit` window is full. Traversal mode does
not support nonzero `offset`; narrow the target or lower `max_depth` before
asking for another page.

### Find indirect callees

> "Show me all functions eventually called by `foo`."

**Tool:** `get_code_relationship_story`

```json
{ "target": "foo", "relationship_type": "CALLS", "direction": "outgoing", "include_transitive": true, "max_depth": 7, "limit": 50 }
```

Pass `entity_id` instead of `target` when an earlier symbol lookup already
selected the exact function.

### Find the call chain between two functions

> "What is the call chain from `wrapper` to `helper`?"

**Tool:** `find_function_call_chain`

```json
{ "start": "wrapper", "end": "helper", "max_depth": 5 }
```

`analyze_code_relationships` also accepts `{"query_type":"call_chain","target":"wrapper->helper"}` for compatibility, but the dedicated tool is the canonical public contract.

### Find cross-module calls

> "Find functions in `module_a.py` that call `helper` in `module_b.py`."

**Tool:** `execute_cypher_query`

```json
{
  "cypher_query": "MATCH (caller:Function)-[:CALLS]->(callee:Function {name: 'helper'}) WHERE caller.path ENDS WITH 'module_a.py' AND callee.path ENDS WITH 'module_b.py' RETURN caller.name",
  "limit": 50
}
```

### Find recursive functions

> "Find all functions that call themselves."

**Tool:** `execute_cypher_query`

```json
{
  "cypher_query": "MATCH (f:Function)-[:CALLS]->(f2:Function) WHERE f.name = f2.name AND f.path = f2.path RETURN f.name, f.path",
  "limit": 50
}
```

### Find hub functions (most connected)

> "Find the functions that are most central to the codebase."

**Tool:** `execute_cypher_query`

```json
{
  "cypher_query": "MATCH (f:Function) OPTIONAL MATCH (f)-[:CALLS]->(callee:Function) OPTIONAL MATCH (caller:Function)-[:CALLS]->(f) WITH f, count(DISTINCT callee) AS calls_out, count(DISTINCT caller) AS calls_in ORDER BY (calls_out + calls_in) DESC LIMIT 5 RETURN f.name, f.path, calls_out, calls_in",
  "limit": 5
}
```

---

## Code Quality

### Find the most complex functions

> "Find the 5 most complex functions."

**Tool:** `inspect_code_quality`

```json
{
  "check": "complexity",
  "repo_id": "payments-service",
  "limit": 5
}
```

### Calculate complexity of a specific function

> "What is the cyclomatic complexity of `try_except_finally`?"

**Tool:** `calculate_cyclomatic_complexity`

```json
{ "function_name": "try_except_finally" }
```

### Find dead code

> "Find unused code, but ignore API endpoints."

**Tool:** `investigate_dead_code`

```json
{
  "repo_id": "payments",
  "limit": 200,
  "offset": 0,
  "exclude_decorated_with": ["@app.route"]
}
```

This returns a prompt-ready dead-code investigation packet. It still uses the
same bounded dead-code candidate scan and root policy as `find_dead_code`, but
the response groups results into `cleanup_ready`, `ambiguous`, and
`suppressed` buckets, includes repository coverage/freshness and language
maturity, and returns exact source handles plus recommended next calls. The
`repo_id` argument may be a canonical repository ID, repository name, repo slug,
or indexed path; the server resolves it before querying. JavaScript and
TypeScript candidates stay in `ambiguous` until corpus precision is proven, so
callers do not treat known false-positive-prone results as cleanup-safe.

### Find dead code with the lower-level candidate scan

Use `find_dead_code` when you need the raw derived candidate list instead of
the investigation packet:

```json
{ "repo_id": "payments", "language": "go", "limit": 100 }
```

### Find dead code (diagnostic Cypher only)

> "Find functions that are never called."

**Tool:** `execute_cypher_query`

```json
{
  "cypher_query": "MATCH (f:Function) WHERE NOT (()-[:CALLS]->(f)) AND f.is_dependency = false RETURN f.name, f.path",
  "limit": 100
}
```

### Find large functions

> "Find functions with more than 20 lines that might need refactoring."

**Tool:** `inspect_code_quality`

```json
{
  "check": "function_length",
  "repo_id": "payments-service",
  "min_lines": 20,
  "limit": 25
}
```

### Find functions with many arguments

> "Find all functions with more than 5 arguments."

**Tool:** `inspect_code_quality`

```json
{
  "check": "argument_count",
  "repo_id": "payments-service",
  "min_arguments": 5,
  "limit": 25
}
```

---

## Class Hierarchy

### Find class methods

> "What are the methods of class `A`?"

**Tool:** `analyze_code_relationships`

```json
{ "query_type": "class_hierarchy", "target": "A", "repo_id": "payments", "limit": 25 }
```

The response includes `class_hierarchy.methods`, direct parents, direct
children, source handles, and bounded depth metadata.

### Find subclasses

> "Show me all classes that inherit from `Base`."

**Tool:** `analyze_code_relationships`

```json
{ "query_type": "class_hierarchy", "target": "Base", "repo_id": "payments", "limit": 25 }
```

Read `class_hierarchy.children` for direct subclasses. If the response is
truncated, page or narrow by repository/language before asking for source
evidence.

### Find method overrides

> "Find all overridden methods."

**Tool:** `analyze_code_relationships`

```json
{ "query_type": "overrides", "repo_id": "payments", "limit": 25 }
```

Use `target` when you want overrides for one method. Omit `target` only when
you want the bounded repo-scoped override list.

### Find inheritance depth

> "How deep are the inheritance chains?"

**Tool:** `analyze_code_relationships`

```json
{ "query_type": "class_hierarchy", "target": "Base", "repo_id": "payments", "max_depth": 5, "limit": 25 }
```

Read `class_hierarchy.depth_summary.max_parent_depth` and
`class_hierarchy.depth_summary.max_child_depth`. Use the returned handles for
follow-up source reads.

### Find overriding methods

> "Find all methods that override a parent class method."

**Tool:** `analyze_code_relationships`

```json
{ "query_type": "overrides", "repo_id": "payments", "limit": 25 }
```

Raw Cypher is diagnostics-only for this prompt family. Normal MCP callers
should use the first-class story response so the answer is scoped, paged, and
ambiguity-aware.

---

## Repository Management

### List indexed projects

> "List all projects I have indexed."

**Tool:** `list_indexed_repositories`

```json
{}
```

### Explain a relationship evidence pointer

> "Why does this deployment edge exist?"

**Tool:** `get_relationship_evidence`

```json
{ "resolved_id": "resolved_abc123" }
```

### Check job status

> "What is the status of job `4cb9a60e-...`?"

**Tool:** `check_job_status`

```json
{ "job_id": "4cb9a60e-c1b1-43a7-9c94-c840771506bc" }
```

### List background jobs

> "Show me all background jobs."

**Tool:** `list_jobs`

```json
{}
```

---

## Advanced Cypher Queries

`execute_cypher_query` is a diagnostics-only fallback. Prefer the named MCP
tools above when they answer the question. When you do use raw Cypher, include a
small `limit`; the server also appends a bounded `LIMIT` when the query omits
one and returns `truncated` when the row window clips the result.

### Find all function definitions

```json
{ "cypher_query": "MATCH (n:Function) RETURN n.name, n.path, n.line_number LIMIT 50", "limit": 50 }
```

### Find all classes

```json
{ "cypher_query": "MATCH (n:Class) RETURN n.name, n.path, n.line_number LIMIT 50", "limit": 50 }
```

### Find functions in a specific file

```json
{ "cypher_query": "MATCH (f:Function) WHERE f.path ENDS WITH 'module_a.py' RETURN f.name", "limit": 50 }
```

### Find top-level elements in a file

```json
{ "cypher_query": "MATCH (f:File)-[:CONTAINS]->(n) WHERE f.name = 'module_a.py' AND (n:Function OR n:Class) AND n.context IS NULL RETURN n.name", "limit": 50 }
```

### Find circular file imports

```json
{ "cypher_query": "MATCH (f1:File)-[:IMPORTS]->(m2:Module), (f2:File)-[:IMPORTS]->(m1:Module) WHERE f1.name = m1.name + '.py' AND f2.name = m2.name + '.py' RETURN f1.name, f2.name", "limit": 50 }
```

### Find documented functions

```json
{ "cypher_query": "MATCH (f:Function) WHERE f.docstring IS NOT NULL AND f.docstring <> '' RETURN f.name, f.path LIMIT 50", "limit": 50 }
```

### Find decorated methods in a class

```json
{ "cypher_query": "MATCH (c:Class {name: 'Child'})-[:CONTAINS]->(m:Function) WHERE m.decorators IS NOT NULL AND size(m.decorators) > 0 RETURN m.name", "limit": 50 }
```

### Count functions per file

```json
{ "cypher_query": "MATCH (f:Function) RETURN f.path, count(f) AS function_count ORDER BY function_count DESC", "limit": 50 }
```

### Find classes with a specific method

```json
{ "cypher_query": "MATCH (c:Class)-[:CONTAINS]->(m:Function {name: 'greet'}) RETURN c.name, c.path", "limit": 50 }
```

### Find `super()` calls

```json
{ "cypher_query": "MATCH (f:Function)-[r:CALLS]->() WHERE r.full_call_name STARTS WITH 'super(' RETURN f.name, f.path", "limit": 50 }
```

### Find modules imported by a file

```json
{ "cypher_query": "MATCH (f:File {name: 'module_a.py'})-[:IMPORTS]->(m:Module) RETURN m.name AS imported_module_name", "limit": 50 }
```

### Find all Python package imports

```json
{ "cypher_query": "MATCH (f:File)-[:IMPORTS]->(m:Module) WHERE f.path ENDS WITH '.py' RETURN DISTINCT m.name", "limit": 100 }
```

---

## Security Analysis

### Find potential hardcoded secrets

> "Find potential hardcoded passwords, API keys, or secrets."

**Tool:** `investigate_hardcoded_secrets`

```json
{ "repo_id": "payments-service", "limit": 25, "include_suppressed": false }
```

This scans indexed content, returns redacted excerpts only, reports finding
kind, confidence, severity, suppression notes, source handles, and
`truncated`. Use `include_suppressed=true` only when you want test, fixture,
example, or placeholder candidates included with suppression metadata.
