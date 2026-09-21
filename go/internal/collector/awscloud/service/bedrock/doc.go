// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package bedrock maps AWS Bedrock control-plane metadata into AWS cloud
// collector facts.
//
// The scanner emits metadata-only facts for the Bedrock control-plane surface:
// foundation model availability, custom models, model customization jobs,
// provisioned model throughputs, guardrails, agents, agent action groups, and
// knowledge bases. It also reports custom-model-to-base-model,
// custom-model-to-S3-output, custom-model-to-customization-job,
// provisioned-throughput-to-model, agent-to-foundation-model,
// agent-to-knowledge-base, agent-to-action-group, action-group-to-Lambda, and
// knowledge-base-to-S3/Confluence/SharePoint/web-crawler relationships when AWS
// reports ARN- or URL-shaped identities.
//
// Inference is outside the scanner contract. The bedrock-runtime module
// (InvokeModel, InvokeModelWithResponseStream, Converse, ConverseStream) and
// the bedrock-agent-runtime module (InvokeAgent, Retrieve, RetrieveAndGenerate)
// are never imported, so a model can never be invoked and a knowledge base can
// never be queried through this package. The scanner is also payload-blind by
// construction: agent instructions (system prompts), prompt-override
// configurations, guardrail topic and content policy bodies, knowledge base
// ingested document content, and action-group API schema bodies are never
// copied into scanner-owned types. SDK-call safety is enforced by the
// reflection guard in the awssdk adapter, which fails the build if a mutation
// or inference method reaches the adapter-local read interfaces, and by the
// struct-reflection guard in redaction_test.go, which proves no scanner type
// has a field that could carry a forbidden payload.
package bedrock
