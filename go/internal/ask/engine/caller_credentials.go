// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package engine

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query"
)

type callerCredentialsKey struct{}

// callerCredentials is an immutable copy of the accepted request's credential
// headers. Its string forms never expose secrets through diagnostics.
type callerCredentials struct {
	authorization string
	cookie        string
	csrf          string
	hasSession    bool
}

func (callerCredentials) String() string   { return "caller credentials [redacted]" }
func (callerCredentials) GoString() string { return "caller credentials [redacted]" }

// ContextWithCallerAuthHeader marks even an empty caller Authorization header
// as an explicit request credential, preventing a shared-key fallback.
func ContextWithCallerAuthHeader(ctx context.Context, header string) context.Context {
	return context.WithValue(ctx, callerCredentialsKey{}, callerCredentials{authorization: header})
}

// ContextWithCallerRequestCredentials captures only the credentials needed to
// reauthenticate nested Ask reads. It retains no request, header, or cookie
// pointers. The request marker exists even when every credential is absent.
func ContextWithCallerRequestCredentials(ctx context.Context, request *http.Request) context.Context {
	credentials := callerCredentials{}
	if request != nil {
		credentials.authorization = request.Header.Get("Authorization")
		credentials.csrf = request.Header.Get("X-Eshu-CSRF")
		var cookies []string
		for _, cookie := range request.Cookies() {
			switch cookie.Name {
			case "__Host-eshu_session", "eshu_session":
				credentials.hasSession = true
			case "__Host-eshu_csrf", "eshu_csrf":
			default:
				continue
			}
			cookies = append(cookies, cookie.String())
		}
		credentials.cookie = strings.Join(cookies, "; ")
	}
	return context.WithValue(ctx, callerCredentialsKey{}, credentials)
}

func callerCredentialsFromContext(ctx context.Context) (callerCredentials, bool) {
	credentials, ok := ctx.Value(callerCredentialsKey{}).(callerCredentials)
	return credentials, ok
}

func validateCallerCredentials(ctx context.Context, credentials callerCredentials, marked bool) error {
	auth, authenticated := query.AuthContextFromContext(ctx)
	if !marked {
		if authenticated {
			return errors.New("authenticated Ask request has no caller credentials")
		}
		return nil // Explicit legacy non-HTTP runner call.
	}
	if !authenticated {
		return nil // Authentication-disabled local request; dispatch without credentials.
	}
	switch auth.Mode {
	case query.AuthModeBrowserSession:
		if strings.TrimSpace(credentials.authorization) == "" && credentials.hasSession {
			return nil
		}
	case query.AuthModeShared, query.AuthModeScoped:
		if strings.TrimSpace(credentials.authorization) != "" {
			return nil
		}
	}
	return errors.New("authenticated Ask request has no matching caller credentials")
}

type callerCredentialHandler struct {
	next        http.Handler
	credentials callerCredentials
}

func (handler callerCredentialHandler) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	inner := request.Clone(request.Context())
	inner.Header.Del("Authorization")
	inner.Header.Del("Cookie")
	inner.Header.Del("X-Eshu-CSRF")
	if handler.credentials.authorization != "" {
		inner.Header.Set("Authorization", handler.credentials.authorization)
	}
	if handler.credentials.cookie != "" {
		inner.Header.Set("Cookie", handler.credentials.cookie)
	}
	if handler.credentials.csrf != "" {
		inner.Header.Set("X-Eshu-CSRF", handler.credentials.csrf)
	}
	handler.next.ServeHTTP(w, inner)
}
