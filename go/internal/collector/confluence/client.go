package confluence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/metric"
)

const defaultHTTPTimeout = 30 * time.Second

// ErrPermissionDenied marks a Confluence page that the read-only credential
// cannot view.
var ErrPermissionDenied = errors.New("confluence permission denied")

// Client reads bounded Confluence source evidence.
type Client interface {
	GetSpace(context.Context, string) (Space, error)
	ListSpacePages(context.Context, string, int) ([]Page, error)
	ListPageTree(context.Context, string, int) ([]string, error)
	GetPage(context.Context, string) (Page, error)
}

// HTTPClientConfig configures the read-only Confluence HTTP client.
type HTTPClientConfig struct {
	BaseURL     string
	Email       string
	APIToken    string
	BearerToken string
	Client      *http.Client
	Instruments *telemetry.Instruments
}

// HTTPClient is a read-only Confluence Cloud REST API v2 client.
type HTTPClient struct {
	baseURL     *url.URL
	email       string
	apiToken    string
	bearerToken string
	client      *http.Client
	instruments *telemetry.Instruments
}

// NewHTTPClient creates a read-only Confluence HTTP client.
func NewHTTPClient(config HTTPClientConfig) (*HTTPClient, error) {
	if strings.TrimSpace(config.BaseURL) == "" {
		return nil, errors.New("confluence base URL is required")
	}
	if err := validateBaseURL(strings.TrimRight(strings.TrimSpace(config.BaseURL), "/")); err != nil {
		return nil, err
	}
	if strings.TrimSpace(config.BearerToken) == "" &&
		(strings.TrimSpace(config.Email) == "" || strings.TrimSpace(config.APIToken) == "") {
		return nil, errors.New("read-only Confluence API credentials are required")
	}
	baseURL, err := url.Parse(strings.TrimRight(config.BaseURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("parse confluence base URL: %w", err)
	}
	client := config.Client
	if client == nil {
		client = &http.Client{Timeout: defaultHTTPTimeout}
	}
	return &HTTPClient{
		baseURL:     baseURL,
		email:       config.Email,
		apiToken:    config.APIToken,
		bearerToken: config.BearerToken,
		client:      client,
		instruments: config.Instruments,
	}, nil
}

// GetSpace reads one Confluence space.
func (c *HTTPClient) GetSpace(ctx context.Context, id string) (Space, error) {
	var space Space
	if err := c.getJSON(ctx, "/api/v2/spaces/"+url.PathEscape(id), nil, &space); err != nil {
		return Space{}, err
	}
	return space, nil
}

// ListSpacePages reads current pages visible in a Confluence space.
func (c *HTTPClient) ListSpacePages(ctx context.Context, spaceID string, limit int) ([]Page, error) {
	values := url.Values{}
	values.Set("body-format", "storage")
	values.Set("status", "current")
	values.Set("limit", strconv.Itoa(limit))

	var out []Page
	endpoint := "/api/v2/spaces/" + url.PathEscape(spaceID) + "/pages"
	for endpoint != "" {
		var response pageListResponse
		if err := c.getJSON(ctx, endpoint, values, &response); err != nil {
			return nil, err
		}
		out = append(out, response.Results...)
		endpoint = response.Links.Next
		values = nil
	}
	return out, nil
}

// ListPageTree returns the root page ID and descendant page IDs.
func (c *HTTPClient) ListPageTree(ctx context.Context, rootPageID string, limit int) ([]string, error) {
	values := url.Values{}
	values.Set("limit", strconv.Itoa(limit))

	ids := []string{rootPageID}
	endpoint := "/api/v2/pages/" + url.PathEscape(rootPageID) + "/descendants"
	for endpoint != "" {
		var response struct {
			Results []struct {
				ID   string `json:"id"`
				Type string `json:"type"`
			} `json:"results"`
			Links Links `json:"_links"`
		}
		if err := c.getJSON(ctx, endpoint, values, &response); err != nil {
			return nil, err
		}
		for _, result := range response.Results {
			if strings.EqualFold(result.Type, "page") && strings.TrimSpace(result.ID) != "" {
				ids = append(ids, result.ID)
			}
		}
		endpoint = response.Links.Next
		values = nil
	}
	return ids, nil
}

// GetPage reads one Confluence page with body and labels.
func (c *HTTPClient) GetPage(ctx context.Context, id string) (Page, error) {
	values := url.Values{}
	values.Set("body-format", "storage")
	values.Set("include-labels", "true")
	values.Set("include-version", "true")
	var page Page
	if err := c.getJSON(ctx, "/api/v2/pages/"+url.PathEscape(id), values, &page); err != nil {
		return Page{}, err
	}
	return page, nil
}

func (c *HTTPClient) getJSON(ctx context.Context, endpoint string, query url.Values, target any) error {
	requestURL := c.resolve(endpoint)
	operation := confluenceOperation(requestURL.Path)
	if query != nil {
		requestURL.RawQuery = query.Encode()
	}
	startedAt := time.Now()
	statusClass := "transport_error"
	result := "network_error"
	defer func() {
		c.recordRequest(ctx, operation, result, statusClass, time.Since(startedAt))
	}()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		result = "request_build_error"
		return fmt.Errorf("build confluence request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if c.bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.bearerToken)
	} else {
		req.SetBasicAuth(c.email, c.apiToken)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("confluence GET %s: %w", requestURL.Path, err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	statusClass = httpStatusClass(resp.StatusCode)
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound {
		result = "permission_denied"
		return ErrPermissionDenied
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		result = "status_error"
		return fmt.Errorf("confluence GET %s returned status %d", requestURL.Path, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		result = "decode_error"
		return fmt.Errorf("decode confluence response: %w", err)
	}
	result = "success"
	return nil
}

func (c *HTTPClient) recordRequest(
	ctx context.Context,
	operation string,
	result string,
	statusClass string,
	duration time.Duration,
) {
	if c.instruments == nil {
		return
	}
	if c.instruments.ConfluenceHTTPRequests != nil {
		c.instruments.ConfluenceHTTPRequests.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrOperation(operation),
			telemetry.AttrResult(result),
			telemetry.AttrStatusClass(statusClass),
		))
	}
	if c.instruments.ConfluenceFetchDuration != nil {
		c.instruments.ConfluenceFetchDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(
			telemetry.AttrOperation(operation),
			telemetry.AttrResult(result),
		))
	}
}

func (c *HTTPClient) resolve(endpoint string) url.URL {
	if strings.HasPrefix(endpoint, "http://") || strings.HasPrefix(endpoint, "https://") {
		parsed, err := url.Parse(endpoint)
		if err == nil {
			return *parsed
		}
	}
	resolved := *c.baseURL
	relative, err := url.Parse(endpoint)
	if err != nil {
		resolved.Path = c.resolvePath(endpoint)
		return resolved
	}
	resolved.Path = c.resolvePath(relative.Path)
	resolved.RawQuery = relative.RawQuery
	resolved.Fragment = relative.Fragment
	return resolved
}

func (c *HTTPClient) resolvePath(endpointPath string) string {
	if endpointPath == "" {
		return c.baseURL.Path
	}
	basePath := cleanURLPath(c.baseURL.Path)
	resolvedPath := cleanURLPath(endpointPath)
	if basePath == "/" {
		return resolvedPath
	}
	if resolvedPath == basePath || strings.HasPrefix(resolvedPath, basePath+"/") {
		return resolvedPath
	}
	return path.Join(basePath, resolvedPath)
}

func cleanURLPath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "/"
	}
	return path.Clean("/" + strings.TrimPrefix(value, "/"))
}

func confluenceOperation(requestPath string) string {
	cleaned := cleanURLPath(requestPath)
	switch {
	case strings.HasSuffix(cleaned, "/descendants"):
		return "list_page_tree"
	case strings.HasSuffix(cleaned, "/pages"):
		return "list_pages"
	case strings.Contains(cleaned, "/pages/"):
		return "fetch_page"
	case strings.Contains(cleaned, "/spaces/"):
		return "get_space"
	default:
		return "unknown"
	}
}

func httpStatusClass(statusCode int) string {
	if statusCode < 100 {
		return "unknown"
	}
	return strconv.Itoa(statusCode/100) + "xx"
}
