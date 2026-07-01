// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/secretsiam"
)

// Asset types and full-resource-name prefixes for the Eventarc Trigger endpoints.
// The Cloud Run Service, Cloud Functions Function, and Pub/Sub Topic asset types
// are reused from their own extractors; only the Eventarc and Workflows types and
// the destination prefixes are declared here.
const (
	assetTypeEventarcTrigger = "eventarc.googleapis.com/Trigger"
	assetTypeEventarcChannel = "eventarc.googleapis.com/Channel"
	assetTypeWorkflow        = "workflows.googleapis.com/Workflow"

	runServiceResourceNamePrefix    = "//run.googleapis.com/"
	cloudFunctionResourceNamePrefix = "//cloudfunctions.googleapis.com/"
	workflowResourceNamePrefix      = "//workflows.googleapis.com/"
	eventarcResourceNamePrefix      = "//eventarc.googleapis.com/"
)

// Bounded provider relationship types for Eventarc Trigger edges.
const (
	relationshipTypeTriggerTargetsService  = "trigger_targets_service"
	relationshipTypeTriggerTargetsFunction = "trigger_targets_function"
	relationshipTypeTriggerTargetsWorkflow = "trigger_targets_workflow"
	relationshipTypeTriggerTransportTopic  = "trigger_transport_topic"
	relationshipTypeTriggerUsesChannel     = "trigger_uses_channel"
)

func init() {
	RegisterAssetExtractor(assetTypeEventarcTrigger, extractEventarcTrigger)
}

// eventarcTriggerData is the bounded view of a CAI eventarc.googleapis.com/Trigger
// resource.data blob. Only redaction-safe control-plane metadata and resource
// references are decoded; the destination http-endpoint URI and the Cloud Run
// destination path are never decoded, and the service account is reduced to an
// email fingerprint.
type eventarcTriggerData struct {
	EventFilters []struct {
		Attribute string `json:"attribute"`
		Value     string `json:"value"`
	} `json:"eventFilters"`
	ServiceAccount string `json:"serviceAccount"`
	Destination    struct {
		CloudRun *struct {
			Service string `json:"service"`
			Region  string `json:"region"`
		} `json:"cloudRun"`
		CloudFunction string `json:"cloudFunction"`
		Workflow      string `json:"workflow"`
	} `json:"destination"`
	Transport *struct {
		Pubsub *struct {
			Topic string `json:"topic"`
		} `json:"pubsub"`
	} `json:"transport"`
	Channel    string `json:"channel"`
	CreateTime string `json:"createTime"`
}

// extractEventarcTrigger extracts bounded, redaction-safe typed depth for one CAI
// Eventarc Trigger. It returns the Terraform/drift/monitoring attribute set
// (event type, event-filter count, destination type, transport type, creation
// time, and the fingerprinted trigger service account), the destination target,
// transport topic, channel, and fingerprinted service-account email as
// correlation anchors, and the typed destination (service/function/workflow),
// transport-topic, and channel edges. The service account is joined via its
// fingerprinted-email digest; the destination http URI and Cloud Run path are
// never read.
func extractEventarcTrigger(ctx ExtractContext) (AttributeExtraction, error) {
	var data eventarcTriggerData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode eventarc trigger data: %w", err)
	}

	saDigest := secretsiam.GCPServiceAccountEmailDigest(data.ServiceAccount)
	attrs := eventarcTriggerAttributes(data, saDigest)

	var anchors []string
	var rels []RelationshipObservation
	if saDigest != "" {
		anchors = append(anchors, saDigest)
	}
	if dest, targetType, relType := eventarcTriggerDestination(ctx.FullResourceName, data); dest != "" {
		anchors = append(anchors, dest)
		rels = append(rels, eventarcTriggerEdge(ctx, relType, dest, targetType))
	}
	if data.Transport != nil && data.Transport.Pubsub != nil {
		if topic := eventarcRelativeFullName(pubSubResourceNamePrefix, data.Transport.Pubsub.Topic); topic != "" {
			anchors = append(anchors, topic)
			rels = append(rels, eventarcTriggerEdge(ctx, relationshipTypeTriggerTransportTopic, topic, assetTypePubSubTopic))
		}
	}
	if channel := eventarcRelativeFullName(eventarcResourceNamePrefix, data.Channel); channel != "" {
		anchors = append(anchors, channel)
		rels = append(rels, eventarcTriggerEdge(ctx, relationshipTypeTriggerUsesChannel, channel, assetTypeEventarcChannel))
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: dedupeNonEmpty(anchors),
		Relationships:      rels,
	}, nil
}

