// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// invokesCloudActionEvidenceSource labels INVOKES_CLOUD_ACTION edges so the
// retract-before-write contract only ever touches edges this emitter owns.
const invokesCloudActionEvidenceSource = "parser/aws-sdk-call"

// cloudActionIDPrefix namespaces the CloudAction node id so the graph id can
// never collide with another node identity scheme.
const cloudActionIDPrefix = "cloud-action:"

// cloudActionServiceMethod keys the SDK-call-to-action mapping table by the
// parser-provided AWS SDK service token and the exact AWS SDK v2 Go method name
// observed at the call site.
type cloudActionServiceMethod struct {
	// Service is the lowercase AWS SDK service token the Go parser records under
	// receiver_sdk_service (the last path segment of the SDK service import, for
	// example "s3" or "dynamodb").
	Service string
	// Method is the exact AWS SDK v2 Go client method name as it appears in the
	// call site's function_calls "name" field (for example "PutObject").
	Method string
}

// cloudActionByServiceMethod is the small, explicit, reviewable mapping from an
// observed AWS SDK v2 Go (service, method) call to the lowercase service:action
// token it provably invokes (#2723). Every value is an action present in the
// closed CAN_PERFORM catalog (iam_can_perform_catalog.go); the
// TestInvokesCloudActionMappingNeverProducesNonCatalogAction guard enforces that
// invariant so a row can never fabricate an unjoinable action. Method names were
// confirmed against the aws-sdk-go-v2 service packages in the module cache, so
// each row is an unambiguous, single-operation binding.
//
// A catalog action is included only when its AWS SDK v2 Go method name is a
// single unambiguous operation. The one deliberate omission is s3:listbucket:
// the IAM action s3:ListBucket is granted by several distinct SDK methods
// (ListObjects, ListObjectsV2, ListObjectVersions) and there is no SDK method
// literally named "ListBucket", so no unambiguous (service, method) row exists
// and it is skipped rather than guessed.
//
// It is a package-level value built once; callers MUST NOT mutate it.
var cloudActionByServiceMethod = map[cloudActionServiceMethod]string{
	// S3 object data-plane and destructive bucket actions.
	{Service: "s3", Method: "GetObject"}:    "s3:getobject",
	{Service: "s3", Method: "PutObject"}:    "s3:putobject",
	{Service: "s3", Method: "DeleteBucket"}: "s3:deletebucket",
	// s3:listbucket is intentionally omitted: ambiguous SDK method (see above).
	// KMS data-at-rest exposure and data-key issuance.
	{Service: "kms", Method: "Decrypt"}:         "kms:decrypt",
	{Service: "kms", Method: "GenerateDataKey"}: "kms:generatedatakey",
	// Secret / parameter exfiltration or value mutation.
	{Service: "secretsmanager", Method: "GetSecretValue"}: "secretsmanager:getsecretvalue",
	{Service: "secretsmanager", Method: "PutSecretValue"}: "secretsmanager:putsecretvalue",
	{Service: "ssm", Method: "GetParameter"}:              "ssm:getparameter",
	{Service: "ssm", Method: "GetParameters"}:             "ssm:getparameters",
	// DynamoDB item/table read and mutation.
	{Service: "dynamodb", Method: "GetItem"}:    "dynamodb:getitem",
	{Service: "dynamodb", Method: "Query"}:      "dynamodb:query",
	{Service: "dynamodb", Method: "Scan"}:       "dynamodb:scan",
	{Service: "dynamodb", Method: "PutItem"}:    "dynamodb:putitem",
	{Service: "dynamodb", Method: "UpdateItem"}: "dynamodb:updateitem",
	{Service: "dynamodb", Method: "DeleteItem"}: "dynamodb:deleteitem",
	// Destructive or workload-executing compute / database actions.
	{Service: "ec2", Method: "TerminateInstances"}: "ec2:terminateinstances",
	{Service: "ec2", Method: "StopInstances"}:      "ec2:stopinstances",
	{Service: "rds", Method: "DeleteDBInstance"}:   "rds:deletedbinstance",
	{Service: "rds", Method: "StopDBInstance"}:     "rds:stopdbinstance",
	// Lambda invocation. The IAM action is lambda:InvokeFunction but the SDK v2
	// Go method is Invoke (there is no method named InvokeFunction), so the
	// unambiguous binding is (lambda, Invoke) -> lambda:invokefunction.
	{Service: "lambda", Method: "Invoke"}: "lambda:invokefunction",
}

