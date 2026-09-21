// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"time"

	awsinspector2 "github.com/aws/aws-sdk-go-v2/service/inspector2"
)

// fakeInspector2API implements the metadata-only apiClient interface and
// records the operation names it served so tests can assert no finding-body or
// mutation call was made.
type fakeInspector2API struct {
	accountStatus *awsinspector2.BatchGetAccountStatusOutput

	memberPages []*awsinspector2.ListMembersOutput
	memberCalls int

	filterPages []*awsinspector2.ListFiltersOutput
	filterCalls int

	cisPages []*awsinspector2.ListCisScanConfigurationsOutput
	cisCalls int

	calls []string
}

func (f *fakeInspector2API) BatchGetAccountStatus(
	_ context.Context,
	_ *awsinspector2.BatchGetAccountStatusInput,
	_ ...func(*awsinspector2.Options),
) (*awsinspector2.BatchGetAccountStatusOutput, error) {
	f.calls = append(f.calls, "BatchGetAccountStatus")
	if f.accountStatus == nil {
		return &awsinspector2.BatchGetAccountStatusOutput{}, nil
	}
	return f.accountStatus, nil
}

func (f *fakeInspector2API) ListMembers(
	_ context.Context,
	_ *awsinspector2.ListMembersInput,
	_ ...func(*awsinspector2.Options),
) (*awsinspector2.ListMembersOutput, error) {
	f.calls = append(f.calls, "ListMembers")
	return nextPage(f.memberPages, &f.memberCalls, &awsinspector2.ListMembersOutput{}), nil
}

func (f *fakeInspector2API) ListFilters(
	_ context.Context,
	_ *awsinspector2.ListFiltersInput,
	_ ...func(*awsinspector2.Options),
) (*awsinspector2.ListFiltersOutput, error) {
	f.calls = append(f.calls, "ListFilters")
	return nextPage(f.filterPages, &f.filterCalls, &awsinspector2.ListFiltersOutput{}), nil
}

func (f *fakeInspector2API) ListCisScanConfigurations(
	_ context.Context,
	_ *awsinspector2.ListCisScanConfigurationsInput,
	_ ...func(*awsinspector2.Options),
) (*awsinspector2.ListCisScanConfigurationsOutput, error) {
	f.calls = append(f.calls, "ListCisScanConfigurations")
	return nextPage(f.cisPages, &f.cisCalls, &awsinspector2.ListCisScanConfigurationsOutput{}), nil
}

func nextPage[T any](pages []*T, calls *int, empty *T) *T {
	if *calls >= len(pages) {
		return empty
	}
	page := pages[*calls]
	*calls = *calls + 1
	return page
}

func mustTime() time.Time {
	return time.Date(2026, 5, 27, 12, 10, 0, 0, time.UTC)
}
