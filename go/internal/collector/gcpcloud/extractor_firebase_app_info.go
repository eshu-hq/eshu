// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"
)

// firebaseAppInfoAssetType is the Cloud Asset Inventory asset type for a Firebase
// App Info summary (a Web, Android, or iOS app registered under a Firebase
// project). The parent project edge targets firebaseProjectAssetType declared in
// the Firebase Project extractor.
const firebaseAppInfoAssetType = "firebase.googleapis.com/FirebaseAppInfo"

// firebaseProjectResourceNamePrefix is the canonical CAI full-resource-name
// prefix for a Firebase Project (`//firebase.googleapis.com/projects/<projectId>`).
const firebaseProjectResourceNamePrefix = "//firebase.googleapis.com/projects/"

// relationshipTypeFirebaseAppBelongsToProject is the bounded provider
// relationship type for the edge from a Firebase app to its parent Firebase
// project.
const relationshipTypeFirebaseAppBelongsToProject = "firebase_app_belongs_to_project"

func init() {
	RegisterAssetExtractor(firebaseAppInfoAssetType, extractFirebaseAppInfo)
}

// firebaseAppInfoData is the bounded view of a CAI
// firebase.googleapis.com/FirebaseAppInfo resource.data blob. The app id,
// platform, display name, namespace (Android package / iOS bundle / web
// namespace), and lifecycle state are control-plane metadata; no key material,
// secret value, or response body is present in this asset type.
type firebaseAppInfoData struct {
	AppID       string `json:"appId"`
	Platform    string `json:"platform"`
	DisplayName string `json:"displayName"`
	Namespace   string `json:"namespace"`
	State       string `json:"state"`
}

// extractFirebaseAppInfo extracts bounded, redaction-safe typed depth for one CAI
// Firebase App Info asset. It surfaces the app id, platform (WEB / ANDROID /
// IOS), display name, namespace, and lifecycle state; and emits the typed edge to
// the parent Firebase project with the canonical FirebaseProject full resource
// name as the correlation anchor.
//
// The platform is an enum, not a Cloud Asset Inventory resource, so it stays an
// attribute and mints no edge. The parent project is derived from the app's own
// full resource name; when no project segment is present the edge is skipped so
// no unresolvable target is emitted.
func extractFirebaseAppInfo(ctx ExtractContext) (AttributeExtraction, error) {
	var data firebaseAppInfoData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode firebase app info data: %w", err)
	}

	attrs := map[string]any{}
	if v := strings.TrimSpace(data.AppID); v != "" {
		attrs["app_id"] = v
	}
	if v := strings.TrimSpace(data.Platform); v != "" {
		attrs["platform"] = v
	}
	if v := strings.TrimSpace(data.DisplayName); v != "" {
		attrs["display_name"] = v
	}
	if v := strings.TrimSpace(data.Namespace); v != "" {
		attrs["namespace"] = v
	}
	if v := strings.TrimSpace(data.State); v != "" {
		attrs["state"] = v
	}

	var anchors []string
	var rels []RelationshipObservation
	if projectID := ProjectIDFromFullName(ctx.FullResourceName); projectID != "" {
		target := firebaseProjectResourceNamePrefix + projectID
		anchors = append(anchors, target)
		rels = append(rels, RelationshipObservation{
			SourceFullResourceName: ctx.FullResourceName,
			SourceAssetType:        ctx.AssetType,
			RelationshipType:       relationshipTypeFirebaseAppBelongsToProject,
			TargetFullResourceName: target,
			TargetAssetType:        firebaseProjectAssetType,
			SupportState:           RelationshipSupportSupported,
		})
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: anchors,
		Relationships:      rels,
	}, nil
}
