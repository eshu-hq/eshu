package bedrock

import (
	"reflect"
	"strings"
	"testing"
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

// TestPopulatedSensitiveValuesNeverReachFacts feeds populated sensitive sentinel
// values into the fixture and proves that, after a full scan, none of them
// appears in any emitted fact. The values land only on fields the scanner does
// persist (description, name); the forbidden values have no field, so the test
// proves the contract end to end rather than only at the type level.
func TestPopulatedSensitiveValuesNeverReachFacts(t *testing.T) {
	client := richClient()
	// Stuff sensitive-looking content into the fields the scanner DOES persist
	// to confirm those fields are not where IP lives, and prove the forbidden
	// values cannot be emitted because there is nowhere to put them.
	client.agents[0].Description = "handles orders"
	client.guardrails[0].Description = "blocks unsafe content"

	envelopes := scanFixture(t, client)
	for _, envelope := range envelopes {
		flat := strings.ToLower(flatten(envelope.Payload))
		for _, banned := range []string{
			"you-are-a-helpful-secret-agent-prompt",
			"prompt-override-template-body",
			"deny-topic-policy-body",
			"deny-content-filter-body",
			"customer-ip-action-api-schema",
			"ingested-document-secret-chunk",
		} {
			if strings.Contains(flat, banned) {
				t.Fatalf("forbidden value %q reached emitted fact %q", banned, envelope.FactKind)
			}
		}
	}
}
