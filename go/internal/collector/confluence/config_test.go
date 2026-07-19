// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package confluence

import (
	"strings"
	"testing"
	"time"
)

func TestLoadConfigRequiresBoundedScopeAndReadOnlyCredentials(t *testing.T) {
	t.Parallel()

	_, err := LoadConfig(func(key string) string {
		values := map[string]string{
			"ESHU_CONFLUENCE_BASE_URL":  "https://example.atlassian.net/wiki",
			"ESHU_CONFLUENCE_API_TOKEN": "token",
			"ESHU_CONFLUENCE_EMAIL":     "bot@example.com",
		}
		return values[key]
	})
	if err == nil {
		t.Fatal("LoadConfig() error = nil, want missing bounded scope error")
	}

	_, err = LoadConfig(func(key string) string {
		values := map[string]string{
			"ESHU_CONFLUENCE_BASE_URL": "https://example.atlassian.net/wiki",
			"ESHU_CONFLUENCE_SPACE_ID": "100",
		}
		return values[key]
	})
	if err == nil {
		t.Fatal("LoadConfig() error = nil, want missing read-only credential error")
	}

	config, err := LoadConfig(func(key string) string {
		values := map[string]string{
			"ESHU_CONFLUENCE_BASE_URL":      "https://example.atlassian.net/wiki",
			"ESHU_CONFLUENCE_SPACE_ID":      "100",
			"ESHU_CONFLUENCE_SPACE_KEY":     "PLAT",
			"ESHU_CONFLUENCE_API_TOKEN":     "token",
			"ESHU_CONFLUENCE_EMAIL":         "bot@example.com",
			"ESHU_CONFLUENCE_PAGE_LIMIT":    "75",
			"ESHU_CONFLUENCE_POLL_INTERVAL": "15m",
		}
		return values[key]
	})
	if err != nil {
		t.Fatalf("LoadConfig() error = %v, want nil", err)
	}
	if got, want := config.SpaceID, "100"; got != want {
		t.Fatalf("SpaceID = %q, want %q", got, want)
	}
	if got, want := config.PageLimit, 75; got != want {
		t.Fatalf("PageLimit = %d, want %d", got, want)
	}
	if got, want := config.PollInterval, 15*time.Minute; got != want {
		t.Fatalf("PollInterval = %v, want %v", got, want)
	}
}

func TestLoadConfigResolvesMaxTotalPages(t *testing.T) {
	t.Parallel()

	config, err := LoadConfig(func(key string) string {
		values := map[string]string{
			"ESHU_CONFLUENCE_BASE_URL":        "https://example.atlassian.net/wiki",
			"ESHU_CONFLUENCE_SPACE_ID":        "100",
			"ESHU_CONFLUENCE_API_TOKEN":       "token",
			"ESHU_CONFLUENCE_EMAIL":           "bot@example.com",
			"ESHU_CONFLUENCE_MAX_TOTAL_PAGES": "2500",
		}
		return values[key]
	})
	if err != nil {
		t.Fatalf("LoadConfig() error = %v, want nil", err)
	}
	if got, want := config.MaxTotalPages, 2500; got != want {
		t.Fatalf("MaxTotalPages = %d, want %d", got, want)
	}
}

func TestLoadConfigDefaultsMaxTotalPagesWhenUnset(t *testing.T) {
	t.Parallel()

	config, err := LoadConfig(func(key string) string {
		values := map[string]string{
			"ESHU_CONFLUENCE_BASE_URL":  "https://example.atlassian.net/wiki",
			"ESHU_CONFLUENCE_SPACE_ID":  "100",
			"ESHU_CONFLUENCE_API_TOKEN": "token",
			"ESHU_CONFLUENCE_EMAIL":     "bot@example.com",
		}
		return values[key]
	})
	if err != nil {
		t.Fatalf("LoadConfig() error = %v, want nil", err)
	}
	if got, want := config.MaxTotalPages, defaultMaxTotalPages; got != want {
		t.Fatalf("MaxTotalPages = %d, want default %d", got, want)
	}
}

func TestLoadConfigRejectsInvalidMaxTotalPages(t *testing.T) {
	t.Parallel()

	tests := []string{"0", "-1", "not-a-number"}
	for _, value := range tests {
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			_, err := LoadConfig(func(key string) string {
				values := map[string]string{
					"ESHU_CONFLUENCE_BASE_URL":        "https://example.atlassian.net/wiki",
					"ESHU_CONFLUENCE_SPACE_ID":        "100",
					"ESHU_CONFLUENCE_API_TOKEN":       "token",
					"ESHU_CONFLUENCE_EMAIL":           "bot@example.com",
					"ESHU_CONFLUENCE_MAX_TOTAL_PAGES": value,
				}
				return values[key]
			})
			if err == nil {
				t.Fatal("LoadConfig() error = nil, want invalid max_total_pages error")
			}
		})
	}
}

