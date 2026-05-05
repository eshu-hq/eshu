//go:build !nolocalllm

package main

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/eshulocal"
)

// embeddedLocalNornicDBAvailable reports whether this Eshu binary contains the
// NornicDB library-mode runtime. Plain builds keep the process fallback because
// current NornicDB library imports require the no-local-LLM build tag.
func embeddedLocalNornicDBAvailable() bool {
	return false
}

// startEmbeddedLocalNornicDB returns actionable guidance when the caller asks a
// plain Eshu build to use the library-mode runtime.
func startEmbeddedLocalNornicDB(ctx context.Context, layout eshulocal.Layout) (*managedLocalGraph, error) {
	return nil, fmt.Errorf("embedded NornicDB is not available in this Eshu build; rebuild with -tags nolocalllm or set %s=process", localNornicDBRuntimeModeEnv)
}
