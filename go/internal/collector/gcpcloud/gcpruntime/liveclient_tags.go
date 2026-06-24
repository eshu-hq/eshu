// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"golang.org/x/oauth2"

	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud"
)

const (
	// CloudResourceManagerEndpoint is the default endpoint for Resource Manager
	// direct and effective tag APIs.
	CloudResourceManagerEndpoint = "https://cloudresourcemanager.googleapis.com"
	// DefaultLiveTagPageSize keeps Resource Manager tag pages bounded.
	DefaultLiveTagPageSize = 100
	// MaxLiveTagPageSize is the largest pageSize LiveClient sends to tag APIs.
	MaxLiveTagPageSize = 300

	tagBindingsSourceURI = "resourcemanager://tagBindings.list"
	effectiveTagsURI     = "resourcemanager://effectiveTags.list"
)

type tagBindingsListResponse struct {
	TagBindings   []tagBindingResponse `json:"tagBindings"`
	NextPageToken string               `json:"nextPageToken"`
}

type tagBindingResponse struct {
	TagValue               string `json:"tagValue"`
	TagValueNamespacedName string `json:"tagValueNamespacedName"`
}

type effectiveTagsListResponse struct {
	EffectiveTags []effectiveTagResponse `json:"effectiveTags"`
	NextPageToken string                 `json:"nextPageToken"`
}

type effectiveTagResponse struct {
	TagValue           string `json:"tagValue"`
	NamespacedTagValue string `json:"namespacedTagValue"`
	TagKey             string `json:"tagKey"`
	NamespacedTagKey   string `json:"namespacedTagKey"`
	Inherited          bool   `json:"inherited"`
}

// FetchTagPage fetches one Resource Manager direct or effective tag page.
func (c LiveClient) FetchTagPage(ctx context.Context, req TagRequest) (TagPage, error) {
	endpoint, sourceURI, err := c.tagRequestURL(req)
	if err != nil {
		return TagPage{}, err
	}
	token, err := c.token()
	if err != nil {
		return TagPage{}, err
	}

	attempts := c.maxAttempts()
	for attempt := 1; attempt <= attempts; attempt++ {
		page, retryable, err := c.fetchTagAttempt(ctx, endpoint, sourceURI, req.SourceKind, token)
		if err == nil {
			return page, nil
		}
		if !retryable || attempt == attempts {
			return TagPage{}, err
		}
		if sleepErr := c.sleep(ctx, c.backoff(attempt)); sleepErr != nil {
			return TagPage{}, sleepErr
		}
	}
	return TagPage{}, ProviderWarning{
		WarningKind: gcpcloud.WarningKindUnavailable,
		Outcome:     gcpcloud.OutcomeUnavailable,
		Reason:      "resource manager tag retry attempts exhausted",
		Retryable:   true,
		SourceURI:   sourceURI,
	}
}

