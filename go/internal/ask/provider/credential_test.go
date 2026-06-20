package provider

import (
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/semanticprofile"
)

func TestResolveCredential(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		src     semanticprofile.CredentialSource
		env     map[string]string
		want    string
		wantErr bool
		errIs   error
	}{
		{
			name: "env var present resolves to its value",
			src: semanticprofile.CredentialSource{
				Kind:   semanticprofile.CredentialSourceEnvironmentVariable,
				Handle: "MY_TOKEN",
			},
			env:     map[string]string{"MY_TOKEN": "secret-token-value"},
			want:    "secret-token-value",
			wantErr: false,
		},
		{
			name: "env var unset returns error naming the var",
			src: semanticprofile.CredentialSource{
				Kind:   semanticprofile.CredentialSourceEnvironmentVariable,
				Handle: "MISSING_TOKEN",
			},
			env:     map[string]string{},
			want:    "",
			wantErr: true,
		},
		{
			name: "env var empty handle returns error",
			src: semanticprofile.CredentialSource{
				Kind:   semanticprofile.CredentialSourceEnvironmentVariable,
				Handle: "",
			},
			env:     map[string]string{},
			want:    "",
			wantErr: true,
		},
		{
			name: "cloud workload identity returns empty string and nil error",
			src: semanticprofile.CredentialSource{
				Kind:   semanticprofile.CredentialSourceCloudWorkloadIdentity,
				Handle: "",
			},
			env:     map[string]string{},
			want:    "",
			wantErr: false,
		},
		{
			name: "kubernetes secret returns unsupported error",
			src: semanticprofile.CredentialSource{
				Kind:   semanticprofile.CredentialSourceKubernetesSecret,
				Handle: "secret-name",
			},
			env:     map[string]string{},
			want:    "",
			wantErr: true,
			errIs:   errUnsupportedCredentialSource,
		},
		{
			name: "vault secret handle returns unsupported error",
			src: semanticprofile.CredentialSource{
				Kind:   semanticprofile.CredentialSourceVaultSecretHandle,
				Handle: "vault/path",
			},
			env:     map[string]string{},
			want:    "",
			wantErr: true,
			errIs:   errUnsupportedCredentialSource,
		},
		{
			name: "local dev profile returns unsupported error",
			src: semanticprofile.CredentialSource{
				Kind:   semanticprofile.CredentialSourceLocalDevProfile,
				Handle: "default",
			},
			env:     map[string]string{},
			want:    "",
			wantErr: true,
			errIs:   errUnsupportedCredentialSource,
		},
		{
			name: "unknown kind returns unsupported error",
			src: semanticprofile.CredentialSource{
				Kind:   "unknown_kind",
				Handle: "something",
			},
			env:     map[string]string{},
			want:    "",
			wantErr: true,
			errIs:   errUnsupportedCredentialSource,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			getenv := func(k string) string {
				return tt.env[k]
			}

			got, err := resolveCredential(tt.src, getenv)

			if (err != nil) != tt.wantErr {
				t.Errorf("resolveCredential() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.errIs != nil && !errors.Is(err, tt.errIs) {
				t.Errorf("resolveCredential() error = %v, expected errors.Is(%v)", err, tt.errIs)
			}

			if got != tt.want {
				t.Errorf("resolveCredential() = %q, want %q", got, tt.want)
			}

			// For env var errors, verify the error message does not contain secret values.
			if tt.wantErr && tt.src.Kind == semanticprofile.CredentialSourceEnvironmentVariable {
				if err != nil {
					secret := tt.env[tt.src.Handle]
					if secret != "" && strings.Contains(err.Error(), secret) {
						t.Errorf("resolveCredential() error message contains secret value: %q", err.Error())
					}
				}
			}
		})
	}
}
