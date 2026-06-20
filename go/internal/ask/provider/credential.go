package provider

import (
	"errors"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/semanticprofile"
)

var errUnsupportedCredentialSource = errors.New("unsupported credential source")

// resolveCredential resolves a credential from a semantic profile credential source.
// For CredentialSourceEnvironmentVariable, it returns the value of the environment variable
// named by src.Handle, or an error if the handle is empty or the environment variable is unset.
// For CredentialSourceCloudWorkloadIdentity, it returns ("", nil) because auth is supplied out of band.
// For all other credential source kinds, it returns ("", error) with a clear message about the kind.
func resolveCredential(src semanticprofile.CredentialSource, getenv func(string) string) (string, error) {
	switch src.Kind {
	case semanticprofile.CredentialSourceEnvironmentVariable:
		if src.Handle == "" {
			return "", fmt.Errorf("environment variable credential source: handle is empty")
		}
		val := getenv(src.Handle)
		if val == "" {
			return "", fmt.Errorf("environment variable %q is not set", src.Handle)
		}
		return val, nil

	case semanticprofile.CredentialSourceCloudWorkloadIdentity:
		return "", nil

	default:
		return "", fmt.Errorf("credential source %q is not supported by the ask provider yet: %w", src.Kind, errUnsupportedCredentialSource)
	}
}
