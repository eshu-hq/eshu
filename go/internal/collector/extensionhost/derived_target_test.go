// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/scannercomponent"
	"github.com/eshu-hq/eshu/go/internal/github/app"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const (
	testGoScope  = "go-reachability://repository:r_1a2b3c4d"
	testGoCommit = "0123456789abcdef0123456789abcdef01234567"
)

func goScopeSet(cloneURL string) string {
	return `{"targets":[{"scope_id":"` + testGoScope + `","target_kind":"repository",` +
		`"repository_id":"repository:r_1a2b3c4d","source_commit_sha":"` + testGoCommit + `",` +
		`"clone_url":"` + cloneURL + `","acceptance_unit_id":"` + testGoScope + `"}]}`
}

type fakeRunScopeSetReader struct {
	raw   string
	err   error
	reads int
}

func (f *fakeRunScopeSetReader) GetRunRequestedScopeSet(context.Context, string) (string, error) {
	f.reads++
	return f.raw, f.err
}

func goClaim(scopeID string) workflow.WorkItem {
	item := testWorkItem()
	item.ScopeID = scopeID
	return item
}

// TestGoReachabilityTargetResolverDelivery covers the resolver's three outcomes:
// the validated target, a fail-closed transport error, and a retryable store fault.
func TestGoReachabilityTargetResolverDelivery(t *testing.T) {
	t.Parallel()

	reader := &fakeRunScopeSetReader{raw: goScopeSet("https://github.com/eshu-hq/widgets")}
	got, err := GoReachabilityTargetResolver(reader)(context.Background(), goClaim(testGoScope))
	if err != nil {
		t.Fatalf("resolver error = %v", err)
	}
	if got["source_commit_sha"] != testGoCommit || got["clone_url"] != "https://github.com/eshu-hq/widgets" ||
		got["scope_id"] != testGoScope || got["repository_id"] != "repository:r_1a2b3c4d" {
		t.Fatalf("resolved target = %v", got)
	}

	for name, reader := range map[string]*fakeRunScopeSetReader{
		"missing run":  {err: workflow.ErrRunNotFound},
		"empty row":    {raw: ""},
		"no entry":     {raw: `{"targets":[]}`},
		"unsafe clone": {raw: goScopeSet("https://user:pw@github.com/eshu-hq/widgets")},
	} {
		_, err := GoReachabilityTargetResolver(reader)(context.Background(), goClaim(testGoScope))
		if !errors.Is(err, collector.ErrSlotScopeTransport) {
			t.Errorf("%s: error = %v, want the terminal transport sentinel", name, err)
		}
		if err != nil && strings.Contains(err.Error(), "pw@") {
			t.Errorf("%s: error %q echoes the clone url", name, err)
		}
	}

	fault := errors.New("connection reset")
	_, err = GoReachabilityTargetResolver(&fakeRunScopeSetReader{err: fault})(context.Background(), goClaim(testGoScope))
	if !errors.Is(err, fault) || errors.Is(err, collector.ErrSlotScopeTransport) {
		t.Fatalf("store fault error = %v, want it retryable", err)
	}
}

// TestGoReachabilityTargetResolverIgnoresOtherScopes proves other claims stay
// byte-identical and cost no run-row read.
func TestGoReachabilityTargetResolverIgnoresOtherScopes(t *testing.T) {
	t.Parallel()

	reader := &fakeRunScopeSetReader{}
	got, err := GoReachabilityTargetResolver(reader)(context.Background(), goClaim("vuln-intel://cisa/kev"))
	if err != nil || got != nil || reader.reads != 0 {
		t.Fatalf("resolver = %v, %v after %d reads, want nil, nil, 0", got, err, reader.reads)
	}
	if _, err := GoReachabilityTargetResolver(nil)(context.Background(), goClaim(testGoScope)); !errors.Is(err, collector.ErrSlotScopeTransport) {
		t.Fatalf("nil store error = %v, want the transport sentinel", err)
	}
}

func goDerivedSource(t *testing.T, resolver DerivedTargetResolver, config map[string]any) (*Source, error) {
	t.Helper()
	return NewSource(Config{
		Manifest:              testManifest(),
		CollectorInstanceID:   "scorecard-instance",
		ScopeKind:             scope.KindRepository,
		ConfigHandle:          "cfg-scorecard",
		Config:                config,
		Runner:                &recordingRunner{},
		Clock:                 testObservedAt,
		DerivedTargetResolver: resolver,
	})
}

