// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/registry/oci/distribution"
	"github.com/eshu-hq/eshu/go/internal/collector/scannerworker"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestRegistryImageAnalyzerScansVerifiedLayer(t *testing.T) {
	stageRoot := t.TempDir()
	t.Setenv("TMPDIR", stageRoot)
	var tarBytes bytes.Buffer
	tw := tar.NewWriter(&tarBytes)
	for name, content := range map[string]string{
		"etc/os-release":       "ID=alpine\nVERSION_ID=3.19.1\n",
		"lib/apk/db/installed": "P:openssl\nV:3.1.4-r5\nA:x86_64\no:openssl\n\n",
	} {
		data := []byte(content)
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(data))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	var layer bytes.Buffer
	gz := gzip.NewWriter(&layer)
	if _, err := gz.Write(tarBytes.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	layerDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(layer.Bytes()))
	configDigest := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte("config")))
	manifest, err := json.Marshal(map[string]any{
		"schemaVersion": 2,
		"mediaType":     "application/vnd.oci.image.manifest.v1+json",
		"config":        map[string]any{"mediaType": "application/vnd.oci.image.config.v1+json", "digest": configDigest, "size": 6},
		"layers":        []map[string]any{{"mediaType": "application/vnd.oci.image.layer.v1.tar+gzip", "digest": layerDigest, "size": layer.Len()}},
	})
	if err != nil {
		t.Fatal(err)
	}
	manifestDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(manifest))
	var configRequests atomic.Int64
	var blockLayer atomic.Bool
	layerRequested := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/manifests/"):
			w.Header().Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
			_, _ = w.Write(manifest)
		case strings.HasSuffix(r.URL.Path, "/blobs/"+configDigest):
			configRequests.Add(1)
			_, _ = w.Write([]byte("config"))
		case strings.Contains(r.URL.Path, "/blobs/"):
			if blockLayer.Load() {
				layerRequested <- struct{}{}
				<-r.Context().Done()
				return
			}
			_, _ = w.Write(layer.Bytes())
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := distribution.NewClient(distribution.ClientConfig{BaseURL: server.URL, BearerToken: "test-secret-never-emitted"})
	if err != nil {
		t.Fatal(err)
	}
	input := testSBOMClaimInput(t)
	input.Analyzer = scannerworker.AnalyzerImageUnpacking
	input.Target.Kind = scannerworker.TargetImage
	input.Limits, err = scannerworker.DefaultResourceLimits(scannerworker.AnalyzerImageUnpacking)
	if err != nil {
		t.Fatal(err)
	}
	parsedURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	domain := registryDomainConfig{
		AuthorizationDomain: "local-test", Registry: parsedURL.Host, BaseURL: server.URL,
		AuthMode: "bearer_env", BearerTokenEnv: "PULL_TOKEN",
	}
	sourceTarget := workflow.ScannerImageTarget{
		WorkItemID: input.WorkItemID, AuthorizationNamespace: workflow.ScannerAuthorizationLocal,
		AuthorizationDomain: domain.AuthorizationDomain, ManifestDigest: manifestDigest,
		Sources: []workflow.ScannerImageSourceBinding{{
			SourceObservationID: 1, ManifestVerificationID: "protected-verification-1",
			Registry: domain.Registry, Repository: "example/app",
			RuntimeDigest: configDigest, RuntimeDigestKind: workflow.ScannerRuntimeDigestConfig,
			RuntimeImageReference: domain.Registry + "/example/app@" + manifestDigest,
			SourceFactID:          "source-fact-should-not-escape", SourceCollectorInstanceID: "source-instance",
			SourceScopeID: "source-scope", SourceGenerationID: "source-generation",
		}},
	}
	sourceTarget.ScanTargetID = workflow.ScannerImageTargetScopeID(sourceTarget)
	sourceTarget.RegistryDomainConfig = registryDomainJSON(t, domain)
	input.Target.ScopeID = sourceTarget.ScanTargetID
	resolver := newRegistryImageResolver(fakeRegistryImageTargetStore{target: sourceTarget}, []registryDomainConfig{domain}, func(key string) string {
		if key == "PULL_TOKEN" {
			return "test-secret-never-emitted"
		}
		return ""
	})
	analyzer := registryImageAnalyzer{resolver: resolver, collectorInstanceID: "scanner-worker-sbom"}
	result, err := analyzer.Analyze(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if result.Output.ResultCount != 1 {
		t.Fatalf("package count = %d, want 1", result.Output.ResultCount)
	}
	if result.Usage.PeakMemoryBytes < int64(len(manifest))+(64<<10) {
		t.Fatalf("tracked memory = %d, want staging manifest and stream buffer included", result.Usage.PeakMemoryBytes)
	}
	if configRequests.Load() != 1 {
		t.Fatalf("config requests = %d, want 1", configRequests.Load())
	}
	if err := scannerworker.ValidateFactOutput(input, result.Output); err != nil {
		t.Fatal(err)
	}
	analysisFound := false
	for _, fact := range result.Output.Facts {
		if fact.FactKind != "scanner_worker.analysis" {
			continue
		}
		analysisFound = true
		if fact.SourceRef.SourceRecordID != sourceTarget.ScanTargetID ||
			fact.Payload["image_digest"] != manifestDigest || fact.Payload["image_reference"] != manifestDigest {
			t.Fatalf("analysis identity = %#v, source record = %q, want manifest and scan target", fact.Payload, fact.SourceRef.SourceRecordID)
		}
	}
	if !analysisFound {
		t.Fatal("scanner analysis fact missing")
	}
	serializedFacts, err := json.Marshal(result.Output)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(serializedFacts, []byte("test-secret-never-emitted")) {
		t.Fatal("registry credential appeared in scanner facts")
	}
	if bytes.Contains(serializedFacts, []byte("source-fact-should-not-escape")) {
		t.Fatal("source binding fact ID appeared in shared scanner facts")
	}
	if bytes.Contains(serializedFacts, []byte("example/app")) {
		t.Fatal("source repository appeared in shared scanner facts")
	}
	if bytes.Contains(serializedFacts, []byte(configDigest)) {
		t.Fatal("runtime config digest appeared in shared scanner facts")
	}
	if !bytes.Contains(serializedFacts, []byte(manifestDigest)) {
		t.Fatal("verified artifact manifest digest missing from shared scanner facts")
	}
	badAnalyzer := registryImageAnalyzer{resolver: registryImageResolverFunc(func(context.Context, scannerworker.ClaimInput) (registryImageTarget, error) {
		return registryImageTarget{
			ScanTargetID: input.Target.ScopeID, Client: client, Repository: "example/app",
			ManifestDigest: manifestDigest, RuntimeDigest: layerDigest, RuntimeDigestKind: "config",
		}, nil
	}), collectorInstanceID: "scanner-worker-sbom"}
	_, err = badAnalyzer.Analyze(context.Background(), input)
	var failure scannerworker.AnalyzerFailure
	if !errors.As(err, &failure) || failure.ResourceUsage().PeakMemoryBytes < int64(len(manifest)) {
		t.Fatalf("failed scan usage = %v, want verified manifest bytes", err)
	}
	blockLayer.Store(true)
	cancelCtx, cancel := context.WithCancel(context.Background())
	resultCh := make(chan error, 1)
	go func() {
		_, runErr := analyzer.Analyze(cancelCtx, input)
		resultCh <- runErr
	}()
	select {
	case <-layerRequested:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("cancel test never reached staged layer fetch")
	}
	cancel()
	select {
	case runErr := <-resultCh:
		if runErr == nil {
			t.Fatal("canceled image acquisition succeeded")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled image acquisition did not stop")
	}
	entries, err := os.ReadDir(stageRoot)
	if err != nil || len(entries) != 0 {
		t.Fatalf("staging entries after scan = %v, err = %v", entries, err)
	}
}

type registryImageResolverFunc func(context.Context, scannerworker.ClaimInput) (registryImageTarget, error)

func (f registryImageResolverFunc) ResolveImage(ctx context.Context, input scannerworker.ClaimInput) (registryImageTarget, error) {
	return f(ctx, input)
}

func TestDecodeImageManifestRejectsWrongRuntimeIdentity(t *testing.T) {
	t.Parallel()
	runtimeDigest := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte("runtime")))
	configDigest := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte("other")))
	raw, err := json.Marshal(map[string]any{
		"schemaVersion": 2,
		"mediaType":     imageManifestMediaType,
		"config":        map[string]any{"digest": configDigest},
		"layers":        []map[string]any{{"mediaType": imageGzipLayerMediaType, "digest": runtimeDigest, "size": 128}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = decodeImageManifest(context.Background(), raw, registryImageTarget{RuntimeDigestKind: "config", RuntimeDigest: runtimeDigest})
	if err == nil {
		t.Fatal("accepted config digest different from observed runtime")
	}
}

func TestDecodeImageManifestRejectsUnknownConfigMediaType(t *testing.T) {
	t.Parallel()
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte("config")))
	raw, err := json.Marshal(map[string]any{
		"schemaVersion": 2,
		"mediaType":     imageManifestMediaType,
		"config":        map[string]any{"mediaType": "application/x-untrusted", "digest": digest, "size": 6},
		"layers":        []map[string]any{{"mediaType": imageGzipLayerMediaType, "digest": digest, "size": 6}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = decodeImageManifest(context.Background(), raw, registryImageTarget{RuntimeDigestKind: "config", RuntimeDigest: digest})
	if err == nil {
		t.Fatal("unknown image config media type accepted")
	}
}

func TestDecodeImageManifestRejectsNestedDuplicateDescriptorKey(t *testing.T) {
	t.Parallel()
	const firstDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const secondDigest = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	raw := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json",` +
		`"config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"` + firstDigest + `",` +
		`"digest":"` + secondDigest + `","size":6},` +
		`"layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar+gzip","digest":"` + firstDigest + `","size":6}]}`)
	_, err := decodeImageManifest(context.Background(), raw, registryImageTarget{RuntimeDigestKind: "config", RuntimeDigest: secondDigest})
	if err == nil {
		t.Fatal("duplicate nested config digest accepted")
	}
}

func TestDecodeImageManifestStopsOnCancellationAndDepth(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := decodeImageManifest(ctx, []byte(`{"schemaVersion":2}`), registryImageTarget{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled manifest decode = %v, want context cancellation", err)
	}
	deep := []byte(strings.Repeat("[", 65) + "0" + strings.Repeat("]", 65))
	if err := validateUniqueManifestJSONKeys(context.Background(), deep); err == nil {
		t.Fatal("overdeep manifest JSON accepted")
	}
	oversized := []byte(`"` + strings.Repeat("x", (1<<20)+1) + `"`)
	if err := validateUniqueManifestJSONKeys(context.Background(), oversized); err == nil {
		t.Fatal("overlarge manifest JSON accepted")
	}
	tooManyTokens := []byte("[" + strings.TrimSuffix(strings.Repeat("0,", maxManifestJSONTokens), ",") + "]")
	if err := validateUniqueManifestJSONKeys(context.Background(), tooManyTokens); err == nil {
		t.Fatal("manifest JSON token cap was not enforced")
	}
	cancelDuringParse := &cancelAfterManifestChecks{Context: context.Background()}
	manyValues := []byte("[" + strings.TrimSuffix(strings.Repeat("0,", 300), ",") + "]")
	if err := validateUniqueManifestJSONKeys(cancelDuringParse, manyValues); !errors.Is(err, context.Canceled) {
		t.Fatalf("manifest JSON parse cancellation = %v, want canceled", err)
	}
}

type cancelAfterManifestChecks struct {
	context.Context
	checks int
}

func (ctx *cancelAfterManifestChecks) Err() error {
	ctx.checks++
	if ctx.checks >= 2 {
		return context.Canceled
	}
	return nil
}

func TestExpandGzipLayerStopsAtExpandedByteCap(t *testing.T) {
	t.Parallel()
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(bytes.Repeat([]byte("x"), 1<<20)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	source := t.TempDir() + "/compressed"
	target := t.TempDir() + "/expanded"
	if err := os.WriteFile(source, compressed.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	size, err := expandGzipLayer(context.Background(), source, target, 64<<10)
	if err == nil {
		t.Fatal("expanded byte cap accepted oversized layer")
	}
	if !errors.Is(err, errExpandedLayerLimit) {
		t.Fatalf("expanded layer error = %v, want byte cap", err)
	}
	if size > 64<<10 {
		t.Fatalf("written bytes = %d, cap = %d", size, 64<<10)
	}
}

func TestRegistryImageAnalyzerRejectsRevokedTarget(t *testing.T) {
	t.Parallel()
	input := testSBOMClaimInput(t)
	input.Analyzer = scannerworker.AnalyzerImageUnpacking
	input.Target.Kind = scannerworker.TargetImage
	analyzer := registryImageAnalyzer{resolver: registryImageResolverFunc(func(context.Context, scannerworker.ClaimInput) (registryImageTarget, error) {
		return registryImageTarget{}, errRegistryImageUnauthorized
	}), collectorInstanceID: "scanner-worker-sbom"}
	_, err := analyzer.Analyze(context.Background(), input)
	var failure scannerworker.AnalyzerFailure
	if !errors.As(err, &failure) || failure.Retryable() || failure.FailureClass() != scannerworker.FailureClassUnsupportedTarget {
		t.Fatalf("Analyze() error = %v, want terminal unsupported target", err)
	}
}

func TestExpandGzipLayerHonorsCancellation(t *testing.T) {
	t.Parallel()
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(bytes.Repeat([]byte("x"), 64<<10)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	source := t.TempDir() + "/compressed"
	if err := os.WriteFile(source, compressed.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := expandGzipLayer(ctx, source, t.TempDir()+"/expanded", 1<<20)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expandGzipLayer() error = %v, want canceled", err)
	}
}

func TestExpandGzipLayerClassifiesStagingFailure(t *testing.T) {
	t.Parallel()
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write([]byte("layer")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	source := t.TempDir() + "/compressed"
	if err := os.WriteFile(source, compressed.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := expandGzipLayer(context.Background(), source, t.TempDir(), 1024)
	if !errors.Is(err, errExpandedLayerStaging) {
		t.Fatalf("staging error = %v, want retryable staging class", err)
	}
}
