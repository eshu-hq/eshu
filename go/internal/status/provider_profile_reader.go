// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status/semantic"
)

// WithSemanticProviderProfiles wraps a reader with static, redacted provider
// profile metadata sourced from runtime configuration.
func WithSemanticProviderProfiles(reader Reader, profiles ...semantic.ProviderProfileStatus) Reader {
	if reader == nil {
		return nil
	}
	if len(profiles) == 0 {
		return reader
	}
	return semanticProviderProfileReader{
		reader:   reader,
		profiles: semantic.CloneProviderProfiles(profiles),
	}
}

type semanticProviderProfileReader struct {
	reader   Reader
	profiles []semantic.ProviderProfileStatus
}

func (r semanticProviderProfileReader) CheckStatusReadiness(ctx context.Context) error {
	return checkReaderReadiness(ctx, r.reader)
}

func (r semanticProviderProfileReader) ReadStatusSnapshot(
	ctx context.Context,
	asOf time.Time,
) (RawSnapshot, error) {
	return r.ReadStatusSnapshotFiltered(ctx, asOf, FullSnapshotSelection())
}

func (r semanticProviderProfileReader) ReadStatusSnapshotFiltered(
	ctx context.Context,
	asOf time.Time,
	selection SnapshotSelection,
) (RawSnapshot, error) {
	raw, err := r.reader.ReadStatusSnapshotFiltered(ctx, asOf, selection)
	if err != nil {
		return RawSnapshot{}, err
	}
	raw.SemanticExtraction.ProviderProfiles = semantic.CloneProviderProfiles(r.profiles)
	return raw, nil
}
