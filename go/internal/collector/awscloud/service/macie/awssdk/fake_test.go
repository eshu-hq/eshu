// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"time"

	awsmacie2 "github.com/aws/aws-sdk-go-v2/service/macie2"
)

// fakeMacie2API implements the metadata-only apiClient interface and records the
// operation names it served so tests can assert no sensitive-data finding,
// regex-body, allow-list-content, criteria, or mutation call was made.
type fakeMacie2API struct {
	session    *awsmacie2.GetMacieSessionOutput
	sessionErr error

	administrator    *awsmacie2.GetAdministratorAccountOutput
	administratorErr error

	memberPages []*awsmacie2.ListMembersOutput
	memberCalls int

	jobPages []*awsmacie2.ListClassificationJobsOutput
	jobCalls int

	allowListPages []*awsmacie2.ListAllowListsOutput
	allowListCalls int

	identifierPages []*awsmacie2.ListCustomDataIdentifiersOutput
	identifierCalls int

	filterPages []*awsmacie2.ListFindingsFiltersOutput
	filterCalls int

	statistics    *awsmacie2.GetFindingStatisticsOutput
	statisticsErr error

	calls []string
}

func (f *fakeMacie2API) GetMacieSession(
	_ context.Context,
	_ *awsmacie2.GetMacieSessionInput,
	_ ...func(*awsmacie2.Options),
) (*awsmacie2.GetMacieSessionOutput, error) {
	f.calls = append(f.calls, "GetMacieSession")
	if f.sessionErr != nil {
		return nil, f.sessionErr
	}
	if f.session == nil {
		return &awsmacie2.GetMacieSessionOutput{}, nil
	}
	return f.session, nil
}

func (f *fakeMacie2API) GetAdministratorAccount(
	_ context.Context,
	_ *awsmacie2.GetAdministratorAccountInput,
	_ ...func(*awsmacie2.Options),
) (*awsmacie2.GetAdministratorAccountOutput, error) {
	f.calls = append(f.calls, "GetAdministratorAccount")
	if f.administratorErr != nil {
		return nil, f.administratorErr
	}
	if f.administrator == nil {
		return &awsmacie2.GetAdministratorAccountOutput{}, nil
	}
	return f.administrator, nil
}

func (f *fakeMacie2API) ListMembers(
	_ context.Context,
	_ *awsmacie2.ListMembersInput,
	_ ...func(*awsmacie2.Options),
) (*awsmacie2.ListMembersOutput, error) {
	f.calls = append(f.calls, "ListMembers")
	return nextPage(f.memberPages, &f.memberCalls, &awsmacie2.ListMembersOutput{}), nil
}

func (f *fakeMacie2API) ListClassificationJobs(
	_ context.Context,
	_ *awsmacie2.ListClassificationJobsInput,
	_ ...func(*awsmacie2.Options),
) (*awsmacie2.ListClassificationJobsOutput, error) {
	f.calls = append(f.calls, "ListClassificationJobs")
	return nextPage(f.jobPages, &f.jobCalls, &awsmacie2.ListClassificationJobsOutput{}), nil
}

func (f *fakeMacie2API) ListAllowLists(
	_ context.Context,
	_ *awsmacie2.ListAllowListsInput,
	_ ...func(*awsmacie2.Options),
) (*awsmacie2.ListAllowListsOutput, error) {
	f.calls = append(f.calls, "ListAllowLists")
	return nextPage(f.allowListPages, &f.allowListCalls, &awsmacie2.ListAllowListsOutput{}), nil
}

func (f *fakeMacie2API) ListCustomDataIdentifiers(
	_ context.Context,
	_ *awsmacie2.ListCustomDataIdentifiersInput,
	_ ...func(*awsmacie2.Options),
) (*awsmacie2.ListCustomDataIdentifiersOutput, error) {
	f.calls = append(f.calls, "ListCustomDataIdentifiers")
	return nextPage(f.identifierPages, &f.identifierCalls, &awsmacie2.ListCustomDataIdentifiersOutput{}), nil
}

func (f *fakeMacie2API) ListFindingsFilters(
	_ context.Context,
	_ *awsmacie2.ListFindingsFiltersInput,
	_ ...func(*awsmacie2.Options),
) (*awsmacie2.ListFindingsFiltersOutput, error) {
	f.calls = append(f.calls, "ListFindingsFilters")
	return nextPage(f.filterPages, &f.filterCalls, &awsmacie2.ListFindingsFiltersOutput{}), nil
}

func (f *fakeMacie2API) GetFindingStatistics(
	_ context.Context,
	_ *awsmacie2.GetFindingStatisticsInput,
	_ ...func(*awsmacie2.Options),
) (*awsmacie2.GetFindingStatisticsOutput, error) {
	f.calls = append(f.calls, "GetFindingStatistics")
	if f.statisticsErr != nil {
		return nil, f.statisticsErr
	}
	if f.statistics == nil {
		return &awsmacie2.GetFindingStatisticsOutput{}, nil
	}
	return f.statistics, nil
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
