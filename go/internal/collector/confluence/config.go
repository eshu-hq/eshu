package confluence

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const defaultPageLimit = 100
const defaultPollInterval = 5 * time.Minute

// SourceConfig configures one bounded Confluence documentation source sync.
type SourceConfig struct {
	BaseURL      string
	SpaceID      string
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
	if config.SpaceID == "" && config.RootPageID == "" {
		return SourceConfig{}, errors.New("ESHU_CONFLUENCE_SPACE_ID or ESHU_CONFLUENCE_ROOT_PAGE_ID is required")
	}
	if config.SpaceID != "" && config.RootPageID != "" {
		return SourceConfig{}, errors.New("configure only one of ESHU_CONFLUENCE_SPACE_ID or ESHU_CONFLUENCE_ROOT_PAGE_ID")
	}
	if config.BearerToken == "" && (config.Email == "" || config.APIToken == "") {
		return SourceConfig{}, errors.New("read-only Confluence API credentials are required")
	}
	return config, nil
}

func validateBaseURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("parse ESHU_CONFLUENCE_BASE_URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("ESHU_CONFLUENCE_BASE_URL must use http or https")
	}
	if strings.TrimSpace(parsed.Host) == "" {
		return errors.New("ESHU_CONFLUENCE_BASE_URL must include a host")
	}
	return nil
}

func (c SourceConfig) now() time.Time {
	if c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
}

func (c SourceConfig) boundedID() string {
	if c.SpaceID != "" {
		return "space:" + c.SpaceID
	}
	return "page-tree:" + c.RootPageID
}
