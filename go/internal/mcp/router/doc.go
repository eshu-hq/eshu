// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package router serves the opt-in MCP router surface: the tool-surface
// selector, the find_tool and call_tool definitions, the Ranker and Dispatcher
// seams, and the Engine that answers both tools.
//
// Engine.Find validates a find_tool request, filters the registry per call by
// the query profile and the caller's AuthContext (classified with the query
// package's route ledgers), asks the injected Ranker for candidates, and
// attaches a matching query-playbook plan. Engine.Call refuses find_tool,
// call_tool, and ask as targets, suggests close names the caller could be
// offered for an unknown one, refuses a target the credential can never call,
// validates arguments with ValidateArguments, and otherwise runs the injected
// Dispatcher with the caller's arguments untouched. ValidateArguments is an in-repo JSON-schema subset
// covering every keyword the registered tools use; UnsupportedSchemaFeatures
// reports any it does not.
//
// The parent mcp package embeds a Host, injects a Dispatcher and a
// RouteResolver, and sends every name Intercepts accepts to Host.Serve before
// dispatchTool, so this package must never import the mcp root.
package router