// TestSourceDeliversDerivedTargetOnTheClaimConfig proves the component receives
// the commit and clone URL under the reserved key, with the claim identity unchanged.
func TestSourceDeliversDerivedTargetOnTheClaimConfig(t *testing.T) {
	t.Parallel()

	source, err := goDerivedSource(t, GoReachabilityTargetResolver(
		&fakeRunScopeSetReader{raw: goScopeSet("https://github.com/eshu-hq/widgets")}), map[string]any{"fixture": "x"})
	if err != nil {
		t.Fatalf("NewSource() error = %v", err)
	}
	request, err := source.requestForWorkItem(context.Background(), goClaim(testGoScope))
	if err != nil {
		t.Fatalf("requestForWorkItem() error = %v", err)
	}
	target, ok := request.Config[HostDerivedTargetKey].(map[string]any)
	if !ok || target["source_commit_sha"] != testGoCommit {
		t.Fatalf("config = %v, want the derived target", request.Config)
	}
	if request.Claim.Scope.ID != testGoScope || len(request.Claim.ScopeQueries) != 0 {
		t.Fatalf("claim = %+v, want the scope id and no scope queries", request.Claim)
	}
	if strings.Contains(request.Claim.Scope.ID, "github.com") {
		t.Fatalf("scope id %q carries the clone url", request.Claim.Scope.ID)
	}
	if request.Config["fixture"] != "x" {
		t.Fatalf("config = %v, want the static config kept", request.Config)
	}

	// The per-claim value must not leak into the source's shared config.
	other, err := source.requestForWorkItem(context.Background(), goClaim("vuln-intel://cisa/kev"))
	if err != nil {
		t.Fatalf("requestForWorkItem(other) error = %v", err)
	}
	if _, present := other.Config[HostDerivedTargetKey]; present {
		t.Fatalf("config = %v, want no derived target on another scope", other.Config)
	}
}

// TestNewSourceRefusesStaticDerivedTargetKey proves static config cannot forge a
// commit or clone URL. The error names the key, not the value.
func TestNewSourceRefusesStaticDerivedTargetKey(t *testing.T) {
	t.Parallel()

	_, err := goDerivedSource(t, GoReachabilityTargetResolver(&fakeRunScopeSetReader{}),
		map[string]any{HostDerivedTargetKey: "forged-value"})
	if err == nil || !strings.Contains(err.Error(), HostDerivedTargetKey) || strings.Contains(err.Error(), "forged-value") {
		t.Fatalf("NewSource() error = %v, want a refusal naming only the key", err)
	}
}

// TestNextClaimedClassifiesDerivedTargetFailures proves a transport failure is
// terminal and never launches the component, while a store fault stays retryable.
func TestNextClaimedClassifiesDerivedTargetFailures(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name         string
		reader       *fakeRunScopeSetReader
		wantTerminal bool
	}{
		{name: "missing entry", reader: &fakeRunScopeSetReader{raw: `{"targets":[]}`}, wantTerminal: true},
		{name: "store fault", reader: &fakeRunScopeSetReader{err: errors.New("connection reset")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runner := &recordingRunner{}
			source, err := NewSource(Config{
				Manifest: testManifest(), CollectorInstanceID: "scorecard-instance", ScopeKind: scope.KindRepository,
				ConfigHandle: "cfg-scorecard", Runner: runner, Clock: testObservedAt,
				DerivedTargetResolver: GoReachabilityTargetResolver(tc.reader),
			})
			if err != nil {
				t.Fatalf("NewSource() error = %v", err)
			}
			_, _, err = source.NextClaimed(context.Background(), goClaim(testGoScope))
			if err == nil {
				t.Fatal("NextClaimed() error = nil, want a failure")
			}
			if len(runner.requests) != 0 {
				t.Fatalf("component launched %d times, want 0", len(runner.requests))
			}
			var failure extensionFailure
			if !errors.As(err, &failure) || failure.terminal != tc.wantTerminal {
				t.Fatalf("failure = %+v, want terminal %v", failure, tc.wantTerminal)
			}
		})
	}
}

func goInstallationProvider(
	t *testing.T,
	installations map[string]string,
	reader *fakeRunScopeSetReader,
) (TokenProvider, *[]string) {
	t.Helper()
	var mu sync.Mutex
	seen := &[]string{}
	server := appProviderTestServer(t, &mu, seen)
	provider, err := NewAppTokenProvider(AppTokenProviderConfig{
		AppID: "42", PrivateKey: appProviderTestKey(t), Installations: installations,
		BaseURL: server.URL, HTTPClient: server.Client(),
		InstallationKey: GoReachabilityInstallationKey(reader),
	})
	if err != nil {
		t.Fatalf("NewAppTokenProvider() error = %v", err)
	}
	return provider, seen
}

// TestAppTokenProviderSelectsInstallationByOwnerForDerivedScope is the #10027
// rule: a derived scope selects its installation from the host and owner of its
// planned remote, because a static table cannot name scopes that are derived.
func TestAppTokenProviderSelectsInstallationByOwnerForDerivedScope(t *testing.T) {
	t.Parallel()

	reader := &fakeRunScopeSetReader{raw: goScopeSet("https://github.com/eshu-hq/widgets")}
	provider, _ := goInstallationProvider(t, map[string]string{"github-owner:github.com/Eshu-HQ": "777"}, reader)
	token, err := provider(context.Background(), goClaim(testGoScope))
	if err != nil {
		t.Fatalf("provider() error = %v", err)
	}
	if token != "tok-for-777" {
		t.Fatalf("token = %q, want the owner's installation", token)
	}
}

