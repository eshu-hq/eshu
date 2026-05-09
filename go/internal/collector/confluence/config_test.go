package confluence

import (
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
