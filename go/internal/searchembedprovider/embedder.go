package searchembedprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/semanticprofile"
)

const embeddingsPath = "/v1/embeddings"

var errUnsupportedCredentialSource = errors.New("unsupported credential source")

var supportedEmbeddingProviderKinds = []string{
	semanticprofile.ProviderOpenAICompatible,
	semanticprofile.ProviderInternalGateway,
}

// Embedder calls an approved OpenAI-compatible embeddings endpoint.
type Embedder struct {
	profileID  string
	modelID    string
	endpoint   string
	credential string
	dimensions int
	httpClient *http.Client
	cloudAuth  bool
}

// New validates profile and returns a provider-backed search embedder.
func New(
	profile semanticprofile.ProviderProfile,
	getenv func(string) string,
	client *http.Client,
) (*Embedder, error) {
	profile = normalizeProfile(profile)
	if !slices.Contains(supportedEmbeddingProviderKinds, profile.ProviderKind) {
		return nil, fmt.Errorf("search embedder profile %q provider_kind %q is not supported for %s transport", profile.ProfileID, profile.ProviderKind, embeddingsPath)
	}
	if !slices.Contains(profile.SourceClasses, semanticprofile.SourceSearchDocuments) {
		return nil, fmt.Errorf("search embedder profile %q must include %s", profile.ProfileID, semanticprofile.SourceSearchDocuments)
	}
	if !profile.SourcePolicyConfigured {
		return nil, fmt.Errorf("search embedder profile %q requires source policy configured", profile.ProfileID)
	}
	if profile.ModelID == "" {
		return nil, fmt.Errorf("search embedder profile %q requires model_id", profile.ProfileID)
	}
	if profile.EndpointProfileID == "" {
		return nil, fmt.Errorf("search embedder profile %q requires endpoint_profile_id", profile.ProfileID)
	}
	endpoint, err := endpointURL(profile.EndpointProfileID)
	if err != nil {
		return nil, fmt.Errorf("search embedder profile %q endpoint_profile_id is invalid", profile.ProfileID)
	}
	if profile.EmbeddingDimensions <= 0 {
		return nil, fmt.Errorf("search embedder profile %q requires positive embedding_dimensions", profile.ProfileID)
	}
	credential, cloudAuth, err := resolveCredential(profile.CredentialSource, getenv)
	if err != nil {
		return nil, fmt.Errorf("search embedder profile %q: %w", profile.ProfileID, err)
	}
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	return &Embedder{
		profileID:  profile.ProfileID,
		modelID:    profile.ModelID,
		endpoint:   endpoint,
		credential: credential,
		dimensions: profile.EmbeddingDimensions,
		httpClient: client,
		cloudAuth:  cloudAuth,
	}, nil
}

// Dimensions returns the fixed vector width declared by the approved profile.
func (e *Embedder) Dimensions() int {
	if e == nil {
		return 0
	}
	return e.dimensions
}

// Embed calls the configured provider and returns one embedding vector.
func (e *Embedder) Embed(ctx context.Context, text string) ([]float64, error) {
	if e == nil {
		return nil, errors.New("search embedder is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	body, err := json.Marshal(embeddingRequest{
		Model: e.modelID,
		Input: text,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal embedding request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("build embedding request failed")
	}
	req.Header.Set("Content-Type", "application/json")
	if !e.cloudAuth {
		req.Header.Set("Authorization", "Bearer "+e.credential)
	}
	resp, err := e.httpClient.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, errors.New("execute embedding request failed")
	}
	defer drainAndClose(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("provider returned HTTP %d", resp.StatusCode)
	}
	var out embeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode embedding response: %w", err)
	}
	if len(out.Data) != 1 {
		return nil, fmt.Errorf("embedding response returned %d vectors, want 1", len(out.Data))
	}
	vector := append([]float64(nil), out.Data[0].Embedding...)
	if len(vector) != e.dimensions {
		return nil, fmt.Errorf("embedding dimensions %d do not match configured dimensions %d", len(vector), e.dimensions)
	}
	return vector, nil
}

type embeddingRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

type embeddingResponse struct {
	Data []embeddingData `json:"data"`
}

type embeddingData struct {
	Embedding []float64 `json:"embedding"`
}

func normalizeProfile(profile semanticprofile.ProviderProfile) semanticprofile.ProviderProfile {
	profile.ProfileID = strings.TrimSpace(profile.ProfileID)
	profile.ModelID = strings.TrimSpace(profile.ModelID)
	profile.EndpointProfileID = strings.TrimSpace(profile.EndpointProfileID)
	profile.CredentialSource.Kind = strings.TrimSpace(profile.CredentialSource.Kind)
	profile.CredentialSource.Handle = strings.TrimSpace(profile.CredentialSource.Handle)
	for i := range profile.SourceClasses {
		profile.SourceClasses[i] = strings.TrimSpace(profile.SourceClasses[i])
	}
	return profile
}

func endpointURL(endpointProfileID string) (string, error) {
	parsed, err := url.Parse(endpointProfileID)
	if err != nil {
		return "", err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("unsupported endpoint scheme")
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("endpoint host is required")
	}
	return strings.TrimRight(parsed.String(), "/") + embeddingsPath, nil
}

func resolveCredential(
	src semanticprofile.CredentialSource,
	getenv func(string) string,
) (credential string, cloudAuth bool, err error) {
	switch src.Kind {
	case semanticprofile.CredentialSourceEnvironmentVariable:
		if src.Handle == "" {
			return "", false, errors.New("environment variable credential source handle is required")
		}
		if getenv == nil {
			return "", false, errors.New("environment variable credential is not set")
		}
		value := getenv(src.Handle)
		if value == "" {
			return "", false, errors.New("environment variable credential is not set")
		}
		return value, false, nil
	case semanticprofile.CredentialSourceCloudWorkloadIdentity:
		return "", true, nil
	default:
		return "", false, fmt.Errorf("credential source %q is not supported: %w", src.Kind, errUnsupportedCredentialSource)
	}
}

func drainAndClose(r io.ReadCloser) {
	if r == nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(r, 4*1024))
	_ = r.Close()
}
