// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/securityalerts"
	"github.com/eshu-hq/eshu/go/internal/collector/securityalerts/alertruntime"
)

// TestBuildClaimedServiceCommitsOrgReadStateWithItsGeneration pins the #8943
// wiring: the claimed service's committer persists generation state in the
// commit transaction, and the source is configured to hand it the org read
// state. Without either half a truncated org read commits without its record.
func TestBuildClaimedServiceCommitsOrgReadStateWithItsGeneration(t *testing.T) {
	t.Parallel()

	service, err := buildClaimedService(nil, testSecurityAlertGetenv, nil, nil, nil)
	if err != nil {
		t.Fatalf("buildClaimedService() error = %v, want nil", err)
	}
	if _, ok := service.Committer.(collector.StateClaimedCommitter); !ok {
		t.Fatalf("committer %T does not implement collector.StateClaimedCommitter", service.Committer)
	}

	var source alertruntime.SourceConfig
	attachReadState(&source)
	if source.ReadStateWriter == nil {
		t.Fatal("attachReadState left ReadStateWriter nil")
	}
	if writer := source.ReadStateWriter(securityalerts.TargetReadState{GenerationID: "g", ScopeID: "s"}); writer == nil {
		t.Fatal("ReadStateWriter returned a nil writer")
	}
}
