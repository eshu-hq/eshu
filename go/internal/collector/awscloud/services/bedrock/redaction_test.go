// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package bedrock

import (
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestScannerTypesHaveNoForbiddenPayloadField is the high-redaction acceptance
// gate the issue calls out. The Bedrock scanner is payload-blind by
// construction: the sensitive surfaces (agent instructions / system prompts,
// prompt-override configurations, guardrail topic and content policy bodies,
// knowledge base ingested document content, and action-group API schema bodies)
// must have no field on any scanner-owned type to land in. We reflect over the
// fields of every scanner-owned struct and fail the build if a field name ever
// matches a forbidden-payload token. A future edit that adds, for example, an
// Agent.Instruction field is caught here before it can carry IP into a fact.
func TestScannerTypesHaveNoForbiddenPayloadField(t *testing.T) {
	forbiddenTokens := []string{
		"instruction",    // Agent.Instruction (system prompt)
		"prompt",         // PromptOverrideConfiguration / prompt templates
		"policy",         // guardrail topic/content policy bodies
		"topicpolicy",    // guardrail topic policy
		"contentpolicy",  // guardrail content policy
		"apischema",      // action-group API schema body
		"functionschema", // action-group function schema body
		"schemabody",     // any inline schema body
		"document",       // knowledge base ingested document content
		"chunk",          // knowledge base ingested chunks
		"content",        // ingested content
		"hyperparameter", // custom-model hyperparameter values
		"trainingdata",   // custom-model training input data
		"inputdata",      // training/customization input data
	}

	scannerTypes := []reflect.Type{
		reflect.TypeOf(FoundationModel{}),
		reflect.TypeOf(CustomModel{}),
		reflect.TypeOf(ModelCustomizationJob{}),
		reflect.TypeOf(ProvisionedModelThroughput{}),
		reflect.TypeOf(Guardrail{}),
		reflect.TypeOf(Agent{}),
		reflect.TypeOf(AgentActionGroup{}),
		reflect.TypeOf(KnowledgeBase{}),
		reflect.TypeOf(KnowledgeBaseDataSource{}),
	}

	for _, scannerType := range scannerTypes {
		for i := 0; i < scannerType.NumField(); i++ {
			name := strings.ToLower(scannerType.Field(i).Name)
			for _, token := range forbiddenTokens {
				if strings.Contains(name, token) {
					t.Fatalf("scanner type %s has field %q matching forbidden payload token %q; the high-redaction contract forbids a field that could carry agent prompts, guardrail policies, KB content, action-group schemas, or training data",
						scannerType.Name(), scannerType.Field(i).Name, token)
				}
			}
		}
	}
}

// TestAgentAndGuardrailEmitNoForbiddenAttributeKeys is the structural redaction
// gate for the two highest-IP resources at the scanner-owned fixture layer. The
// scanner-owned Agent and Guardrail types deliberately have no field for the
// agent instruction/prompt-override or the guardrail topic/content policy
// bodies, so a value-substring scan over this fixture would be vacuous: the
// sentinel values are never present in the inputs and therefore prove nothing.
// This gate is instead assertive against a real regression class: it fails if
// any emitted agent or guardrail attribute key ever names one of those IP
// surfaces, which is what a future edit that started persisting prompts or
// policy bodies would produce.
//
// The genuinely end-to-end proof — populated SDK payloads (Instruction,
// PromptOverrideConfiguration, ApiSchema, FunctionSchema, HyperParameters,
// TrainingDataConfig) injected at the SDK layer the adapter reads, asserted
// absent from emitted facts — lives in the awssdk package as
// TestScannerNeverEmitsSDKSentinelsEndToEnd, because only the SDK output types
// have a field to carry those values in the first place.
func TestAgentAndGuardrailEmitNoForbiddenAttributeKeys(t *testing.T) {
	client := richClient()
	// Populate the descriptions the scanner DOES persist so the gate runs
	// against a realistic, fully-populated agent and guardrail.
	client.agents[0].Description = "handles orders"
	client.guardrails[0].Description = "blocks unsafe content"

	forbiddenKeyTokens := []string{
		"instruction", // agent system prompt
		"prompt",      // prompt-override template body
		"policy",      // guardrail topic/content policy body
		"topic",       // guardrail topic policy
		"filter",      // guardrail content filter body
	}

	envelopes := scanFixture(t, client)
	agent := resourceByType(t, envelopes, awscloud.ResourceTypeBedrockAgent)
	guardrail := resourceByType(t, envelopes, awscloud.ResourceTypeBedrockGuardrail)
	for _, envelope := range []facts.Envelope{agent, guardrail} {
		attributes, ok := envelope.Payload["attributes"].(map[string]any)
		if !ok {
			t.Fatalf("resource %v has no attributes map", envelope.Payload["resource_type"])
		}
		for key := range attributes {
			lower := strings.ToLower(key)
			for _, token := range forbiddenKeyTokens {
				if strings.Contains(lower, token) {
					t.Fatalf("resource %v emitted forbidden attribute key %q (token %q); agent prompts and guardrail policy bodies are IP and must not be persisted",
						envelope.Payload["resource_type"], key, token)
				}
			}
		}
	}
}
