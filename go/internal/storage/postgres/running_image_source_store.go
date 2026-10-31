// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// RunningImageSourceStore calls protected typed-source transactions with the
// authenticated producer connection. The database, not these DTOs, grants
// producer instance/scope authority.
type RunningImageSourceStore struct{ DB db.Queryer }

// RunningImageCollectionStart describes one fresh fenced producer collection.
type RunningImageCollectionStart struct {
	ProducerInstanceID string
	SourceScopeID      string
	FreshGenerationID  string
	ProducerClass      workflow.RunningImageProducerClass
	ObservationClass   workflow.RunningImageObservationClass
	ObservedFrom       time.Time
	ObservedThrough    time.Time
	ConsistencyClass   string
	LeaseSeconds       int
	PolicyRevision     int64
	DatabaseIdentity   string
}

// RunningImagePage is a bounded immutable page and delivery receipt.
type RunningImagePage struct {
	ProducerInstanceID string
	SourceScopeID      string
	SourceGenerationID string
	CollectionFence    int64
	DeliveryID         string
	PageIndex          int
	StartTokenSHA256   string
	NextTokenSHA256    string
	Terminal           bool
	Observations       []workflow.RunningImageObservation
	PolicyRevision     int64
	DatabaseIdentity   string
}

// RunningImagePageResult is the accepted receipt status and DB-computed page
// hash. Duplicate returns the original hash; conflict fails the generation.
type RunningImagePageResult struct {
	Status        string `json:"status"`
	RequestSHA256 string `json:"request_sha256"`
}

// RunningImagePublication seals a complete page chain and switches the
// protected active source pointer in a short transaction.
type RunningImagePublication struct {
	ProducerInstanceID string
	SourceScopeID      string
	SourceGenerationID string
	CollectionFence    int64
	ExpectedPages      int
	ExpectedSlots      int
	ManifestSHA256     string
	PolicyRevision     int64
	DatabaseIdentity   string
}

type runningImageObservationPayload struct {
	ObjectID              string                                `json:"object_id"`
	SlotID                string                                `json:"slot_id"`
	ProducerClass         workflow.RunningImageProducerClass    `json:"producer_class"`
	ObservationClass      workflow.RunningImageObservationClass `json:"observation_class"`
	ObservedRevision      string                                `json:"observed_revision"`
	Registry              string                                `json:"registry"`
	Repository            string                                `json:"repository"`
	RuntimeImageReference string                                `json:"runtime_image_reference"`
	RuntimeDigest         string                                `json:"runtime_digest"`
	RuntimeDigestKind     workflow.RunningImageDigestKind       `json:"runtime_digest_kind"`
	LastUpdateStatus      string                                `json:"last_update_status"`
}

