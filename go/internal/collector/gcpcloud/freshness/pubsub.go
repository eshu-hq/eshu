// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package freshness

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud"
)

// ErrWelcomeMessage is returned when a Pub/Sub push delivery carries the
// bare-string welcome message Cloud Asset Inventory sends on first
// subscription to a feed, instead of a TemporalAsset payload. Callers must
// treat this as a benign no-op, not a malformed event.
var ErrWelcomeMessage = errors.New("gcp freshness: pub/sub welcome message, not a temporal asset")

// pubSubPushEnvelope is the standard Google Cloud Pub/Sub push delivery
// shape: https://cloud.google.com/pubsub/docs/push#receiving_messages.
type pubSubPushEnvelope struct {
	Message struct {
		Data        string `json:"data"`
		MessageID   string `json:"messageId"`
		PublishTime string `json:"publishTime"`
	} `json:"message"`
	Subscription string `json:"subscription"`
}

// temporalAsset is a defensive, partial model of the Cloud Asset Inventory
// TemporalAsset feed payload delivered inside the Pub/Sub message data. Only
// the fields needed to derive bounded scope identity are modeled; the raw
// resource.data blob, IAM policy, and other asset content are intentionally
// not decoded into any field this package retains.
type temporalAsset struct {
	Asset           *caiAsset `json:"asset"`
	PriorAsset      *caiAsset `json:"priorAsset"`
	Deleted         bool      `json:"deleted"`
	PriorAssetState string    `json:"priorAssetState"`
	Window          struct {
		StartTime time.Time `json:"startTime"`
	} `json:"window"`
}

type caiAsset struct {
	Name       string    `json:"name"`
	AssetType  string    `json:"assetType"`
	Ancestors  []string  `json:"ancestors"`
	UpdateTime time.Time `json:"updateTime"`
	Resource   struct {
		Location string `json:"location"`
	} `json:"resource"`
}

// NormalizePubSubPush maps one Cloud Asset Inventory feed notification,
// delivered as a Google Cloud Pub/Sub push request body, into a bounded GCP
// freshness trigger. It performs no GCP API calls and treats the event as a
// wake-up signal only.
//
// The raw asset blob, IAM policy, and resource body are never retained; only
// bounded scope identity (parent scope, asset type, location) survives on the
// returned Trigger.
func NormalizePubSubPush(payload []byte) (Trigger, error) {
	var envelope pubSubPushEnvelope
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return Trigger{}, fmt.Errorf("decode Pub/Sub push envelope: %w", err)
	}
	eventID := strings.TrimSpace(envelope.Message.MessageID)
	if eventID == "" {
		return Trigger{}, fmt.Errorf("gcp freshness: pub/sub message is missing messageId")
	}
	data, err := base64.StdEncoding.DecodeString(envelope.Message.Data)
	if err != nil {
		return Trigger{}, fmt.Errorf("decode Pub/Sub push message data: %w", err)
	}

	// The first delivery to a brand-new CAI feed subscription is a bare JSON
	// string welcome message, not a TemporalAsset object. Detect and reject
	// it before attempting to unmarshal into the object shape below.
	var welcome string
	if err := json.Unmarshal(data, &welcome); err == nil {
		return Trigger{}, ErrWelcomeMessage
	}

	var feed temporalAsset
	if err := json.Unmarshal(data, &feed); err != nil {
		return Trigger{}, fmt.Errorf("decode Cloud Asset Inventory TemporalAsset payload: %w", err)
	}

	kind := EventKindAssetChange
	asset := feed.Asset
	if feed.Deleted {
		kind = EventKindAssetDeleted
		if asset == nil {
			asset = feed.PriorAsset
		}
	}
	if asset == nil {
		return Trigger{}, fmt.Errorf("gcp freshness: temporal asset payload has no asset or priorAsset")
	}

	parentScopeKind, parentScopeID, err := parentScopeFromAncestors(asset.Ancestors)
	if err != nil {
		return Trigger{}, err
	}

	publishTime, _ := time.Parse(time.RFC3339, strings.TrimSpace(envelope.Message.PublishTime))
	observedAt := firstTime(feed.Window.StartTime, asset.UpdateTime, publishTime)
	if observedAt.IsZero() {
		return Trigger{}, fmt.Errorf("gcp freshness: unable to derive observed_at from window, asset, or publish time")
	}

	trigger := Trigger{
		EventID:         eventID,
		Kind:            kind,
		ParentScopeKind: parentScopeKind,
		ParentScopeID:   parentScopeID,
		AssetType:       asset.AssetType,
		Location:        asset.Resource.Location,
		ObservedAt:      observedAt,
	}
	if err := trigger.Validate(); err != nil {
		return Trigger{}, err
	}
	return trigger.normalized(), nil
}

// parentScopeFromAncestors derives the parent scope kind and id from a CAI
// ancestors list, for example
// ["projects/123456789012", "folders/999", "organizations/1"]. The most
// specific ancestor (index 0) wins.
func parentScopeFromAncestors(ancestors []string) (gcpcloud.ParentScopeKind, string, error) {
	for _, ancestor := range ancestors {
		ancestor = strings.TrimSpace(ancestor)
		kind, id, ok := strings.Cut(ancestor, "/")
		if !ok || strings.TrimSpace(id) == "" {
			continue
		}
		switch kind {
		case "projects":
			return gcpcloud.ParentScopeProject, id, nil
		case "folders":
			return gcpcloud.ParentScopeFolder, id, nil
		case "organizations":
			return gcpcloud.ParentScopeOrganization, id, nil
		}
	}
	return "", "", fmt.Errorf("gcp freshness: ancestors carry no parseable parent scope")
}

func firstTime(values ...time.Time) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value.UTC()
		}
	}
	return time.Time{}
}
