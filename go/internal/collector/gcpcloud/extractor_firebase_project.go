// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud //nolint:dirgate // #6696 wave-1: flat extractor moves to extractor/<group> in a later slice

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud/catalog"
)

func init() {
	RegisterAssetExtractor(catalog.FirebaseProjectAssetType, extractFirebaseProject)
}

// firebaseProjectData is the bounded view of a CAI
// firebase.googleapis.com/FirebaseProject resource.data blob. Only redaction-safe
// control-plane posture and default-resource references are decoded; no key
// material, secret value, or response body is present in this asset type.
type firebaseProjectData struct {
	ProjectID     string `json:"projectId"`
	ProjectNumber string `json:"projectNumber"`
	DisplayName   string `json:"displayName"`
	State         string `json:"state"`
	Resources     *struct {
		HostingSite              string `json:"hostingSite"`
		RealtimeDatabaseInstance string `json:"realtimeDatabaseInstance"`
		StorageBucket            string `json:"storageBucket"`
		LocationID               string `json:"locationId"`
	} `json:"resources"`
}

// extractFirebaseProject extracts bounded, redaction-safe typed depth for one CAI
// Firebase Project asset. It surfaces the lifecycle state, display name, default
// resource location, and presence flags for the default Hosting site, Realtime
// Database instance, and Storage bucket; emits the supported edge to the backing
// GCP project (number-keyed canonical name, the form Cloud Asset Inventory uses
// for cloudresourcemanager.googleapis.com/Project) and a provenance-only
// `partial` edge to the default Storage bucket (canonical bucket name); and
// surfaces the backing project, default bucket, Hosting site, and Realtime
// Database instance references as correlation anchors.
//
// The default Hosting site and Realtime Database instance are not Cloud Asset
// Inventory asset types, so they remain correlation anchors only and mint no
// edge; the backing project edge is skipped when projectNumber is absent so no
// unresolvable target is emitted.
func extractFirebaseProject(ctx ExtractContext) (AttributeExtraction, error) {
	var data firebaseProjectData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode firebase project data: %w", err)
	}

	attrs := map[string]any{}
	if v := strings.TrimSpace(data.State); v != "" {
		attrs["state"] = v
	}
	if v := strings.TrimSpace(data.DisplayName); v != "" {
		attrs["display_name"] = v
	}

	var anchors []string
	var rels []RelationshipObservation

	if number := strings.TrimSpace(data.ProjectNumber); number != "" {
		target := catalog.CloudProjectResourceNamePrefix + number
		anchors = append(anchors, target)
		rels = append(rels, RelationshipObservation{
			SourceFullResourceName: ctx.FullResourceName,
			SourceAssetType:        ctx.AssetType,
			RelationshipType:       catalog.RelationshipTypeFirebaseProjectBackedByProject,
			TargetFullResourceName: target,
			TargetAssetType:        catalog.AssetTypeCloudResourceManagerProject,
			SupportState:           RelationshipSupportSupported,
		})
	}

	if data.Resources != nil {
		if v := strings.TrimSpace(data.Resources.LocationID); v != "" {
			attrs["location_id"] = v
		}
		if v := strings.TrimSpace(data.Resources.HostingSite); v != "" {
			attrs["hosting_site_present"] = true
			anchors = append(anchors, v)
		}
		if v := strings.TrimSpace(data.Resources.RealtimeDatabaseInstance); v != "" {
			attrs["realtime_database_present"] = true
			anchors = append(anchors, v)
		}
		if bucket := strings.TrimSpace(data.Resources.StorageBucket); bucket != "" {
			attrs["default_storage_bucket_present"] = true
			target := storageBucketResourceNamePrefixFmt + bucket
			anchors = append(anchors, target)
			// The default Storage bucket comes from Firebase's deprecated
			// DefaultResources block, which is not reliable as current resource
			// truth and can be stale. Carry the edge as `partial` so the reducer
			// treats the target as unresolved and never materializes a clean
			// canonical edge to a possibly-stale bucket; the presence flag and
			// anchor still record the observation for correlation.
			rels = append(rels, RelationshipObservation{
				SourceFullResourceName: ctx.FullResourceName,
				SourceAssetType:        ctx.AssetType,
				RelationshipType:       catalog.RelationshipTypeFirebaseProjectDefaultBucket,
				TargetFullResourceName: target,
				TargetAssetType:        assetTypeStorageBucket,
				SupportState:           RelationshipSupportPartial,
			})
		}
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: anchors,
		Relationships:      rels,
	}, nil
}
