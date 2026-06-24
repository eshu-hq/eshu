// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpruntime

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud"
)

func (s *Source) drainTagPages(
	ctx context.Context,
	scopeCfg ScopeConfig,
	generation *gcpcloud.Generation,
	resources []gcpcloud.ResourceObservation,
) error {
	if s.TagProvider == nil || len(resources) == 0 {
		return nil
	}
	for _, resource := range resources {
		for _, sourceKind := range tagSourceKinds(scopeCfg) {
			if err := s.drainTagSource(ctx, scopeCfg, generation, resource, sourceKind); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Source) drainTagSource(
	ctx context.Context,
	scopeCfg ScopeConfig,
	generation *gcpcloud.Generation,
	resource gcpcloud.ResourceObservation,
	sourceKind string,
) error {
	token := ""
	tags := map[string]string{}
	inheritance := map[string]string{}
	sourceURI := ""
	for {
		if token != "" {
			s.recordPageTokenResume(ctx, scopeCfg.ParentScopeKind)
		}
		page, err := s.TagProvider.FetchTagPage(ctx, TagRequest{
			Scope:            scopeCfg,
			FullResourceName: resource.Name,
			AssetType:        resource.AssetType,
			SourceKind:       sourceKind,
			PageToken:        token,
		})
		if err != nil {
			var warning ProviderWarning
			if errors.As(err, &warning) {
				s.addTagObservation(generation, resource, sourceKind, tags, inheritance, sourceURI)
				generation.AddWarning(s.providerWarning(scopeCfg, generation, warning))
				s.recordWarning(ctx, warning.WarningKind, warning.Outcome)
				return nil
			}
			return fmt.Errorf("fetch gcp %s tags for parent scope kind %q: %w", sourceKind, scopeCfg.ParentScopeKind, err)
		}
		if sourceURI == "" {
			sourceURI = strings.TrimSpace(page.SourceURI)
		}
		for key, value := range page.Tags {
			key = strings.TrimSpace(key)
			if key == "" {
				continue
			}
			tags[key] = value
		}
		for key, state := range page.InheritanceState {
			key = strings.TrimSpace(key)
			if key == "" {
				continue
			}
			inheritance[key] = state
		}
		token = strings.TrimSpace(page.NextPageToken)
		if token == "" {
			s.addTagObservation(generation, resource, sourceKind, tags, inheritance, sourceURI)
			return nil
		}
	}
}

func (s *Source) addTagObservation(
	generation *gcpcloud.Generation,
	resource gcpcloud.ResourceObservation,
	sourceKind string,
	tags map[string]string,
	inheritance map[string]string,
	sourceURI string,
) {
	obs, ok := tagObservationFromPage(generation.Boundary(), resource, sourceKind, TagPage{
		Tags:             tags,
		InheritanceState: inheritance,
		SourceURI:        sourceURI,
	})
	if ok {
		generation.AddTagObservation(obs)
	}
}

func resourceList(resources map[string]gcpcloud.ResourceObservation) []gcpcloud.ResourceObservation {
	if len(resources) == 0 {
		return nil
	}
	keys := make([]string, 0, len(resources))
	for key := range resources {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]gcpcloud.ResourceObservation, 0, len(keys))
	for _, key := range keys {
		out = append(out, resources[key])
	}
	return out
}

func resourceCollectionKey(resource gcpcloud.ResourceObservation) string {
	name := strings.TrimSpace(resource.Name)
	if name == "" {
		return ""
	}
	return name + "\x00" + strings.TrimSpace(resource.AssetType)
}