// eventarcTriggerAttributes assembles the bounded attribute map. Absent fields are
// omitted rather than written as zero values.
func eventarcTriggerAttributes(data eventarcTriggerData, saDigest string) map[string]any {
	attrs := map[string]any{}
	if v := eventarcTriggerEventType(data); v != "" {
		attrs["event_type"] = v
	}
	if n := len(data.EventFilters); n > 0 {
		attrs["event_filter_count"] = n
	}
	if v := eventarcTriggerDestinationType(data); v != "" {
		attrs["destination_type"] = v
	}
	if data.Transport != nil && data.Transport.Pubsub != nil {
		attrs["transport_type"] = "pubsub"
	}
	if v, ok := normalizeRFC3339(data.CreateTime); ok {
		attrs["creation_time"] = v
	}
	if saDigest != "" {
		attrs["service_account_fingerprint"] = saDigest
	}
	return attrs
}

// eventarcTriggerEventType returns the value of the "type" event filter, the
// bounded CloudEvents type the trigger matches.
func eventarcTriggerEventType(data eventarcTriggerData) string {
	for _, f := range data.EventFilters {
		if strings.TrimSpace(f.Attribute) == "type" {
			return strings.TrimSpace(f.Value)
		}
	}
	return ""
}

// eventarcTriggerDestinationType returns the bounded destination kind.
func eventarcTriggerDestinationType(data eventarcTriggerData) string {
	switch {
	case data.Destination.CloudRun != nil && strings.TrimSpace(data.Destination.CloudRun.Service) != "":
		return "run"
	case strings.TrimSpace(data.Destination.CloudFunction) != "":
		return "function"
	case strings.TrimSpace(data.Destination.Workflow) != "":
		return "workflow"
	default:
		return ""
	}
}

// eventarcTriggerDestination resolves the trigger's destination to a CAI full
// resource name, the target asset type, and the edge relationship type. The Cloud
// Run destination is a short service name qualified with the trigger's project and
// the destination region (falling back to the trigger's own location). It returns
// empty strings when no supported destination is set.
func eventarcTriggerDestination(triggerFullName string, data eventarcTriggerData) (full, targetType, relType string) {
	if cr := data.Destination.CloudRun; cr != nil && strings.TrimSpace(cr.Service) != "" {
		project, location := cloudFunctionProjectLocation(triggerFullName)
		region := strings.TrimSpace(cr.Region)
		if region == "" {
			region = location
		}
		if project == "" || region == "" {
			return "", "", ""
		}
		full = runServiceResourceNamePrefix + "projects/" + project + "/locations/" + region + "/services/" + strings.TrimSpace(cr.Service)
		return full, assetTypeRunService, relationshipTypeTriggerTargetsService
	}
	if fn := eventarcRelativeFullName(cloudFunctionResourceNamePrefix, data.Destination.CloudFunction); fn != "" {
		return fn, assetTypeCloudFunction, relationshipTypeTriggerTargetsFunction
	}
	if wf := eventarcRelativeFullName(workflowResourceNamePrefix, data.Destination.Workflow); wf != "" {
		return wf, assetTypeWorkflow, relationshipTypeTriggerTargetsWorkflow
	}
	return "", "", ""
}

// eventarcRelativeFullName prefixes a relative CAI resource name (projects/...)
// with the given service prefix. An already-absolute //... name is returned
// unchanged; a blank reference yields "".
func eventarcRelativeFullName(prefix, ref string) string {
	trimmed := strings.TrimSpace(ref)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "//") {
		return trimmed
	}
	return prefix + strings.TrimPrefix(trimmed, "/")
}

func eventarcTriggerEdge(ctx ExtractContext, relationshipType, targetName, targetType string) RelationshipObservation {
	return RelationshipObservation{
		SourceFullResourceName: ctx.FullResourceName,
		SourceAssetType:        ctx.AssetType,
		RelationshipType:       relationshipType,
		TargetFullResourceName: targetName,
		TargetAssetType:        targetType,
		SupportState:           RelationshipSupportSupported,
	}
}
