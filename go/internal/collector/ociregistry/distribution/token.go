package distribution

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// TokenConfig configures an OCI Distribution bearer-token request.
type TokenConfig struct {
	Realm    string
	Service  string
	Scope    string
	Username string
	Password string
	Client   *http.Client
}

// FetchBearerToken requests a bearer token from a Distribution token service.
func FetchBearerToken(ctx context.Context, config TokenConfig) (string, error) {
	realm := strings.TrimSpace(config.Realm)
	if realm == "" {
		return "", fmt.Errorf("oci token realm is required")
	}
	requestURL, err := url.Parse(realm)
	if err != nil {
		return "", fmt.Errorf("parse oci token realm: %w", err)
	}
	query := requestURL.Query()
	if service := strings.TrimSpace(config.Service); service != "" {
		query.Set("service", service)
	}
	if scope := strings.TrimSpace(config.Scope); scope != "" {
		query.Set("scope", scope)
	}
	requestURL.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return "", fmt.Errorf("build OCI token request: %w", err)
	}
	if config.Username != "" || config.Password != "" {
		req.SetBasicAuth(config.Username, config.Password)
	}

	client := config.Client
	if client == nil {
		client = &http.Client{Timeout: defaultHTTPTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("OCI token request failed: %w", err)
	}
	defer closeBody(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", statusError(http.MethodGet, requestURL.Path, resp.StatusCode)
	}

	var decoded struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return "", fmt.Errorf("decode OCI token response: %w", err)
	}
	if token := strings.TrimSpace(decoded.Token); token != "" {
		return token, nil
	}
	if token := strings.TrimSpace(decoded.AccessToken); token != "" {
		return token, nil
	}
	return "", fmt.Errorf("OCI token response did not include a token")
}
