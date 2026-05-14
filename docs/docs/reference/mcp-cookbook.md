# MCP Cookbook

Practical examples of MCP tool usage. Each entry shows the natural-language question, the tool to use, and the JSON arguments.

If you want shorter, role-based prompts before you drop into tool names and JSON payloads, start with [Starter Prompts](../guides/starter-prompts.md).

## Contents

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
`upstream_dependencies`, `downstream_consumers`, and `evidence_graph` first.
Only call `get_relationship_evidence` or content tools when the answer needs
the exact source behind a `resolved_id` or file handle.

---

## Finding Code

### Find a function by name

> "Where is the function `foo` defined?"

**Tool:** `find_code`

```json
{ "query": "foo" }
```

### Find all imports of a module

> "Where is the `math` module imported?"

**Tool:** `analyze_code_relationships`

```json
{ "query_type": "find_importers", "target": "math" }
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
{ "cypher_query": "MATCH (c:Class) WHERE 'dataclass' IN c.decorators RETURN c.name, c.path" }
```

---

## Call Graph Analysis

### Find all callers of a function

> "Find all calls to the `helper` function."

**Tool:** `analyze_code_relationships`

```json
{ "query_type": "find_callers", "target": "helper" }
```

This now maps to the Go `code/relationships` route using `name=helper`,
`direction=incoming`, and `relationship_type=CALLS`.

### Find what a function calls

> "What functions are called inside `foo`?"

**Tool:** `analyze_code_relationships`

```json
{ "query_type": "find_callees", "target": "foo" }
```

This now maps to the Go `code/relationships` route using `name=foo`,
`direction=outgoing`, and `relationship_type=CALLS`.

### Find indirect callers

> "Show me all functions that eventually call `helper`."

**Tool:** `analyze_code_relationships`

```json
{ "query_type": "find_all_callers", "target": "helper", "max_depth": 7 }
```

This now maps to the Go `code/relationships` route using `name=helper`,
`direction=incoming`, `relationship_type=CALLS`, `transitive=true`, and the
provided `max_depth`.

### Find indirect callees

> "Show me all functions eventually called by `foo`."

**Tool:** `analyze_code_relationships`

```json
{ "query_type": "find_all_callees", "target": "foo", "max_depth": 7 }
```

