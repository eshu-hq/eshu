package confluence

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
)

const (
	defaultPageLimit    = 100
	defaultPollInterval = 5 * time.Minute
)

// SourceConfig configures bounded Confluence documentation source syncs.
type SourceConfig struct {
	BaseURL      string
	SpaceID      string
	SpaceIDs     []string
	SpaceKey     string
	RootPageID   string
	PageLimit    int
	PollInterval time.Duration
	Email        string
	APIToken     string
	BearerToken  string
	Now          func() time.Time
}

// LoadConfig parses the Confluence collector environment contract.
func LoadConfig(getenv func(string) string) (SourceConfig, error) {
	if getenv == nil {
		return SourceConfig{}, errors.New("confluence getenv is required")
	}
	config := SourceConfig{
		BaseURL:      strings.TrimRight(strings.TrimSpace(getenv("ESHU_CONFLUENCE_BASE_URL")), "/"),
		SpaceID:      strings.TrimSpace(getenv("ESHU_CONFLUENCE_SPACE_ID")),
		SpaceKey:     strings.TrimSpace(getenv("ESHU_CONFLUENCE_SPACE_KEY")),
		RootPageID:   strings.TrimSpace(getenv("ESHU_CONFLUENCE_ROOT_PAGE_ID")),
		Email:        strings.TrimSpace(getenv("ESHU_CONFLUENCE_EMAIL")),
		APIToken:     strings.TrimSpace(getenv("ESHU_CONFLUENCE_API_TOKEN")),
		BearerToken:  strings.TrimSpace(getenv("ESHU_CONFLUENCE_BEARER_TOKEN")),
		PageLimit:    defaultPageLimit,
		PollInterval: defaultPollInterval,
	}
	if raw := strings.TrimSpace(getenv("ESHU_CONFLUENCE_SPACE_IDS")); raw != "" {
		spaceIDs, err := parseSpaceIDs(raw)
		if err != nil {
			return SourceConfig{}, err
		}
		config.SpaceIDs = spaceIDs
	}
	if raw := strings.TrimSpace(getenv("ESHU_CONFLUENCE_PAGE_LIMIT")); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit <= 0 {
			return SourceConfig{}, fmt.Errorf("ESHU_CONFLUENCE_PAGE_LIMIT must be a positive integer")
		}
		config.PageLimit = limit
	}
	if raw := strings.TrimSpace(getenv("ESHU_CONFLUENCE_POLL_INTERVAL")); raw != "" {
		interval, err := time.ParseDuration(raw)
		if err != nil || interval <= 0 {
			return SourceConfig{}, fmt.Errorf("ESHU_CONFLUENCE_POLL_INTERVAL must be a positive duration")
		}
		config.PollInterval = interval
	}
	if config.BaseURL == "" {
		return SourceConfig{}, errors.New("ESHU_CONFLUENCE_BASE_URL is required")
	}
	if err := validateBaseURL(config.BaseURL); err != nil {
		return SourceConfig{}, err
	}
	boundedModes := 0
	if config.SpaceID != "" {
		boundedModes++
	}
	if len(config.SpaceIDs) > 0 {
		boundedModes++
	}
	if config.RootPageID != "" {
		boundedModes++
	}
	if boundedModes == 0 {
		return SourceConfig{}, errors.New("ESHU_CONFLUENCE_SPACE_ID, ESHU_CONFLUENCE_SPACE_IDS, or ESHU_CONFLUENCE_ROOT_PAGE_ID is required")
	}
	if boundedModes > 1 {
		return SourceConfig{}, errors.New("configure only one of ESHU_CONFLUENCE_SPACE_ID, ESHU_CONFLUENCE_SPACE_IDS, or ESHU_CONFLUENCE_ROOT_PAGE_ID")
	}
	if config.BearerToken == "" && (config.Email == "" || config.APIToken == "") {
		return SourceConfig{}, errors.New("read-only Confluence API credentials are required")
	}
	return config, nil
}

func validateBaseURL(raw string) error {
	_, err := parseConfluenceBaseURL(raw)
	return err
}

func parseConfluenceBaseURL(raw string) (*url.URL, error) {
	parsed, err := sdk.ParseBaseURL("confluence", raw)
	if err != nil {
		return nil, err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("ESHU_CONFLUENCE_BASE_URL must use http or https")
	}
	return parsed, nil
}

func parseSpaceIDs(raw string) ([]string, error) {
	parts := strings.Split(raw, ",")
	spaceIDs := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		spaceID := strings.TrimSpace(part)
		if spaceID == "" {
			return nil, errors.New("ESHU_CONFLUENCE_SPACE_IDS must be a comma-separated list of non-empty space IDs")
		}
		if !isPositiveDecimalID(spaceID) {
			return nil, fmt.Errorf("ESHU_CONFLUENCE_SPACE_IDS contains non-numeric space ID %q", spaceID)
		}
		if _, ok := seen[spaceID]; ok {
			return nil, fmt.Errorf("ESHU_CONFLUENCE_SPACE_IDS contains duplicate space ID %q", spaceID)
		}
		seen[spaceID] = struct{}{}
		spaceIDs = append(spaceIDs, spaceID)
	}
	return spaceIDs, nil
}

func isPositiveDecimalID(value string) bool {
	nonZero := false
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
		if char != '0' {
			nonZero = true
		}
	}
	return nonZero
}

func (c SourceConfig) now() time.Time {
	if c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
}

func (c SourceConfig) boundedID(spaceID string) string {
	if spaceID != "" {
		return "space:" + spaceID
	}
	return "page-tree:" + c.RootPageID
}
