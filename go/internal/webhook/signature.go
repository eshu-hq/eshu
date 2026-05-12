package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
)

const githubSHA256Prefix = "sha256="

// VerifyGitHubSignature validates GitHub's X-Hub-Signature-256 header.
//
// Only SHA-256 signatures are accepted. The legacy SHA-1 header is rejected so
// callers cannot accidentally downgrade webhook authentication.
func VerifyGitHubSignature(payload []byte, secret string, signature string) error {
	secret = strings.TrimSpace(secret)
	signature = strings.TrimSpace(signature)
	if secret == "" {
		return errors.New("github webhook secret is required")
	}
	if !strings.HasPrefix(signature, githubSHA256Prefix) {
		return errors.New("github sha256 signature is required")
	}

	got, err := hex.DecodeString(strings.TrimPrefix(signature, githubSHA256Prefix))
	if err != nil {
		return err
	}

	mac := hmac.New(sha256.New, []byte(secret))
	if _, err := mac.Write(payload); err != nil {
		return err
	}
	if !hmac.Equal(got, mac.Sum(nil)) {
		return errors.New("github webhook signature mismatch")
	}
	return nil
}

// VerifyGitLabToken validates GitLab's X-Gitlab-Token header against the
// configured webhook secret.
func VerifyGitLabToken(secret string, token string) error {
	secret = strings.TrimSpace(secret)
	token = strings.TrimSpace(token)
	if secret == "" {
		return errors.New("gitlab webhook secret is required")
	}
	if token == "" {
		return errors.New("gitlab webhook token is required")
	}
	if !hmac.Equal([]byte(secret), []byte(token)) {
		return errors.New("gitlab webhook token mismatch")
	}
	return nil
}
