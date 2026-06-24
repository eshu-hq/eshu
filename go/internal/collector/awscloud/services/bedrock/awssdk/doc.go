// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts AWS SDK for Go v2 Bedrock control-plane calls into
// scanner-owned metadata.
//
// The adapter calls only List, Get, and ListTagsForResource reads on the
// bedrock and bedrock-agent control-plane clients. It never calls any mutation
// operation and never calls any inference operation. The inference operations
// live in the separate bedrockruntime module (InvokeModel,
// InvokeModelWithResponseStream, Converse, ConverseStream) and the
// bedrockagentruntime module (InvokeAgent, Retrieve, RetrieveAndGenerate),
// neither of which this package imports. The adapter never copies agent
// instructions (Agent.Instruction), prompt-override configurations
// (PromptOverrideConfiguration), guardrail topic or content policy bodies,
// knowledge base ingested document content, or action-group API schema bodies
// into scanner-owned types. The reflection gate in exclusion_test.go fails the
// build if a forbidden method ever reaches the adapter-local read interfaces.
package awssdk
