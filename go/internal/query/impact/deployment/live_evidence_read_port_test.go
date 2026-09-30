// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deployment

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

type rejectingLiveEvidenceReader struct {
	calls int
	err   error
}

func (r *rejectingLiveEvidenceReader) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	r.calls++
	return nil, r.err
}

func TestKubernetesPodTemplateGuardedReadPort(t *testing.T) {
	want := errors.New("reader is stale")
	for _, tc := range []struct {
		name   string
		filter KubernetesPodTemplateFilter
		list   bool
	}{
		{name: "tracking has", filter: KubernetesPodTemplateFilter{TrackingID: "app:apps/Deployment:ns/api", AllScopes: true}},
		{name: "tracking list", filter: KubernetesPodTemplateFilter{TrackingID: "app:apps/Deployment:ns/api", AllScopes: true}, list: true},
		{name: "declared has", filter: KubernetesPodTemplateFilter{AnchorKind: liveIdentityAnchorDeclaredObject, GroupVersionResource: "apps/deployments", Namespace: "ns", Name: "api", AllScopes: true}},
		{name: "declared list", filter: KubernetesPodTemplateFilter{AnchorKind: liveIdentityAnchorDeclaredObject, GroupVersionResource: "apps/deployments", Namespace: "ns", Name: "api", AllScopes: true}, list: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := &rejectingLiveEvidenceReader{err: want}
			store := NewPostgresKubernetesPodTemplateStoreWithReadStore(reader)
			var err error
			if tc.list {
				_, err = store.ListLiveIdentityMatches(t.Context(), tc.filter)
			} else {
				_, err = store.HasLiveIdentityMatch(t.Context(), tc.filter)
			}
			if !errors.Is(err, want) || reader.calls != 1 {
				t.Fatalf("error %v, guarded calls %d; want stale reader and one call", err, reader.calls)
			}
		})
	}
}