// StartCollection obtains a DB-issued fence for a fresh generation. A new
// staging claim never retires the last complete published generation.
func (s RunningImageSourceStore) StartCollection(ctx context.Context,
	start RunningImageCollectionStart,
) (int64, error) {
	if s.DB == nil {
		return 0, fmt.Errorf("running-image source database is required")
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT eshu_security.start_running_image_collection_v1(
        $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		start.ProducerInstanceID, start.SourceScopeID, start.FreshGenerationID,
		string(start.ProducerClass), string(start.ObservationClass),
		start.ObservedFrom, start.ObservedThrough, start.ConsistencyClass,
		start.LeaseSeconds, start.PolicyRevision, start.DatabaseIdentity)
	if err != nil {
		return 0, fmt.Errorf("start running-image collection: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return 0, fmt.Errorf("start running-image collection: no fence")
	}
	var fence int64
	if err := rows.Scan(&fence); err != nil {
		return 0, fmt.Errorf("scan running-image fence: %w", err)
	}
	return fence, rows.Err()
}

// StagePage commits one bounded typed delivery, durable receipt, observations
// and projection outbox atomically. The producer builds the complete manifest
// from the DB-returned page hashes, never from a guessed JSON encoding.
func (s RunningImageSourceStore) StagePage(ctx context.Context,
	page RunningImagePage,
) (RunningImagePageResult, error) {
	if s.DB == nil {
		return RunningImagePageResult{}, fmt.Errorf("running-image source database is required")
	}
	if len(page.Observations) > 256 {
		return RunningImagePageResult{}, fmt.Errorf("running-image page exceeds 256 observations")
	}
	payload := make([]runningImageObservationPayload, 0, len(page.Observations))
	for _, observation := range page.Observations {
		if err := observation.Validate(); err != nil {
			return RunningImagePageResult{}, err
		}
		if observation.ProducerInstanceID != page.ProducerInstanceID ||
			observation.SourceScopeID != page.SourceScopeID ||
			observation.SourceGenerationID != page.SourceGenerationID {
			return RunningImagePageResult{}, fmt.Errorf("running-image page observation boundary mismatch")
		}
		payload = append(payload, runningImageObservationPayload{
			ObjectID: observation.ObjectID, SlotID: observation.SlotID,
			ProducerClass: observation.ProducerClass, ObservationClass: observation.ObservationClass,
			ObservedRevision: observation.ObservedRevision, Registry: observation.Registry,
			Repository: observation.Repository, RuntimeImageReference: observation.RuntimeImageReference,
			RuntimeDigest: observation.RuntimeDigest, RuntimeDigestKind: observation.RuntimeDigestKind,
			LastUpdateStatus: observation.LastUpdateStatus,
		})
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return RunningImagePageResult{}, fmt.Errorf("encode running-image page: %w", err)
	}
	if len(raw) > 1048576 {
		return RunningImagePageResult{}, fmt.Errorf("running-image page exceeds 1 MiB")
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT eshu_security.stage_running_image_page_v1(
        $1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb,$11,$12)`,
		page.ProducerInstanceID, page.SourceScopeID, page.SourceGenerationID,
		page.CollectionFence, page.DeliveryID, page.PageIndex,
		page.StartTokenSHA256, page.NextTokenSHA256, page.Terminal,
		string(raw), page.PolicyRevision, page.DatabaseIdentity)
	if err != nil {
		return RunningImagePageResult{}, fmt.Errorf("stage running-image page: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return RunningImagePageResult{}, fmt.Errorf("stage running-image page: no receipt")
	}
	var resultRaw []byte
	if err := rows.Scan(&resultRaw); err != nil {
		return RunningImagePageResult{}, fmt.Errorf("scan running-image page receipt: %w", err)
	}
	if err := rows.Err(); err != nil {
		return RunningImagePageResult{}, err
	}
	var result RunningImagePageResult
	if err := json.Unmarshal(resultRaw, &result); err != nil {
		return RunningImagePageResult{}, fmt.Errorf("decode running-image page receipt: %w", err)
	}
	if result.Status != "accepted" && result.Status != "duplicate" && result.Status != "conflict" {
		return RunningImagePageResult{}, fmt.Errorf("running-image page returned unknown status")
	}
	return result, nil
}

// PublishGeneration seals a complete, accepted page manifest and returns its
// DB-issued activation epoch. It cannot publish a partial generation.
func (s RunningImageSourceStore) PublishGeneration(ctx context.Context,
	publication RunningImagePublication,
) (int64, error) {
	if s.DB == nil {
		return 0, fmt.Errorf("running-image source database is required")
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT eshu_security.publish_running_image_generation_v1(
        $1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		publication.ProducerInstanceID, publication.SourceScopeID,
		publication.SourceGenerationID, publication.CollectionFence,
		publication.ExpectedPages, publication.ExpectedSlots,
		publication.ManifestSHA256, publication.PolicyRevision,
		publication.DatabaseIdentity)
	if err != nil {
		return 0, fmt.Errorf("publish running-image generation: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return 0, fmt.Errorf("publish running-image generation: no epoch")
	}
	var epoch int64
	if err := rows.Scan(&epoch); err != nil {
		return 0, fmt.Errorf("scan running-image publication: %w", err)
	}
	return epoch, rows.Err()
}

// RunningImageManifestSHA256 hashes ordered page indices and DB-returned
// request hashes using the exact format checked by publication.
func RunningImageManifestSHA256(pageHashes []string) (string, error) {
	if len(pageHashes) == 0 || len(pageHashes) > 100001 {
		return "", fmt.Errorf("running-image page manifest size invalid")
	}
	var manifest strings.Builder
	for index, pageHash := range pageHashes {
		if len(pageHash) != 64 {
			return "", fmt.Errorf("running-image page hash invalid")
		}
		if _, err := hex.DecodeString(pageHash); err != nil {
			return "", fmt.Errorf("running-image page hash invalid: %w", err)
		}
		if index > 0 {
			manifest.WriteByte(',')
		}
		fmt.Fprintf(&manifest, "%d:%s", index, pageHash)
	}
	sum := sha256.Sum256([]byte(manifest.String()))
	return hex.EncodeToString(sum[:]), nil
}
