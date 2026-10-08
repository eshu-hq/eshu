// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func discoverSelection(
	ctx context.Context,
	config RepoSyncConfig,
	token string,
) (RepositorySelection, error) {
	switch strings.TrimSpace(config.SourceMode) {
	case "filesystem":
		if strings.TrimSpace(config.FilesystemRoot) == "" {
			return RepositorySelection{}, fmt.Errorf("filesystem source mode requires ESHU_FILESYSTEM_ROOT")
		}
		if len(config.Repositories) > 0 {
			return RepositorySelection{
				RepositoryIDs: sortUniqueStrings(config.Repositories),
			}, nil
		}
		repositoryIDs, err := discoverFilesystemRepositoryIDs(config.FilesystemRoot)
		if err != nil {
			return RepositorySelection{}, err
		}
		return RepositorySelection{RepositoryIDs: repositoryIDs}, nil
	case "explicit":
		return RepositorySelection{RepositoryIDs: sortUniqueStrings(config.Repositories)}, nil
	case "githubOrg":
		if strings.TrimSpace(config.GithubOrg) == "" {
			return RepositorySelection{}, fmt.Errorf("githubOrg source mode requires ESHU_GITHUB_ORG")
		}
		if strings.TrimSpace(token) == "" {
			return RepositorySelection{}, fmt.Errorf("githubOrg source mode requires GitHub token or App auth")
		}
		repositories, truncated, err := listGitHubOrgRepositories(ctx, config.GithubOrg, config.RepoLimit, token)
		if err != nil {
			return RepositorySelection{}, err
		}
		selection := selectGitHubRepositoryIDs(repositories, config.RepositoryRules, config.IncludeArchivedRepos)
		selection.ListingTruncated = truncated
		return selection, nil
	default:
		return RepositorySelection{}, fmt.Errorf("unsupported ESHU_REPO_SOURCE_MODE=%q", config.SourceMode)
	}
}

func resolveGitToken(ctx context.Context, config RepoSyncConfig) (string, error) {
	switch strings.ToLower(strings.TrimSpace(config.GitAuthMethod)) {
	case "", "none", "ssh":
		return strings.TrimSpace(config.GitToken), nil
	case "token":
		token := strings.TrimSpace(config.GitToken)
		if token == "" {
			return "", fmt.Errorf("ESHU_GIT_TOKEN or GITHUB_TOKEN is required when ESHU_GIT_AUTH_METHOD=token")
		}
		return token, nil
	case "githubapp":
		return mintGitHubAppToken(ctx, config)
	default:
		return "", fmt.Errorf("unsupported ESHU_GIT_AUTH_METHOD=%q", config.GitAuthMethod)
	}
}

// githubAPIReposBaseURL is the GitHub REST API base for org repository
// listings. Tests override it with an httptest server; production keeps the
// public API host.
var githubAPIReposBaseURL = "https://api.github.com"

// githubOrgReposPerPage is the fixed page width for org repository
// listings. It must stay constant across every page of one listing: GitHub
// paginates by offset ((page-1)*per_page), so shrinking per_page on the last
// request re-reads earlier repositories and the listing ends incomplete
// while looking complete (#7625 amendment 1).
const githubOrgReposPerPage = 100

