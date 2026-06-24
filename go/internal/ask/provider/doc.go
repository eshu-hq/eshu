// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package provider defines tool-calling adapters over semantic profiles.
//
// The package serves two provider families:
//   - Anthropic tool-use: Claude models with native tool_use block responses.
//   - OpenAI-compatible: GPT-4 and others with native function_calls responses.
//
// The Adapter interface bridges both families with a provider-neutral contract:
// messages as a flat sequence (Role, Text, ToolCallID, ToolName), tools as
// a flat list with name, description, and JSON schema.
//
// Core responsibility: never leak prompts, provider response bodies, or
// credentials in errors or logs. Adapters redact at ingestion.
package provider