func TestLoadConfigAcceptsExplicitSpaceIDAllowlist(t *testing.T) {
	t.Parallel()

	config, err := LoadConfig(func(key string) string {
		values := map[string]string{
			"ESHU_CONFLUENCE_BASE_URL":      "https://example.atlassian.net/wiki",
			"ESHU_CONFLUENCE_SPACE_IDS":     "100, 200,300",
			"ESHU_CONFLUENCE_API_TOKEN":     "token",
			"ESHU_CONFLUENCE_EMAIL":         "bot@example.com",
			"ESHU_CONFLUENCE_POLL_INTERVAL": "15m",
		}
		return values[key]
	})
	if err != nil {
		t.Fatalf("LoadConfig() error = %v, want nil", err)
	}
	if got, want := config.SpaceIDs, []string{"100", "200", "300"}; !equalStrings(got, want) {
		t.Fatalf("SpaceIDs = %#v, want %#v", got, want)
	}
	if got, want := config.PollInterval, 15*time.Minute; got != want {
		t.Fatalf("PollInterval = %v, want %v", got, want)
	}
}

func TestLoadConfigRejectsInvalidSpaceIDAllowlist(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		values map[string]string
	}{
		{
			name: "empty list entry",
			values: map[string]string{
				"ESHU_CONFLUENCE_SPACE_IDS": "100,,200",
			},
		},
		{
			name: "duplicate id",
			values: map[string]string{
				"ESHU_CONFLUENCE_SPACE_IDS": "100,200,100",
			},
		},
		{
			name: "non numeric id",
			values: map[string]string{
				"ESHU_CONFLUENCE_SPACE_IDS": "100,DEV",
			},
		},
		{
			name: "zero id",
			values: map[string]string{
				"ESHU_CONFLUENCE_SPACE_IDS": "100,0",
			},
		},
		{
			name: "mixed single and list",
			values: map[string]string{
				"ESHU_CONFLUENCE_SPACE_ID":  "100",
				"ESHU_CONFLUENCE_SPACE_IDS": "200,300",
			},
		},
		{
			name: "mixed root page and list",
			values: map[string]string{
				"ESHU_CONFLUENCE_ROOT_PAGE_ID": "root",
				"ESHU_CONFLUENCE_SPACE_IDS":    "200,300",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := LoadConfig(func(key string) string {
				values := map[string]string{
					"ESHU_CONFLUENCE_BASE_URL":  "https://example.atlassian.net/wiki",
					"ESHU_CONFLUENCE_API_TOKEN": "token",
					"ESHU_CONFLUENCE_EMAIL":     "bot@example.com",
				}
				for k, v := range tt.values {
					values[k] = v
				}
				return values[key]
			})
			if err == nil {
				t.Fatal("LoadConfig() error = nil, want invalid allowlist error")
			}
		})
	}
}

func TestLoadConfigRejectsInvalidBaseURL(t *testing.T) {
	t.Parallel()

	_, err := LoadConfig(func(key string) string {
		values := map[string]string{
			"ESHU_CONFLUENCE_BASE_URL":     "ftp://example.atlassian.net/wiki",
			"ESHU_CONFLUENCE_SPACE_ID":     "100",
			"ESHU_CONFLUENCE_BEARER_TOKEN": "token",
		}
		return values[key]
	})
	if err == nil {
		t.Fatal("LoadConfig() error = nil, want invalid base URL error")
	}
}

func TestLoadConfigRejectsCredentialBearingBaseURL(t *testing.T) {
	t.Parallel()

	_, err := LoadConfig(func(key string) string {
		values := map[string]string{
			"ESHU_CONFLUENCE_BASE_URL":     "https://user:secret@example.atlassian.net/wiki",
			"ESHU_CONFLUENCE_SPACE_ID":     "100",
			"ESHU_CONFLUENCE_BEARER_TOKEN": "token",
		}
		return values[key]
	})
	if err == nil {
		t.Fatal("LoadConfig() error = nil, want credential-bearing base URL error")
	}
	if strings.Contains(err.Error(), "secret") {
		t.Fatalf("LoadConfig() error leaked credential: %q", err)
	}
}

func TestDocumentationSourceIDIncludesTenant(t *testing.T) {
	t.Parallel()

	spaceValue := Space{ID: "100", Key: "PLAT"}
	first := documentationSourceID(
		SourceConfig{BaseURL: "https://first.atlassian.net/wiki", SpaceID: "100"},
		spaceValue,
	)
	second := documentationSourceID(
		SourceConfig{BaseURL: "https://second.atlassian.net/wiki", SpaceID: "100"},
		spaceValue,
	)
	if first == second {
		t.Fatalf("documentationSourceID() collided across tenants: %q", first)
	}
}