// listGitHubOrgRepositories returns the org's repositories, fetching fixed
// 100-wide pages and trimming to repoLimit client-side. It reports whether
// the listing stopped at RepoLimit before exhaustion: a truncated listing is
// incomplete by construction and the #7625 selection observer must skip it
// rather than mistake present repositories for missing ones.
func listGitHubOrgRepositories(
	ctx context.Context,
	org string,
	repoLimit int,
	token string,
) ([]GitHubRepositoryRecord, bool, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	repositories := make([]GitHubRepositoryRecord, 0)
	exhausted := false
	for page := 1; len(repositories) < repoLimit; page++ {
		request, err := http.NewRequestWithContext(
			ctx,
			http.MethodGet,
			fmt.Sprintf(githubAPIReposBaseURL+"/orgs/%s/repos?per_page=%d&page=%d&type=all", org, githubOrgReposPerPage, page),
			nil,
		)
		if err != nil {
			return nil, false, fmt.Errorf("build GitHub org repos request: %w", err)
		}
		request.Header.Set("Accept", "application/vnd.github+json")
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		request.Header.Set("User-Agent", "eshu-go")

		response, err := client.Do(request)
		if err != nil {
			return nil, false, fmt.Errorf("list GitHub org repositories: %w", err)
		}
		if response.Body == nil {
			response.Close = true
		}
		var payload []struct {
			ID       int64  `json:"id"`
			FullName string `json:"full_name"`
			Archived bool   `json:"archived"`
		}
		decodeErr := json.NewDecoder(response.Body).Decode(&payload)
		_ = response.Body.Close()
		if response.StatusCode >= 300 {
			return nil, false, fmt.Errorf("list GitHub org repositories: status %d", response.StatusCode)
		}
		if decodeErr != nil {
			return nil, false, fmt.Errorf("decode GitHub org repositories: %w", decodeErr)
		}
		if len(payload) == 0 {
			exhausted = true
			break
		}
		for _, item := range payload {
			repoID := normalizeRepositoryID(item.FullName)
			if repoID == "" {
				continue
			}
			repositories = append(repositories, GitHubRepositoryRecord{
				RepoID:   repoID,
				GitHubID: item.ID,
				Archived: item.Archived,
			})
		}
	}
	if len(repositories) > repoLimit {
		repositories = repositories[:repoLimit]
	}
	return repositories, !exhausted, nil
}

func mintGitHubAppToken(ctx context.Context, config RepoSyncConfig) (string, error) {
	if strings.TrimSpace(config.GitHubAppID) == "" ||
		strings.TrimSpace(config.GitHubAppInstallation) == "" ||
		strings.TrimSpace(config.GitHubAppPrivateKey) == "" {
		return "", fmt.Errorf("GitHub App auth requires GITHUB_APP_ID, GITHUB_APP_INSTALLATION_ID, and GITHUB_APP_PRIVATE_KEY")
	}

	now := time.Now().Unix()
	claims := jwt.MapClaims{
		"iat": now - 60,
		"exp": now + 540,
		"iss": config.GitHubAppID,
	}
	parsedKey, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(normalizePrivateKeyPEM(config.GitHubAppPrivateKey)))
	if err != nil {
		return "", fmt.Errorf("parse GitHub App private key: %w", err)
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(parsedKey)
	if err != nil {
		return "", fmt.Errorf("sign GitHub App JWT: %w", err)
	}

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		fmt.Sprintf("https://api.github.com/app/installations/%s/access_tokens", config.GitHubAppInstallation),
		bytes.NewReader([]byte("{}")),
	)
	if err != nil {
		return "", fmt.Errorf("build GitHub App token request: %w", err)
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("Authorization", "Bearer "+signed)
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	request.Header.Set("User-Agent", "eshu-go")

	client := &http.Client{Timeout: 15 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("mint GitHub App token: %w", err)
	}
	defer func() {
		_ = response.Body.Close()
	}()
	if response.StatusCode >= 300 {
		return "", fmt.Errorf("mint GitHub App token: status %d", response.StatusCode)
	}
	var payload struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("decode GitHub App token response: %w", err)
	}
	if strings.TrimSpace(payload.Token) == "" {
		return "", fmt.Errorf("GitHub App token response omitted token")
	}
	return payload.Token, nil
}

func normalizePrivateKeyPEM(privateKey string) string {
	stripped := strings.TrimSpace(privateKey)
	if strings.Contains(stripped, "\n") || !strings.HasPrefix(stripped, "-----BEGIN ") {
		return stripped
	}
	stripped = strings.ReplaceAll(stripped, "-----BEGIN RSA PRIVATE KEY-----", "")
	stripped = strings.ReplaceAll(stripped, "-----END RSA PRIVATE KEY-----", "")
	stripped = strings.TrimSpace(stripped)
	body := make([]string, 0, len(stripped)/64+1)
	for len(stripped) > 64 {
		body = append(body, stripped[:64])
		stripped = stripped[64:]
	}
	if stripped != "" {
		body = append(body, stripped)
	}
	return "-----BEGIN RSA PRIVATE KEY-----\n" + strings.Join(body, "\n") + "\n-----END RSA PRIVATE KEY-----\n"
}

func githubHTTPExtraHeader(token string) string {
	encoded := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token))
	return "AUTHORIZATION: basic " + encoded
}
