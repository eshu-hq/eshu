// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package confluence

import (
	"context"
	"errors"
	"fmt"
)

// collectPages lists and fetches the bounded page set for the active target.
// The returned bool is true when the space/page-tree cursor walk was
// truncated (see Client.ListSpacePages / Client.ListPageTree) -- the
// generation is still emitted, but with a coverage warning on the source
// fact rather than a silent partial read.
func (s *Source) collectPages(ctx context.Context) ([]Page, Space, int, bool, error) {
	if spaceID := s.activeSpaceID(); spaceID != "" {
		spaceValue, err := s.Client.GetSpace(ctx, spaceID)
		if err != nil {
			return nil, Space{}, 0, false, fmt.Errorf("get confluence space: %w", err)
		}
		pages, truncated, err := s.Client.ListSpacePages(ctx, spaceID, pageLimit(s.Config.PageLimit), s.Config.MaxTotalPages)
		if err != nil {
			return nil, Space{}, 0, false, fmt.Errorf("list confluence space pages: %w", err)
		}
		pages, failures, err := s.enrichPages(ctx, pages)
		if err != nil {
			return nil, Space{}, 0, false, err
		}
		return pages, spaceValue, failures, truncated, nil
	}

	ids, truncated, err := s.Client.ListPageTree(ctx, s.Config.RootPageID, pageLimit(s.Config.PageLimit), s.Config.MaxTotalPages)
	if err != nil {
		return nil, Space{}, 0, false, fmt.Errorf("list confluence page tree: %w", err)
	}
	pages := make([]Page, 0, len(ids))
	failures := 0
	for _, id := range ids {
		page, err := s.Client.GetPage(ctx, id)
		if err != nil {
			if errors.Is(err, ErrPermissionDenied) {
				failures++
				s.recordPermissionDeniedPage(ctx, "fetch_page")
				continue
			}
			return nil, Space{}, 0, false, fmt.Errorf("get confluence page %q: %w", id, err)
		}
		pages = append(pages, page)
	}
	return pages, Space{ID: firstSpaceID(pages), Key: s.Config.SpaceKey}, failures, truncated, nil
}

func (s *Source) enrichPages(ctx context.Context, listedPages []Page) ([]Page, int, error) {
	pages := make([]Page, 0, len(listedPages))
	failures := 0
	for _, listed := range listedPages {
		page, err := s.Client.GetPage(ctx, listed.ID)
		if err != nil {
			if errors.Is(err, ErrPermissionDenied) {
				failures++
				s.recordPermissionDeniedPage(ctx, "fetch_page")
				continue
			}
			return nil, 0, fmt.Errorf("get confluence page %q: %w", listed.ID, err)
		}
		pages = append(pages, mergePageDetails(listed, page))
	}
	return pages, failures, nil
}
