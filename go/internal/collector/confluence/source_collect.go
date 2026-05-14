package confluence

import (
	"context"
	"errors"
	"fmt"
)

func (s *Source) collectPages(ctx context.Context) ([]Page, Space, int, error) {
	if s.Config.SpaceID != "" {
		spaceValue, err := s.Client.GetSpace(ctx, s.Config.SpaceID)
		if err != nil {
			return nil, Space{}, 0, fmt.Errorf("get confluence space: %w", err)
		}
		pages, err := s.Client.ListSpacePages(ctx, s.Config.SpaceID, pageLimit(s.Config.PageLimit))
		if err != nil {
			return nil, Space{}, 0, fmt.Errorf("list confluence space pages: %w", err)
		}
		pages, failures, err := s.enrichPages(ctx, pages)
		if err != nil {
			return nil, Space{}, 0, err
		}
		return pages, spaceValue, failures, nil
	}

	ids, err := s.Client.ListPageTree(ctx, s.Config.RootPageID, pageLimit(s.Config.PageLimit))
	if err != nil {
		return nil, Space{}, 0, fmt.Errorf("list confluence page tree: %w", err)
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
			return nil, Space{}, 0, fmt.Errorf("get confluence page %q: %w", id, err)
		}
		pages = append(pages, page)
	}
	return pages, Space{ID: firstSpaceID(pages), Key: s.Config.SpaceKey}, failures, nil
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