// resolveCloudAction returns the lowercase catalog action a (service, method)
// call provably invokes, or "" with ok=false when the pair is not in the mapping
// table or the produced action is not in the closed CAN_PERFORM catalog. The
// catalog re-check is a defense-in-depth correlation-truth guard: even if the
// mapping table drifted, the producer can never emit a non-catalog action.
func resolveCloudAction(
	service string,
	method string,
	catalog map[string]iamCanPerformAction,
) (string, bool) {
	service = strings.TrimSpace(service)
	method = strings.TrimSpace(method)
	if service == "" || method == "" {
		return "", false
	}
	action, ok := cloudActionByServiceMethod[cloudActionServiceMethod{Service: service, Method: method}]
	if !ok {
		return "", false
	}
	if _, inCatalog := catalog[action]; !inCatalog {
		return "", false
	}
	return action, true
}

// buildInvokesCloudActionIntentRows resolves Go AWS SDK call sites to one
// ordering-safe shared-projection intent per (caller Function, catalog action)
// it provably invokes (#2723).
//
// For every file envelope's function_calls it reads the parser-emitted
// receiver_sdk_service token and the call method name. It emits an intent only
// when ALL of the following hold, so a wrong or guessed action can never reach
// the graph:
//
//   - the call carries a non-empty receiver_sdk_service (the parser proved the
//     receiver is bound to an imported AWS SDK service package);
//   - the (service, method) pair maps to an action via the explicit mapping
//     table cloudActionByServiceMethod;
//   - that action is present in the closed CAN_PERFORM catalog; and
//   - the call's containing entity resolves to a Function.
//
// Otherwise the call is skipped. Multiple call sites that resolve to the same
// (Function, action) collapse to one intent.
func buildInvokesCloudActionIntentRows(
	envelopes []facts.Envelope,
	index codeEntityIndex,
	contextByRepoID map[string]ProjectionContext,
	createdAt time.Time,
	evidenceSource string,
) []SharedProjectionIntentRow {
	if len(envelopes) == 0 || len(contextByRepoID) == 0 {
		return nil
	}
	if evidenceSource == "" {
		evidenceSource = invokesCloudActionEvidenceSource
	}
	catalog := iamCanPerformCatalogByAction()

	intents := make([]SharedProjectionIntentRow, 0)
	seen := make(map[string]struct{})
	for _, env := range envelopes {
		if env.FactKind != factKindFile {
			continue
		}
		repositoryID := payloadStr(env.Payload, "repo_id")
		if repositoryID == "" {
			continue
		}
		context, ok := contextByRepoID[repositoryID]
		if !ok {
			continue
		}
		fileData, ok := env.Payload["parsed_file_data"].(map[string]any)
		if !ok {
			continue
		}
		relativePath := payloadStr(env.Payload, "relative_path")
		rawPath := anyToString(fileData["path"])

		for _, call := range mapSlice(fileData["function_calls"]) {
			service := anyToString(call["receiver_sdk_service"])
			method := anyToString(call["name"])
			action, ok := resolveCloudAction(service, method, catalog)
			if !ok {
				continue
			}
			callLine := codeCallInt(call["line_number"], call["ref_line"])
			if callLine <= 0 {
				continue
			}
			functionID := resolveContainingCodeEntityID(index, rawPath, relativePath, callLine)
			if functionID == "" {
				continue
			}
			if codeCallEndpointEntityType(index, repositoryID, functionID) != "Function" {
				continue
			}

			dedupeKey := functionID + "\x00" + action
			if _, exists := seen[dedupeKey]; exists {
				continue
			}
			seen[dedupeKey] = struct{}{}

			payload := map[string]any{
				"function_id":       functionID,
				"repo_id":           repositoryID,
				"action":            action,
				"action_id":         cloudActionIDPrefix + action,
				"evidence_source":   evidenceSource,
				"resolution_method": codeprovenance.MethodImportBinding,
				"confidence":        codeprovenance.Confidence(codeprovenance.MethodImportBinding),
				"reason":            codeprovenance.Reason(codeprovenance.MethodImportBinding),
			}

			intents = append(intents, BuildSharedProjectionIntent(SharedProjectionIntentInput{
				ProjectionDomain: DomainInvokesCloudAction,
				PartitionKey:     functionID + "->" + action,
				ScopeID:          context.ScopeID,
				AcceptanceUnitID: context.acceptanceUnitID(repositoryID),
				RepositoryID:     repositoryID,
				SourceRunID:      context.SourceRunID,
				GenerationID:     context.GenerationID,
				Payload:          payload,
				CreatedAt:        createdAt,
			}))
		}
	}

	sort.SliceStable(intents, func(i, j int) bool {
		if intents[i].RepositoryID != intents[j].RepositoryID {
			return intents[i].RepositoryID < intents[j].RepositoryID
		}
		return intents[i].IntentID < intents[j].IntentID
	})
	return intents
}