func (c LiveClient) fetchTagAttempt(
	ctx context.Context,
	endpoint string,
	sourceURI string,
	sourceKind string,
	token *oauth2.Token,
) (TagPage, bool, error) {
	attemptCtx, cancel := context.WithTimeout(ctx, c.requestTimeout())
	defer cancel()
	httpReq, err := http.NewRequestWithContext(attemptCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return TagPage{}, false, errors.New("build gcp live tag request")
	}
	token.SetAuthHeader(httpReq)

	resp, err := c.httpClient().Do(httpReq)
	if err != nil {
		if attemptCtx.Err() != nil {
			return TagPage{}, false, attemptCtx.Err()
		}
		return TagPage{}, true, ProviderWarning{
			WarningKind: gcpcloud.WarningKindUnavailable,
			Outcome:     gcpcloud.OutcomeUnavailable,
			Reason:      "resource manager tag transport unavailable",
			Retryable:   true,
			SourceURI:   sourceURI,
		}
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := readBounded(resp.Body, c.maxResponseBytes())
	if err != nil {
		return TagPage{}, false, ProviderWarning{
			WarningKind: gcpcloud.WarningKindRedaction,
			Outcome:     gcpcloud.OutcomeUnavailable,
			Reason:      "resource manager tag response exceeded size limit",
			SourceURI:   sourceURI,
		}
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		warning := classifyTagStatus(resp.StatusCode, sourceURI)
		return TagPage{}, warning.Retryable, warning
	}
	page, err := parseTagPage(body, sourceKind, sourceURI)
	if err != nil {
		return TagPage{}, false, fmt.Errorf("parse gcp live tag response: %w", err)
	}
	return page, false, nil
}

func (c LiveClient) tagRequestURL(req TagRequest) (string, string, error) {
	fullName := strings.TrimSpace(req.FullResourceName)
	if fullName == "" {
		return "", "", errors.New("gcp live tag full resource name is required")
	}
	base := strings.TrimRight(firstNonEmpty(c.ResourceManagerEndpoint, CloudResourceManagerEndpoint), "/")
	values := url.Values{}
	values.Set("parent", fullName)
	values.Set("pageSize", strconv.Itoa(c.tagPageSize()))
	if token := strings.TrimSpace(req.PageToken); token != "" {
		values.Set("pageToken", token)
	}
	switch strings.TrimSpace(req.SourceKind) {
	case TagSourceKindDirect:
		return base + "/v3/tagBindings?" + values.Encode(), tagBindingsSourceURI, nil
	case TagSourceKindEffective:
		return base + "/v3/effectiveTags?" + values.Encode(), effectiveTagsURI, nil
	default:
		return "", "", ProviderWarning{
			WarningKind: gcpcloud.WarningKindUnsupported,
			Outcome:     gcpcloud.OutcomeUnsupported,
			Reason:      "resource manager tag source unsupported",
			SourceURI:   "resourcemanager://unknown",
		}
	}
}

func parseTagPage(body []byte, sourceKind string, sourceURI string) (TagPage, error) {
	switch strings.TrimSpace(sourceKind) {
	case TagSourceKindDirect:
		var response tagBindingsListResponse
		if err := json.Unmarshal(body, &response); err != nil {
			return TagPage{}, err
		}
		tags := make(map[string]string, len(response.TagBindings))
		states := make(map[string]string, len(response.TagBindings))
		for _, binding := range response.TagBindings {
			key, value, ok := splitNamespacedTagValue(binding.TagValueNamespacedName)
			if !ok {
				continue
			}
			tags[key] = value
			states[key] = tagInheritanceDirect
		}
		return TagPage{
			Tags:             tags,
			InheritanceState: states,
			NextPageToken:    strings.TrimSpace(response.NextPageToken),
			SourceURI:        sourceURI,
		}, nil
	case TagSourceKindEffective:
		var response effectiveTagsListResponse
		if err := json.Unmarshal(body, &response); err != nil {
			return TagPage{}, err
		}
		tags := make(map[string]string, len(response.EffectiveTags))
		states := make(map[string]string, len(response.EffectiveTags))
		for _, tag := range response.EffectiveTags {
			key, value, ok := effectiveTagKeyValue(tag)
			if !ok {
				continue
			}
			tags[key] = value
			if tag.Inherited {
				states[key] = tagInheritanceInherited
			} else {
				states[key] = tagInheritanceDirect
			}
		}
		return TagPage{
			Tags:             tags,
			InheritanceState: states,
			NextPageToken:    strings.TrimSpace(response.NextPageToken),
			SourceURI:        sourceURI,
		}, nil
	default:
		return TagPage{}, ProviderWarning{
			WarningKind: gcpcloud.WarningKindUnsupported,
			Outcome:     gcpcloud.OutcomeUnsupported,
			Reason:      "resource manager tag source unsupported",
			SourceURI:   sourceURI,
		}
	}
}

func effectiveTagKeyValue(tag effectiveTagResponse) (string, string, bool) {
	key := strings.TrimSpace(firstNonEmpty(tag.NamespacedTagKey, tag.TagKey))
	value := strings.TrimSpace(tag.NamespacedTagValue)
	if key == "" || value == "" {
		return "", "", false
	}
	prefix := key + "/"
	if strings.HasPrefix(value, prefix) {
		value = strings.TrimSpace(strings.TrimPrefix(value, prefix))
	}
	if value == "" {
		return "", "", false
	}
	return key, value, true
}

func splitNamespacedTagValue(namespaced string) (string, string, bool) {
	parts := strings.Split(strings.TrimSpace(namespaced), "/")
	if len(parts) < 3 {
		return "", "", false
	}
	value := strings.TrimSpace(parts[len(parts)-1])
	key := strings.TrimSpace(strings.Join(parts[:len(parts)-1], "/"))
	if key == "" || value == "" {
		return "", "", false
	}
	return key, value, true
}

func classifyTagStatus(status int, sourceURI string) ProviderWarning {
	warning := classifyLiveStatus(status)
	warning.SourceURI = sourceURI
	switch warning.WarningKind {
	case gcpcloud.WarningKindPartialPermission:
		if status == http.StatusForbidden {
			warning.Reason = "resource manager tag permission denied"
		} else {
			warning.Reason = "credential token rejected by resource manager"
		}
	case gcpcloud.WarningKindUnsupported:
		warning.Reason = "resource manager tag request unsupported"
	case gcpcloud.WarningKindQuota:
		warning.Reason = "resource manager tag throttle exhausted"
	default:
		warning.Reason = "resource manager tag source unavailable"
	}
	return warning
}

func (c LiveClient) tagPageSize() int {
	switch {
	case c.TagPageSize <= 0:
		return DefaultLiveTagPageSize
	case c.TagPageSize > MaxLiveTagPageSize:
		return MaxLiveTagPageSize
	default:
		return c.TagPageSize
	}
}

var _ TagProvider = LiveClient{}
