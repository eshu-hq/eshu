// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package distribution

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

const (
	maxTagListLimit      = 100
	maxTagListPages      = 32
	maxTagListPageBytes  = 64 << 10
	maxTagListTotalBytes = 1 << 20
)

// TagListResponse contains the bounded unique tags observed for a repository.
// Complete is false when the registry advertised another page, returned more
// than the requested limit, or supplied an unsafe or non-progressing
// continuation.
type TagListResponse struct {
	Tags     []string
	Complete bool
}

// ListTags returns at most limit+1 unique registry-reported tags for one
// repository and follows safe OCI rel=next links while the result stays below
// the caller's limit.
func (c *Client) ListTags(ctx context.Context, repository string, limit int) (TagListResponse, error) {
	if limit <= 0 || limit > maxTagListLimit {
		return TagListResponse{}, fmt.Errorf("OCI tag list limit must be between 1 and %d", maxTagListLimit)
	}
	endpoint := "/v2/" + repositoryPath(repository) + "/tags/list"
	requestURL := c.resolve(endpoint)
	requestURL.RawQuery = "n=" + strconv.Itoa(limit+1)

	var tags []string
	var visitedURLs []string
	maxDecodedTags := maxTagListPages * (limit + 1)
	decodedTags := 0
	readBytes := int64(0)

	for page := 0; page < maxTagListPages; page++ {
		resp, err := c.doURL(ctx, "list_tags", http.MethodGet, requestURL, nil)
		if err != nil {
			return TagListResponse{}, err
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			closeBody(resp.Body)
			return TagListResponse{}, statusError("list_tags", resp)
		}
		pageTags, pageBytes, oversized, decodeErr := decodeTagListPage(resp.Body)
		closeBody(resp.Body)
		if decodeErr != nil {
			return TagListResponse{}, decodeErr
		}
		readBytes += pageBytes
		if oversized || readBytes > maxTagListTotalBytes {
			return TagListResponse{Tags: tags, Complete: false}, nil
		}

		remainingDecodedTags := maxDecodedTags - decodedTags
		if remainingDecodedTags <= 0 {
			return TagListResponse{Tags: tags, Complete: false}, nil
		}
		cumulativeTagLimitReached := len(pageTags) >= remainingDecodedTags
		if len(pageTags) > remainingDecodedTags {
			pageTags = pageTags[:remainingDecodedTags]
		}
		decodedTags += len(pageTags)
		slices.Sort(pageTags)
		pageTags = slices.Compact(pageTags)
		before := len(tags)
		if page == 0 {
			tags = pageTags
		} else {
			for _, tag := range pageTags {
				if slices.Contains(tags, tag) {
					continue
				}
				tags = append(tags, tag)
			}
		}
		slices.Sort(tags)
		if len(tags) > limit {
			return TagListResponse{Tags: tags[:limit+1], Complete: false}, nil
		}
		if cumulativeTagLimitReached {
			return TagListResponse{Tags: tags, Complete: false}, nil
		}

		nextReference, hasNext, validLink := nextTagListReference(resp.Header)
		if !validLink {
			return TagListResponse{Tags: tags, Complete: false}, nil
		}
		if !hasNext {
			return TagListResponse{Tags: tags, Complete: true}, nil
		}
		if len(tags) >= limit || len(tags) == before {
			return TagListResponse{Tags: tags, Complete: false}, nil
		}
		nextURL, validNext := c.safeTagListNextURL(requestURL, endpoint, nextReference)
		if !validNext {
			return TagListResponse{Tags: tags, Complete: false}, nil
		}
		if len(visitedURLs) == 0 {
			visitedURLs = append(visitedURLs, requestURL.String())
		}
		nextURLString := nextURL.String()
		if slices.Contains(visitedURLs, nextURLString) {
			return TagListResponse{Tags: tags, Complete: false}, nil
		}
		visitedURLs = append(visitedURLs, nextURLString)
		requestURL = nextURL
	}

	return TagListResponse{Tags: tags, Complete: false}, nil
}

func decodeTagListPage(body io.Reader) ([]string, int64, bool, error) {
	limited := &io.LimitedReader{R: body, N: maxTagListPageBytes + 1}
	var decoded struct {
		Tags []string `json:"tags"`
	}
	decoder := json.NewDecoder(limited)
	decodeErr := decoder.Decode(&decoded)
	var trailing any
	trailingErr := io.EOF
	if decodeErr == nil {
		trailingErr = decoder.Decode(&trailing)
	}
	if _, err := io.Copy(io.Discard, limited); err != nil {
		return nil, 0, false, fmt.Errorf("read OCI tag list: %w", err)
	}
	readBytes := maxTagListPageBytes + 1 - limited.N
	if readBytes > maxTagListPageBytes {
		return nil, readBytes, true, nil
	}
	if decodeErr != nil {
		return nil, readBytes, false, fmt.Errorf("decode OCI tag list: %w", decodeErr)
	}
	if trailingErr != io.EOF {
		if trailingErr != nil {
			return nil, readBytes, false, fmt.Errorf(
				"decode OCI tag list trailing content: %w",
				trailingErr,
			)
		}
		return nil, readBytes, false, fmt.Errorf(
			"decode OCI tag list: multiple JSON values",
		)
	}
	return decoded.Tags, readBytes, false, nil
}

func nextTagListReference(header http.Header) (string, bool, bool) {
	values := header.Values("Link")
	if len(values) == 0 {
		return "", false, true
	}
	for _, value := range values {
		links, ok := splitLinkHeader(value)
		if !ok {
			return "", false, false
		}
		for _, link := range links {
			closeBracket := strings.IndexByte(link, '>')
			if !strings.HasPrefix(link, "<") || closeBracket < 2 {
				return "", false, false
			}
			reference := link[1:closeBracket]
			mediaType, params, err := mime.ParseMediaType("application/x-link" + link[closeBracket+1:])
			if err != nil || mediaType != "application/x-link" {
				return "", false, false
			}
			for _, relation := range strings.Fields(params["rel"]) {
				if relation == "next" {
					return reference, true, true
				}
			}
		}
	}
	return "", false, true
}

func splitLinkHeader(value string) ([]string, bool) {
	var links []string
	start := 0
	inAngle := false
	inQuote := false
	escaped := false
	for index, character := range value {
		switch {
		case escaped:
			escaped = false
		case character == '\\' && inQuote:
			escaped = true
		case character == '"' && !inAngle:
			inQuote = !inQuote
		case character == '<' && !inQuote:
			if inAngle {
				return nil, false
			}
			inAngle = true
		case character == '>' && !inQuote:
			if !inAngle {
				return nil, false
			}
			inAngle = false
		case character == ',' && !inAngle && !inQuote:
			link := strings.TrimSpace(value[start:index])
			if link == "" {
				return nil, false
			}
			links = append(links, link)
			start = index + 1
		}
	}
	if inAngle || inQuote || escaped {
		return nil, false
	}
	last := strings.TrimSpace(value[start:])
	if last == "" {
		return nil, false
	}
	return append(links, last), true
}

func (c *Client) safeTagListNextURL(currentURL url.URL, endpoint, reference string) (url.URL, bool) {
	parsed, err := url.Parse(reference)
	if err != nil {
		return url.URL{}, false
	}
	next := currentURL.ResolveReference(parsed)
	expected := c.resolve(endpoint)
	if next.Scheme != c.baseURL.Scheme ||
		next.Host != c.baseURL.Host ||
		next.User != nil ||
		next.Fragment != "" ||
		next.EscapedPath() != expected.EscapedPath() {
		return url.URL{}, false
	}
	return *next, true
}
