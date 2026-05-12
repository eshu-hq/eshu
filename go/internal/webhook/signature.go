package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

const sha256SignaturePrefix = "sha256="

// VerifyGitHubSignature validates GitHub's X-Hub-Signature-256 header.
//
// Only SHA-256 signatures are accepted. The legacy SHA-1 header is rejected so
// callers cannot accidentally downgrade webhook authentication.
func VerifyGitHubSignature(payload []byte, secret string, signature string) error {
	return verifySHA256Signature(payload, secret, signature, "github")
}

// VerifyBitbucketSignature validates Bitbucket Cloud's X-Hub-Signature header.
func VerifyBitbucketSignature(payload []byte, secret string, signature string) error {
	return verifySHA256Signature(payload, secret, signature, "bitbucket")
}

func verifySHA256Signature(payload []byte, secret string, signature string, provider string) error {
	secret = strings.TrimSpace(secret)
	signature = strings.TrimSpace(signature)
	if secret == "" {
		return fmt.Errorf("%s webhook secret is required", provider)
	}
	if !strings.HasPrefix(signature, sha256SignaturePrefix) {
		return fmt.Errorf("%s sha256 signature is required", provider)
	}

	got, err := hex.DecodeString(strings.TrimPrefix(signature, sha256SignaturePrefix))
	if err != nil {
		return err
	}

	mac := hmac.New(sha256.New, []byte(secret))
	if _, err := mac.Write(payload); err != nil {
		return err
	}
	if !hmac.Equal(got, mac.Sum(nil)) {
		return fmt.Errorf("%s webhook signature mismatch", provider)
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