// TestAppTokenProviderExactScopeWins proves an exact scope entry beats the owner
// rule and needs no run-row read, so existing maps behave as before.
func TestAppTokenProviderExactScopeWins(t *testing.T) {
	t.Parallel()

	reader := &fakeRunScopeSetReader{raw: goScopeSet("https://github.com/eshu-hq/widgets")}
	provider, _ := goInstallationProvider(t, map[string]string{
		testGoScope: "111", "github-owner:github.com/eshu-hq": "777",
	}, reader)
	token, err := provider(context.Background(), goClaim(testGoScope))
	if err != nil || token != "tok-for-111" {
		t.Fatalf("provider() = %q, %v, want the exact scope's installation", token, err)
	}
	if reader.reads != 0 {
		t.Fatalf("run reads = %d, want 0 for an exact match", reader.reads)
	}
}

// TestAppTokenProviderOwnerRuleFailsClosed proves no mint happens for an unmapped
// owner or a look-alike host: the token would be sent to that same remote.
func TestAppTokenProviderOwnerRuleFailsClosed(t *testing.T) {
	t.Parallel()

	for name, cloneURL := range map[string]string{
		"unmapped owner":  "https://github.com/someone-else/widgets",
		"look-alike host": "https://github.com.evil.example/eshu-hq/widgets",
		"other host":      "https://gitlab.com/eshu-hq/widgets",
	} {
		reader := &fakeRunScopeSetReader{raw: goScopeSet(cloneURL)}
		provider, seen := goInstallationProvider(t, map[string]string{"github-owner:github.com/eshu-hq": "777"}, reader)
		_, err := provider(context.Background(), goClaim(testGoScope))
		var configErr *app.ConfigError
		if !errors.As(err, &configErr) {
			t.Errorf("%s: error = %v, want a bounded config denial", name, err)
		}
		if len(*seen) != 0 {
			t.Errorf("%s: mint attempted %d times, want 0", name, len(*seen))
		}
	}
}

// TestAppTokenProviderOwnerNeverMatchesAnotherScope proves the owner rule applies
// only to derived scopes: an owner key cannot serve an unrelated claim.
func TestAppTokenProviderOwnerNeverMatchesAnotherScope(t *testing.T) {
	t.Parallel()

	reader := &fakeRunScopeSetReader{raw: goScopeSet("https://github.com/eshu-hq/widgets")}
	provider, seen := goInstallationProvider(t, map[string]string{"github-owner:github.com/eshu-hq": "777"}, reader)
	if _, err := provider(context.Background(), goClaim("github.com/eshu-hq/widgets")); err == nil {
		t.Fatal("provider() error = nil, want denial")
	}
	if len(*seen) != 0 || reader.reads != 0 {
		t.Fatalf("mints = %d reads = %d, want 0 and 0", len(*seen), reader.reads)
	}
}

// TestAppTokenProviderOwnerRuleClassifiesReadFailures proves a transient store
// fault stays retryable through the host's classifier instead of burning the
// claim, while a broken run row is a terminal denial.
func TestAppTokenProviderOwnerRuleClassifiesReadFailures(t *testing.T) {
	t.Parallel()

	secret := errors.New("connect to postgres://user:secret@db failed")
	provider, _ := goInstallationProvider(t, map[string]string{"github-owner:github.com/eshu-hq": "777"},
		&fakeRunScopeSetReader{err: secret})
	_, err := provider(context.Background(), goClaim(testGoScope))
	failure := classifyTokenProviderError(err)
	if failure.terminal {
		t.Fatalf("store fault classified terminal: %+v", failure)
	}
	if strings.Contains(err.Error(), "secret") || strings.Contains(failure.Error(), "secret") {
		t.Fatalf("error %q carries store text", err)
	}

	broken, _ := goInstallationProvider(t, map[string]string{"github-owner:github.com/eshu-hq": "777"},
		&fakeRunScopeSetReader{raw: `{"targets":[]}`})
	_, err = broken(context.Background(), goClaim(testGoScope))
	if failure := classifyTokenProviderError(err); !failure.terminal {
		t.Fatalf("broken run row classified retryable: %+v", failure)
	}
}

// TestGoReachabilityKeyPrefixMatchesTheScannerContract pins the one spelling
// operators write in ESHU_GITHUB_APP_INSTALLATIONS.
func TestGoReachabilityKeyPrefixMatchesTheScannerContract(t *testing.T) {
	t.Parallel()

	if scannercomponent.InstallationOwnerKeyPrefix != "github-owner:" {
		t.Fatalf("prefix = %q", scannercomponent.InstallationOwnerKeyPrefix)
	}
}
