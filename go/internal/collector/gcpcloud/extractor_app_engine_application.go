// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"
)

// assetTypeAppEngineApplication is the CAI asset type for a GCP App Engine
// Application. An application is the top-level App Engine resource for a
// project; it owns services, versions, and a default Cloud Storage bucket.
const assetTypeAppEngineApplication = "appengine.googleapis.com/Application"

// relationshipTypeAppEngineApplicationUsesDefaultBucket is the bounded typed
// relationship from an App Engine Application to its default GCS staging bucket.
// The bucket target is built with the canonical CAI Bucket prefix
// storageBucketResourceNamePrefixFmt (shared with the other collector extractors
// and fixtures) so the endpoint resolves exactly during materialization.
const relationshipTypeAppEngineApplicationUsesDefaultBucket = "application_uses_default_bucket"

func init() {
	RegisterAssetExtractor(assetTypeAppEngineApplication, extractAppEngineApplication)
}

// appEngineApplicationData is the bounded view of a CAI
// appengine.googleapis.com/Application resource.data blob. Only
// redaction-safe control-plane posture fields are decoded; no data-plane
// content or secret material is present on this resource.
type appEngineApplicationData struct {
	LocationID      string `json:"locationId"`
	ServingStatus   string `json:"servingStatus"`
	DefaultBucket   string `json:"defaultBucket"`
	DefaultHostname string `json:"defaultHostname"`
	DatabaseType    string `json:"databaseType"`
	CreateTime      string `json:"createTime"`
}

// extractAppEngineApplication extracts bounded, redaction-safe typed depth for
// one App Engine Application CAI asset. It surfaces the location, serving
// status, default GCS bucket name, default hostname (host only — a public
// appspot.com host), database type, and creation time; emits the typed
// application_uses_default_bucket edge to the default Cloud Storage bucket;
// and surfaces the bucket full resource name as the correlation anchor.
//
// The application name, id, and any data-plane fields are not decoded. The
// default hostname is already a public appspot.com host (no path or query),
// so storing it verbatim is safe. The owning project is base-observation
// ancestry; the default bucket is the only resolvable outbound CAI endpoint
// from the application's own resource.data.
func extractAppEngineApplication(ctx ExtractContext) (AttributeExtraction, error) {
	var data appEngineApplicationData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode app engine application data: %w", err)
	}

	// Normalize the default bucket once so the attribute and the edge/anchor
	// can never drift apart on a later change.
	bucketName := strings.TrimSpace(data.DefaultBucket)

	attrs := map[string]any{}
	if v := strings.TrimSpace(data.LocationID); v != "" {
		attrs["location_id"] = v
	}
	if v := strings.TrimSpace(data.ServingStatus); v != "" {
		attrs["serving_status"] = v
	}
	if bucketName != "" {
		attrs["default_bucket"] = bucketName
	}
	if v := strings.TrimSpace(data.DefaultHostname); v != "" {
		attrs["default_hostname"] = v
	}
	if v := strings.TrimSpace(data.DatabaseType); v != "" {
		attrs["database_type"] = v
	}
	if v, ok := normalizeRFC3339(data.CreateTime); ok {
		attrs["creation_time"] = v
	}

	var anchors []string
	var rels []RelationshipObservation
	if bucketName != "" {
		bucketFullName := storageBucketResourceNamePrefixFmt + bucketName
		anchors = append(anchors, bucketFullName)
		rels = append(rels, RelationshipObservation{
			SourceFullResourceName: ctx.FullResourceName,
			SourceAssetType:        ctx.AssetType,
			RelationshipType:       relationshipTypeAppEngineApplicationUsesDefaultBucket,
			TargetFullResourceName: bucketFullName,
			TargetAssetType:        assetTypeStorageBucket,
			SupportState:           RelationshipSupportSupported,
		})
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: anchors,
		Relationships:      rels,
	}, nil
}
