// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/reducer/cloudasset"
)

// stubCloudAssetResolutionWriter is a no-op cloudasset.CloudAssetResolutionWriter
// used by root wiring tests that need SOME non-nil writer to exercise a
// different domain's registration path through NewDefaultRuntime/
// implementedDefaultDomainDefinitions; it never asserts on its own inputs.
// The cloud asset resolution family's own behavior is tested in
// internal/reducer/cloudasset. Go test files cannot share unexported symbols
// across a package boundary, so this is a local root copy scoped to wiring
// tests only (issue #6061).
type stubCloudAssetResolutionWriter struct {
	result cloudasset.CloudAssetResolutionWriteResult
	err    error
}

func (w *stubCloudAssetResolutionWriter) WriteCloudAssetResolution(
	context.Context,
	cloudasset.CloudAssetResolutionWrite,
) (cloudasset.CloudAssetResolutionWriteResult, error) {
	return w.result, w.err
}
