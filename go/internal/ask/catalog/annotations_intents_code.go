// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

// codeToolIntents curates router intents for the code lookup, source reading,
// code relationship, dead code, code quality, code divergence, data-flow, and
// graph-query tools. Families are named in annotations_intents_families.go.
func codeToolIntents() map[string]toolIntent {
	return map[string]toolIntent{
		// code-lookup
		"find_symbol":            canonicalIntents("where is this symbol defined", "find function definition", "find class definition", "locate symbol by name", "fuzzy symbol search", "go to definition"),
		"find_code":              intents("find code by name", "search entities by case-sensitive name", "substring entity name search", "exact entity name lookup", "page through name matches"),
		"resolve_entity":         intents("resolve entity name to id", "exact entity name resolution", "get entity id for name", "disambiguate entity"),
		"execute_language_query": intents("find functions in language", "list classes written in go", "language specific entity query", "find structs by language", "query by programming language"),
		"search_entity_content":  intents("search function source snippets", "grep entity source", "find code containing text", "search cached entity source", "text search in function bodies"),
		"search_file_content":    intents("search file text", "grep repositories for string", "find files containing text", "substring search across files", "full text search source files"),
		"inspect_code_inventory": intents("list functions in file", "list classes in repository", "decorated methods", "documented functions", "dataclasses in repo", "code structure inventory"),
		"investigate_code_topic": intents("where is this behavior implemented", "find code for a topic", "explore feature implementation", "which files handle this", "broad code topic search"),
		// source-read
		"get_file_content":      canonicalIntents("read file source", "show file contents", "get source of file", "open file in repository", "read repo file by path"),
		"get_file_lines":        intents("read lines of file", "show line range", "get lines 10 to 40", "excerpt of file", "view part of a file"),
		"get_entity_content":    intents("read function body", "get entity source", "full docstring of entity", "source for entity id", "show code of symbol"),
		"list_repository_files": intents("list files in repository", "directory tree of repo", "browse repository files", "files by language in repo", "repo file listing"),
		// code-relationships
		"get_code_relationship_story":     canonicalIntents("who calls this function", "what does this function call", "callers and callees of symbol", "who imports this", "relationship story for symbol", "transitive callers"),
		"analyze_code_relationships":      intents("class hierarchy", "who overrides this method", "who modifies this variable", "callers of function by query type", "find importers", "cross repo callers", "module dependencies"),
		"find_function_call_chain":        intents("call chain between two functions", "path from function a to function b", "how does a reach b", "shortest call path", "transitive call chain"),
		"compare_code_paths":              intents("compare call paths between functions", "alternate call paths", "distinct paths from a to b", "weakest edge on call path"),
		"investigate_import_dependencies": intents("which modules import this module", "import cycles", "package import cycle", "imports by file", "importers of package", "cross module calls"),
		"inspect_call_graph_metrics":      intents("recursive functions", "hub functions", "most connected functions", "highly called functions", "call graph metrics"),
		"trace_route_callers":             intents("who calls this http route handler", "route handler callers", "workloads impacted by route", "framework route to callers"),
		"trace_exposure_path":             intents("internet exposed handler reaches secret", "exposure path to cloud sink", "reachability to privileged iam action", "handler to sql table path", "attack surface reachability"),
		// dead-code
		"investigate_dead_code":     canonicalIntents("what code looks dead", "unused functions with coverage", "dead code candidates", "unreferenced code with exactness blockers", "find dead code safely"),
		"find_dead_code":            intents("find unused functions", "potentially unused code", "dead functions in repository", "exclude decorated functions from dead code"),
		"find_cross_repo_dead_code": intents("dead code across repositories", "producer repo unused symbols", "unused exports kept live by consumers", "cross repo unused code"),
		"find_dead_iac":             intents("unused terraform modules", "dead helm charts", "unused kustomize paths", "unused ansible roles", "dead docker compose services"),
		// code-quality
		"inspect_code_quality":            canonicalIntents("code quality metrics", "refactoring candidates", "long functions", "functions with many arguments", "complexity and length hotspots"),
		"find_most_complex_functions":     intents("most complex functions", "highest cyclomatic complexity", "complexity hotspots in codebase", "top complex functions"),
		"calculate_cyclomatic_complexity": intents("cyclomatic complexity of function", "how complex is this function", "complexity score for one function"),
		// code-divergence
		"report_code_divergence":      canonicalIntents("duplicate code rollup", "parallel implementations summary", "code divergence counts by kind", "copy paste drift overview", "how much duplicated code"),
		"find_code_divergence":        intents("find duplicate functions", "identical token streams", "near duplicate implementations", "wrapper bypass", "missing call in cohort", "convention outliers"),
		"investigate_code_divergence": intents("drill into divergence finding", "divergence finding by fingerprint", "follow up on duplicate pair", "members of duplicate cluster"),
		// data-flow
		"dispatch_taint_path":   intents("taint path evidence", "tainted data flow", "source to sink taint", "taint analysis for repository"),
		"dispatch_reaching_def": intents("reaching definitions", "def use chains", "where is variable defined", "data flow definitions"),
		"dispatch_cfg_summary":  intents("control flow graph summary", "cfg for function", "branches and loops summary", "control flow of repository"),
		"dispatch_pdg_summary":  intents("program dependence graph summary", "pdg for function", "control and data dependence", "program dependence summary"),
		// graph-query
		"execute_cypher_query":          intents("run cypher query", "raw graph query", "custom read only cypher", "diagnostic graph query", "query code graph directly"),
		"visualize_graph_query":         intents("visualize graph", "render cypher result as graph", "draw nodes and edges", "graph visualization packet"),
		"investigate_hardcoded_secrets": intents("hardcoded secrets", "api keys in code", "committed passwords", "private keys in repository", "risky literals", "leaked tokens in source"),
	}
}
