// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package claim

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	workflowv1 "github.com/eshu-hq/eshu-workflow/workflowv1"

	"github.com/eshu-hq/eshu/go/internal/component"
)

// A nil Config.Clock must read the wall clock on every call. The default was
// once a method value bound at construction, which froze time and left an
// expired grant authorized.
func TestNilClockTracksWallTimeSoExpiredGrantsDeny(t *testing.T) {
	t.Parallel()
	grant := liveGrant()
	grant.ExpiresAt = time.Now().UTC().Add(200 * time.Millisecond)
	live := &mutableGrants{}
	live.set([]component.ProducerGrant{grant}, nil)

	cfg := testConfig("http://localhost:1")
	cfg.ReadGrants = live.read
	cfg.Clock = nil
	client, err := NewClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.AuthorizeEmission(context.Background(), replayCoreKind, replayCoreSchema); err != nil {
		t.Fatalf("AuthorizeEmission() before expiry = %v, want allow", err)
	}
	time.Sleep(400 * time.Millisecond)
	err = client.AuthorizeEmission(context.Background(), replayCoreKind, replayCoreSchema)
	var deny *DenyError
	if !errors.As(err, &deny) {
		t.Fatalf("AuthorizeEmission() after expiry = %v, want *DenyError (clock must not freeze)", err)
	}
}

// The bearer must be scrubbed before the message is bounded: truncating
// first would cut a token that straddles the bound, and the scrub (a whole
// string match) would then miss the surviving prefix.
func TestEchoedTokenStraddlingTheBoundIsScrubbedBeforeTruncation(t *testing.T) {
	t.Parallel()
	const canary = "test-only-canary-bearer-9f2k7q"
	fake := newFakeWorkflow(t)
	fake.on(workflowv1.OpClaimNext, func(*http.Request) (int, string) {
		return http.StatusInternalServerError,
			`{"code":"internal","message":"` + strings.Repeat("a", maxErrorMessage-8) + canary + `"}`
	})
	cfg := testConfig(fake.server.URL)
	cfg.AuthToken = canary
	client, err := NewClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = client.ClaimNext(context.Background(), validSelector(), time.Now().UTC(), time.Minute)
	if err == nil {
		t.Fatal("ClaimNext() = nil, want the server error")
	}
	if strings.Contains(err.Error(), canary[:6]) {
		t.Fatalf("error carries a token prefix: %q", err.Error())
	}
}

func TestBoundMessageTruncatesOnARuneBoundary(t *testing.T) {
	t.Parallel()
	got := boundMessage(strings.Repeat("€", maxErrorMessage))
	if !utf8.ValidString(got) {
		t.Fatalf("boundMessage produced invalid UTF-8: %q", got)
	}
	if len(got) > maxErrorMessage+len("…") {
		t.Fatalf("boundMessage = %d bytes, want at most %d plus the ellipsis", len(got), maxErrorMessage)
	}
}

// Lifecycle operations succeed only on the exact acknowledgement the
// pinned server emits. Anything else fails closed.
func TestLifecycleAckIsDecodedStrictly(t *testing.T) {
	t.Parallel()
	bad := map[string]string{
		"empty body":       ``,
		"ok false":         `{"ok":false}` + "\n",
		"missing ok":       `{}` + "\n",
		"unknown field":    `{"ok":true,"extra":1}` + "\n",
		"trailing data":    `{"ok":true}` + "\n" + `{"ok":true}`,
		"trailing garbage": `{"ok":true}garbage`,
		"not json":         `OK`,
	}
	for name, payload := range bad {
		payload := payload
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fake := newFakeWorkflow(t)
			fake.on(workflowv1.OpCompleteClaim, func(*http.Request) (int, string) { return http.StatusOK, payload })
			client, err := NewClient(testConfig(fake.server.URL))
			if err != nil {
				t.Fatal(err)
			}
			if err := client.Complete(context.Background(), validMutation(time.Now().UTC())); err == nil {
				t.Fatalf("Complete() with body %q = nil, want fail-closed", payload)
			}
		})
	}
}

func TestClaimNextRejectsAGrantForAnotherSelector(t *testing.T) {
	t.Parallel()
	for name, mutate := range map[string]func(*workflowv1.ClaimedWorkItem){
		"claim id": func(g *workflowv1.ClaimedWorkItem) {
			g.Claim.ClaimID = "claim-other"
			g.WorkItem.CurrentClaimID = "claim-other"
		},
		"owner id": func(g *workflowv1.ClaimedWorkItem) {
			g.Claim.OwnerID = "owner-other"
			g.WorkItem.CurrentOwnerID = "owner-other"
		},
		"collector kind":     func(g *workflowv1.ClaimedWorkItem) { g.WorkItem.CollectorKind = "other-source" },
		"collector instance": func(g *workflowv1.ClaimedWorkItem) { g.WorkItem.CollectorInstanceID = "other-instance" },
	} {
		mutate := mutate
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fake := newFakeWorkflow(t)
			now := time.Now().UTC().Truncate(time.Second)
			grant := validGrant(now)
			mutate(&grant)
			fake.okJSON(workflowv1.OpClaimNext, mustGrantJSON(t, grant))
			client, err := NewClient(testConfig(fake.server.URL))
			if err != nil {
				t.Fatal(err)
			}
			_, found, err := client.ClaimNext(context.Background(), validSelector(), now, time.Minute)
			if err == nil || found {
				t.Fatalf("ClaimNext() found=%v err=%v, want a fail-closed selector mismatch", found, err)
			}
		})
	}
}