This maps to the same route with `direction=outgoing`,
`relationship_type=CALLS`, `transitive=true`, and the provided `max_depth`.

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
  "cypher_query": "MATCH (caller:Function)-[:CALLS]->(callee:Function {name: 'helper'}) WHERE caller.path ENDS WITH 'module_a.py' AND callee.path ENDS WITH 'module_b.py' RETURN caller.name"
}
```

### Find recursive functions

> "Find all functions that call themselves."

**Tool:** `execute_cypher_query`

```json
{
  "cypher_query": "MATCH (f:Function)-[:CALLS]->(f2:Function) WHERE f.name = f2.name AND f.path = f2.path RETURN f.name, f.path"
}
```

### Find hub functions (most connected)

> "Find the functions that are most central to the codebase."

**Tool:** `execute_cypher_query`

```json
{
  "cypher_query": "MATCH (f:Function) OPTIONAL MATCH (f)-[:CALLS]->(callee:Function) OPTIONAL MATCH (caller:Function)-[:CALLS]->(f) WITH f, count(DISTINCT callee) AS calls_out, count(DISTINCT caller) AS calls_in ORDER BY (calls_out + calls_in) DESC LIMIT 5 RETURN f.name, f.path, calls_out, calls_in"
}
```

---

## Code Quality

### Find the most complex functions

> "Find the 5 most complex functions."

**Tool:** `find_most_complex_functions`

```json
{ "limit": 5 }
```

### Calculate complexity of a specific function

> "What is the cyclomatic complexity of `try_except_finally`?"

**Tool:** `calculate_cyclomatic_complexity`

```json
{ "function_name": "try_except_finally" }
```

### Find dead code

> "Find unused code, but ignore API endpoints."

**Tool:** `find_dead_code`

```json
{ "repo_id": "payments", "limit": 200, "exclude_decorated_with": ["@app.route"] }
```

This returns derived dead-code candidates today: the handler starts from the
graph candidate set, applies the current default entrypoint/test/generated
exclusions plus direct Go Cobra, stdlib HTTP, controller-runtime signature
roots and Go exported public-package roots, and reports its modeled root
categories in the response envelope's `data.analysis` field. The `repo_id`
argument may be a canonical repository ID, repository name, repo slug, or
indexed path; the server resolves it before querying. The response also
includes `data.truncated` when the bounded dead-code result window cut off
additional candidates and `data.analysis.roots_skipped_missing_source` when Go
framework-root checks could not run because entity source text was unavailable.
The same `data.analysis` object now reports
`framework_roots_from_parser_metadata` versus
`framework_roots_from_source_fallback` so local validation can tell whether the
reindex-backed metadata path is taking over from the legacy query-time
heuristic path.

### Find dead code (Cypher)

> "Find functions that are never called."

**Tool:** `execute_cypher_query`

```json
{
  "cypher_query": "MATCH (f:Function) WHERE NOT (()-[:CALLS]->(f)) AND f.is_dependency = false RETURN f.name, f.path"
}
```

### Find large functions

> "Find functions with more than 20 lines that might need refactoring."

**Tool:** `execute_cypher_query`

```json
{
  "cypher_query": "MATCH (f:Function) WHERE f.end_line - f.line_number > 20 RETURN f.name, f.path, (f.end_line - f.line_number) AS lines"
}
```

### Find functions with many arguments

> "Find all functions with more than 5 arguments."

**Tool:** `execute_cypher_query`

```json
{
  "cypher_query": "MATCH (f:Function) WHERE size(f.args) > 5 RETURN f.name, f.path, size(f.args) AS arg_count"
}
```

---

## Class Hierarchy

### Find class methods

> "What are the methods of class `A`?"

**Tool:** `analyze_code_relationships`

```json
{ "query_type": "class_hierarchy", "target": "A" }
```

The response includes a list of methods and child classes.

### Find subclasses

> "Show me all classes that inherit from `Base`."

**Tool:** `analyze_code_relationships`

```json
{ "query_type": "class_hierarchy", "target": "Base" }
```

### Find method overrides

> "Find all overridden methods."

**Tool:** `analyze_code_relationships`

```json
{ "query_type": "overrides", "target": "foo" }
```

### Find inheritance depth

> "How deep are the inheritance chains?"

**Tool:** `execute_cypher_query`

```json
{
  "cypher_query": "MATCH (c:Class) OPTIONAL MATCH path = (c)-[:INHERITS*]->(parent:Class) RETURN c.name, c.path, length(path) AS depth ORDER BY depth DESC"
}
```

### Find overriding methods (Cypher)

> "Find all methods that override a parent class method."

**Tool:** `execute_cypher_query`

```json
{
  "cypher_query": "MATCH (c:Class)-[:INHERITS]->(p:Class), (c)-[:CONTAINS]->(m:Function), (p)-[:CONTAINS]->(m_parent:Function) WHERE m.name = m_parent.name RETURN m.name as method, c.name as child_class, p.name as parent_class"
}
```

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

### Find all function definitions

```json
{ "cypher_query": "MATCH (n:Function) RETURN n.name, n.path, n.line_number LIMIT 50" }
```

### Find all classes

```json
{ "cypher_query": "MATCH (n:Class) RETURN n.name, n.path, n.line_number LIMIT 50" }
```

### Find functions in a specific file

```json
{ "cypher_query": "MATCH (f:Function) WHERE f.path ENDS WITH 'module_a.py' RETURN f.name" }
```

### Find top-level elements in a file

```json
{ "cypher_query": "MATCH (f:File)-[:CONTAINS]->(n) WHERE f.name = 'module_a.py' AND (n:Function OR n:Class) AND n.context IS NULL RETURN n.name" }
```

### Find circular file imports

```json
{ "cypher_query": "MATCH (f1:File)-[:IMPORTS]->(m2:Module), (f2:File)-[:IMPORTS]->(m1:Module) WHERE f1.name = m1.name + '.py' AND f2.name = m2.name + '.py' RETURN f1.name, f2.name" }
```

### Find documented functions

```json
{ "cypher_query": "MATCH (f:Function) WHERE f.docstring IS NOT NULL AND f.docstring <> '' RETURN f.name, f.path LIMIT 50" }
```

### Find decorated methods in a class

```json
{ "cypher_query": "MATCH (c:Class {name: 'Child'})-[:CONTAINS]->(m:Function) WHERE m.decorators IS NOT NULL AND size(m.decorators) > 0 RETURN m.name" }
```

### Count functions per file

```json
{ "cypher_query": "MATCH (f:Function) RETURN f.path, count(f) AS function_count ORDER BY function_count DESC" }
```

### Find classes with a specific method

```json
{ "cypher_query": "MATCH (c:Class)-[:CONTAINS]->(m:Function {name: 'greet'}) RETURN c.name, c.path" }
```

### Find `super()` calls

```json
{ "cypher_query": "MATCH (f:Function)-[r:CALLS]->() WHERE r.full_call_name STARTS WITH 'super(' RETURN f.name, f.path" }
```

### Find modules imported by a file

```json
{ "cypher_query": "MATCH (f:File {name: 'module_a.py'})-[:IMPORTS]->(m:Module) RETURN m.name AS imported_module_name" }
```

### Find all Python package imports

```json
{ "cypher_query": "MATCH (f:File)-[:IMPORTS]->(m:Module) WHERE f.path ENDS WITH '.py' RETURN DISTINCT m.name" }
```

---

## Security Analysis

### Find potential hardcoded secrets

> "Find potential hardcoded passwords, API keys, or secrets."

**Tool:** `execute_cypher_query`

```json
{
  "cypher_query": "WITH ['password', 'api_key', 'apikey', 'secret_token', 'token', 'auth', 'access_key', 'private_key', 'client_secret', 'sessionid', 'jwt'] AS keywords MATCH (f:Function) WHERE ANY(word IN keywords WHERE toLower(f.source_code) CONTAINS word) RETURN f.name, f.path"
}
```