// A 429 without retry_after_seconds yields a zero RetryAfter: the table
// declares the field optional, so the caller must apply its own backoff
// floor rather than spin.
func TestPacedWithoutRetryAfterReportsZeroDelay(t *testing.T) {
	t.Parallel()
	fake := newFakeWorkflow(t)
	fake.fail(workflowv1.OpClaimNext, http.StatusTooManyRequests, workflowv1.CodePaced, "slow down")
	client, err := NewClient(testConfig(fake.server.URL))
	if err != nil {
		t.Fatal(err)
	}
	_, found, err := client.ClaimNext(context.Background(), validSelector(), time.Now().UTC(), time.Minute)
	var paced *PacedError
	if found || !errors.As(err, &paced) {
		t.Fatalf("ClaimNext() found=%v err=%v, want *PacedError", found, err)
	}
	if paced.RetryAfterDelay() != 0 {
		t.Fatalf("RetryAfterDelay() = %s, want 0 when the server sent none", paced.RetryAfterDelay())
	}
}

// hostileText builds server text longer than the bound whose token straddles
// the cut once the fixed prefix the client prepends (prefixLen bytes) is
// counted, followed by multi-byte runes so the cut also lands inside a rune.
// A bound-then-scrub order keeps a token prefix only when the token really
// straddles the cut, so every caller passes the exact prefix length.
func hostileText(prefixLen int, token string) string {
	return strings.Repeat("a", maxErrorMessage-prefixLen-len(token)/2) + token + strings.Repeat("€", maxErrorMessage)
}

func assertSurfacedText(t *testing.T, what, got, token string) {
	t.Helper()
	if len(got) > maxErrorMessage+len("…") {
		t.Errorf("%s = %d bytes, want at most %d plus the ellipsis", what, len(got), maxErrorMessage)
	}
	if !utf8.ValidString(got) {
		t.Errorf("%s is not valid UTF-8", what)
	}
	for _, probe := range []string{token, token[:6], token[len(token)-6:]} {
		if strings.Contains(got, probe) {
			t.Errorf("%s carries token text %q", what, probe)
		}
	}
}

// The commit-pin refusal path renders a Validate error. It must follow the
// same scrub-then-bound rule as every other surfaced text.
func TestPinRefusalMessageIsScrubbedAndBounded(t *testing.T) {
	t.Parallel()
	const canary = "test-only-canary-bearer-9f2k7q"
	cfg := testConfig("http://localhost:1")
	cfg.AuthToken = canary
	client, err := NewClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	refused := client.pinRefusedError(errors.New(hostileText(len("commit pin refused: "), canary)))
	assertSurfacedText(t, "pin refusal message", refused.Message, canary)
	if !refused.Stale() {
		t.Fatalf("pin refusal code = %q, want the stale-fence signal", refused.Code)
	}
}

// The paced error text embeds a server-supplied domain. The domain and the
// rendered text must both be scrubbed and then bounded. Two cases place the
// token across the cut of each: the stored domain (no prefix) and the
// rendered text (the deferral prefix before the domain).
func TestPacedErrorTextIsScrubbedAndBounded(t *testing.T) {
	t.Parallel()
	const canary = "test-only-canary-bearer-9f2k7q"
	const marker = "DOMAIN-MARKER"
	rendered := workflowv1.ClaimPacingDeferral{
		Signal: workflowv1.ClaimPacingSignalDomain, Domain: marker, RetryAfter: time.Second,
	}.Error()
	textPrefix := strings.Index(rendered, marker)
	if textPrefix <= 0 {
		t.Fatalf("deferral text %q does not embed the domain", rendered)
	}
	for name, prefixLen := range map[string]int{"stored domain": 0, "rendered text": textPrefix} {
		prefixLen := prefixLen
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fake := newFakeWorkflow(t)
			raw, err := json.Marshal(workflowv1.ClaimNextResponse{
				Deferral: workflowv1.ClaimPacingDeferral{
					Signal:     workflowv1.ClaimPacingSignalDomain,
					Domain:     hostileText(prefixLen, canary),
					RetryAfter: time.Second,
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			fake.okJSON(workflowv1.OpClaimNext, string(raw))
			cfg := testConfig(fake.server.URL)
			cfg.AuthToken = canary
			client, err := NewClient(cfg)
			if err != nil {
				t.Fatal(err)
			}
			_, _, err = client.ClaimNext(context.Background(), validSelector(), time.Now().UTC(), time.Minute)
			var paced *PacedError
			if !errors.As(err, &paced) {
				t.Fatalf("ClaimNext() = %v, want *PacedError", err)
			}
			assertSurfacedText(t, "paced error text", paced.Error(), canary)
			assertSurfacedText(t, "stored deferral domain", paced.Deferral.Domain, canary)
			if paced.RetryAfterDelay() != time.Second {
				t.Fatalf("RetryAfterDelay() = %s, want 1s", paced.RetryAfterDelay())
			}
		})
	}
}
