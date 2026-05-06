package collector

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

func TestNativeRepositorySnapshotterDefaultDiscoverySkipsDependencyDirs(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()

	// Source file that should be indexed.
	writeCollectorTestFile(t, filepath.Join(repoRoot, "main.py"), "def main(): pass\n")

	// Files inside dependency dirs that should be skipped by default.
	for _, dir := range []string{
		"node_modules", "vendor", "__pycache__", "site-packages",
		".terraform", ".terragrunt-cache", "dist", "build", "Pods",
		"ansible_collections", ".jenkins", ".yarn", "wp-admin", "wp-includes",
	} {
		writeCollectorTestFile(t, filepath.Join(repoRoot, dir, "dep.py"), "def dep(): pass\n")
	}
	writeCollectorTestFile(t, filepath.Join(repoRoot, ".yarn", "releases", "yarn-4.13.0.cjs"), "module.exports = {}\n")

	// Files with ignored extensions that should be skipped.
	writeCollectorTestFile(t, filepath.Join(repoRoot, "server.log"), "log line\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "test.out"), "output\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "app.min.js"), "minified\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "pdf.worker.min.mjs"), "minified\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "style.min.css"), "minified\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "bundle.js.map"), "sourcemap\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "lib.pyc"), "compiled\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, ".pnp.cjs"), "module.exports = {}\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, ".pnp.loader.mjs"), "export default {}\n")

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v", err)
	}

	resolvedRepoRoot, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		resolvedRepoRoot = repoRoot
	}

	now := time.Date(2026, time.April, 15, 12, 0, 0, 0, time.UTC)
	snapshotter := NativeRepositorySnapshotter{
		Engine: engine,
		Now:    func() time.Time { return now },
	}

	got, err := snapshotter.SnapshotRepository(
		context.Background(),
		SelectedRepository{RepoPath: resolvedRepoRoot},
	)
	if err != nil {
		t.Fatalf("SnapshotOneRepository() error = %v", err)
	}

	// Only main.py should be discovered — all dependency dirs should be pruned.
	if got.FileCount != 1 {
		t.Errorf("FileCount = %d, want 1 (only main.py); dependency dirs were not skipped", got.FileCount)
	}
	for _, entity := range got.ContentEntities {
		if entity.EntityName == "dep" {
			t.Errorf("found entity %q from dependency dir; default ignored dirs not applied", entity.RelativePath)
		}
	}
}

func TestResolveNativeSnapshotFileSetSkipsLargeWebpackBundles(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoRoot, ".git", "HEAD"), "ref: refs/heads/main\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "src", "app.js"), "export function app() { return 'source'; }\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "public", "js", "app.js"), largeWebpackBootstrapFixture())

	resolvedRepoRoot, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		resolvedRepoRoot = repoRoot
	}
	registry := parser.DefaultRegistry()
	fileSet, stats, err := resolveNativeSnapshotFileSet(resolvedRepoRoot, registry, NativeRepositorySnapshotter{}.discoveryOptions())
	if err != nil {
		t.Fatalf("resolveNativeSnapshotFileSet() error = %v", err)
	}

	if got, want := len(fileSet.Files), 1; got != want {
		t.Fatalf("file count = %d, want %d; files=%v", got, want, fileSet.Files)
	}
	if got, want := filepath.ToSlash(fileSet.Files[0]), "src/app.js"; !strings.HasSuffix(got, want) {
		t.Fatalf("indexed file = %q, want suffix %q", got, want)
	}
	if got := stats.FilesSkippedByContent["generated-webpack"]; got != 1 {
		t.Fatalf("FilesSkippedByContent[generated-webpack] = %d, want 1", got)
	}
}

func TestResolveNativeSnapshotFileSetSkipsLargeGeneratedJavaScriptBundles(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoRoot, ".git", "HEAD"), "ref: refs/heads/main\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "src", "app.js"), "export function app() { return 'source'; }\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "public", "js", "webpack5.js"), largeWebpack5BootstrapFixture())
	writeCollectorTestFile(t, filepath.Join(repoRoot, "public", "js", "rollup.js"), largeRollupBootstrapFixture())
	writeCollectorTestFile(t, filepath.Join(repoRoot, "public", "js", "esbuild.js"), largeESBuildBootstrapFixture())
	writeCollectorTestFile(t, filepath.Join(repoRoot, "public", "js", "parcel.js"), largeParcelBootstrapFixture())

	resolvedRepoRoot, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		resolvedRepoRoot = repoRoot
	}
	registry := parser.DefaultRegistry()
	fileSet, stats, err := resolveNativeSnapshotFileSet(resolvedRepoRoot, registry, NativeRepositorySnapshotter{}.discoveryOptions())
	if err != nil {
		t.Fatalf("resolveNativeSnapshotFileSet() error = %v", err)
	}

	if got, want := len(fileSet.Files), 1; got != want {
		t.Fatalf("file count = %d, want %d; files=%v", got, want, fileSet.Files)
	}
	if got, want := filepath.ToSlash(fileSet.Files[0]), "src/app.js"; !strings.HasSuffix(got, want) {
		t.Fatalf("indexed file = %q, want suffix %q", got, want)
	}
	for reason, want := range map[string]int{
		"generated-webpack": 1,
		"generated-rollup":  1,
		"generated-esbuild": 1,
		"generated-parcel":  1,
	} {
		if got := stats.FilesSkippedByContent[reason]; got != want {
			t.Fatalf("FilesSkippedByContent[%s] = %d, want %d", reason, got, want)
		}
	}
}

func TestResolveNativeSnapshotFileSetKeepsLargeAuthoredJavaScript(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoRoot, ".git", "HEAD"), "ref: refs/heads/main\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "src", "big_authored.js"), largeAuthoredJavaScriptFixture())

	resolvedRepoRoot, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		resolvedRepoRoot = repoRoot
	}
	registry := parser.DefaultRegistry()
	fileSet, stats, err := resolveNativeSnapshotFileSet(resolvedRepoRoot, registry, NativeRepositorySnapshotter{}.discoveryOptions())
	if err != nil {
		t.Fatalf("resolveNativeSnapshotFileSet() error = %v", err)
	}

	if got, want := len(fileSet.Files), 1; got != want {
		t.Fatalf("file count = %d, want %d; files=%v", got, want, fileSet.Files)
	}
	if got, want := filepath.ToSlash(fileSet.Files[0]), "src/big_authored.js"; !strings.HasSuffix(got, want) {
		t.Fatalf("indexed file = %q, want suffix %q", got, want)
	}
	if got := len(stats.FilesSkippedByContent); got != 0 {
		t.Fatalf("FilesSkippedByContent length = %d, want 0; stats=%v", got, stats.FilesSkippedByContent)
	}
}

func TestResolveNativeSnapshotFileSetKeepsSmallBootstrapLikeJavaScript(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoRoot, ".git", "HEAD"), "ref: refs/heads/main\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "src", "tiny_runtime.js"), smallWebpackBootstrapLikeFixture())

	resolvedRepoRoot, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		resolvedRepoRoot = repoRoot
	}
	registry := parser.DefaultRegistry()
	fileSet, stats, err := resolveNativeSnapshotFileSet(resolvedRepoRoot, registry, NativeRepositorySnapshotter{}.discoveryOptions())
	if err != nil {
		t.Fatalf("resolveNativeSnapshotFileSet() error = %v", err)
	}

	if got, want := len(fileSet.Files), 1; got != want {
		t.Fatalf("file count = %d, want %d; files=%v", got, want, fileSet.Files)
	}
	if got, want := filepath.ToSlash(fileSet.Files[0]), "src/tiny_runtime.js"; !strings.HasSuffix(got, want) {
		t.Fatalf("indexed file = %q, want suffix %q", got, want)
	}
	if got := len(stats.FilesSkippedByContent); got != 0 {
		t.Fatalf("FilesSkippedByContent length = %d, want 0; stats=%v", got, stats.FilesSkippedByContent)
	}
}

func TestResolveNativeSnapshotFileSetSkipsLegacyVendoredLibraries(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoRoot, ".git", "HEAD"), "ref: refs/heads/main\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "src", "jquery_adapter.js"), "export function adaptJQuery() { return true; }\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "src", "bootstrap.js"), "export function bootstrapApplication() { return true; }\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "src", "marinus", "library", "Zend", "Gdata", "GroupEntry.php"), "<?php\n/** Zend Framework */\nclass Zend_Gdata_GroupEntry {}\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "public", "js", "jquery.js"), "/* jQuery JavaScript Library v1.12.4 */\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "public", "js", "aJQuerry.js"), "/*! jQuery v2.2.4 | (c) jQuery Foundation | jquery.org/license */\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "public", "js", "bootstrap.js"), "/*!\n * Bootstrap v3.3.1 (http://getbootstrap.com)\n */\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "public", "js", "calendar.js"), "/*!\nFullCalendar v5.3.2\nDocs & License: https://fullcalendar.io/\n*/\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "public", "js", "fotorama.js"), "/*!\n * Fotorama 4.6.4 | http://fotorama.io/license/\n */\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "public", "js", "gmaps.js"), "/*!\n * GMaps.js v0.4.15\n * http://hpneo.github.com/gmaps/\n */\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "public", "js", "masonry.pkgd.js"), "/*!\n * Masonry PACKAGED v3.1.5\n * http://masonry.desandro.com\n */\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "public", "js", "jwplayer.js"), "jwplayer.version=\"6.12.4956\";\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "public", "js", "jwplayer", "provider.shaka.js"), "function jwPlayerProvider() { return \"shaka\"; }\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "public", "js", "filepond.esm.js"), "/*!\n * FilePond 4.30.6\n * Please visit https://pqina.nl/filepond/ for details.\n */\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "public", "js", "prototype.js"), "/* Prototype JavaScript framework, version 1.6.0\n * http://www.prototypejs.org/\n */\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "public", "js", "reveal.js"), "/*!\n * reveal.js\n * http://lab.hakim.se/reveal-js\n */\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "public", "js", "shadowbox.js"), "/* Shadowbox.js */\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "scripts", "fpdf.php"), "<?php\n/* FPDF */\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "framework", "library", "pear", "php", "PEAR", "FixPHP5PEARWarnings.php"), "<?php\n/* PEAR compatibility library */\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "framework", "library", "pear", "php", "phing.php"), "<?php\n/* Phing entrypoint */\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "framework", "library", "pear", "php", "phing", "types", "AbstractFileSet.php"), "<?php\n/* Phing build-tool library */\n")

	resolvedRepoRoot, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		resolvedRepoRoot = repoRoot
	}
	registry := parser.DefaultRegistry()
	fileSet, stats, err := resolveNativeSnapshotFileSet(resolvedRepoRoot, registry, NativeRepositorySnapshotter{}.discoveryOptions())
	if err != nil {
		t.Fatalf("resolveNativeSnapshotFileSet() error = %v", err)
	}

	if got, want := len(fileSet.Files), 2; got != want {
		t.Fatalf("file count = %d, want %d; files=%v", got, want, fileSet.Files)
	}
	for _, wantSuffix := range []string{
		"src/bootstrap.js",
		"src/jquery_adapter.js",
	} {
		if !fileSetContainsSuffix(fileSet.Files, wantSuffix) {
			t.Fatalf("fileSet missing %q; files=%v", wantSuffix, fileSet.Files)
		}
	}
	if got := stats.FilesSkippedByContent["vendored-zend-framework"]; got != 1 {
		t.Fatalf("FilesSkippedByContent[vendored-zend-framework] = %d, want 1", got)
	}
	if got := stats.FilesSkippedByContent["vendored-browser-library"]; got != 13 {
		t.Fatalf("FilesSkippedByContent[vendored-browser-library] = %d, want 13", got)
	}
	if got := stats.FilesSkippedByContent["vendored-fpdf"]; got != 1 {
		t.Fatalf("FilesSkippedByContent[vendored-fpdf] = %d, want 1", got)
	}
	if got := stats.FilesSkippedByContent["vendored-pear"]; got != 3 {
		t.Fatalf("FilesSkippedByContent[vendored-pear] = %d, want 3", got)
	}
}

func TestResolveNativeSnapshotFileSetSkipsConfiguredVendoredRoots(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoRoot, ".git", "HEAD"), "ref: refs/heads/main\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, ".eshu", "vendor-roots.json"), `{
  "vendor_roots": [
    {"path": "src/wp-content/plugins/**", "reason": "wordpress-plugin"}
  ],
  "keep_roots": [
    {"path": "src/wp-content/plugins/custom-authored/**"}
  ]
}
`)
	writeCollectorTestFile(t, filepath.Join(repoRoot, "src", "app.php"), "<?php\nfunction app_authored() {}\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "src", "wp-content", "plugins", "wordpress-seo", "src", "blocks.php"), "<?php\nfunction yoast_vendor() {}\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "src", "wp-content", "plugins", "custom-authored", "plugin.php"), "<?php\nfunction custom_authored() {}\n")

	resolvedRepoRoot, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		resolvedRepoRoot = repoRoot
	}
	registry := parser.DefaultRegistry()
	fileSet, stats, err := resolveNativeSnapshotFileSet(resolvedRepoRoot, registry, NativeRepositorySnapshotter{}.discoveryOptions())
	if err != nil {
		t.Fatalf("resolveNativeSnapshotFileSet() error = %v", err)
	}

	if got, want := len(fileSet.Files), 2; got != want {
		t.Fatalf("file count = %d, want %d; files=%v", got, want, fileSet.Files)
	}
	for _, wantSuffix := range []string{
		"src/app.php",
		"src/wp-content/plugins/custom-authored/plugin.php",
	} {
		if !fileSetContainsSuffix(fileSet.Files, wantSuffix) {
			t.Fatalf("fileSet missing %q; files=%v", wantSuffix, fileSet.Files)
		}
	}
	if got := stats.DirsSkippedByUser["wordpress-plugin"]; got != 1 {
		t.Fatalf("DirsSkippedByUser[wordpress-plugin] = %d, want 1", got)
	}
}

func TestResolveNativeSnapshotFileSetSkipsStructuredDiscoveryConfig(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoRoot, ".git", "HEAD"), "ref: refs/heads/main\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, ".eshu", "discovery.json"), `{
  "ignored_path_globs": [
    {"path": "public/archive/**", "reason": "archived-site-copy"}
  ],
  "preserved_path_globs": [
    {"path": "public/archive/custom-authored/**"}
  ],
  "vendor_roots": [
    {"path": "public/js/fotorama.js", "reason": "static-browser-library"}
  ],
  "keep_roots": [
    {"path": "public/js/site.js"}
  ]
}
`)
	writeCollectorTestFile(t, filepath.Join(repoRoot, "public", "index.php"), "<?php\nfunction home_page() {}\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "public", "archive", "old.php"), "<?php\nfunction old_archive() {}\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "public", "archive", "custom-authored", "kept.php"), "<?php\nfunction kept_archive() {}\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "public", "js", "fotorama.js"), "function fotoramaVendor() {}\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "public", "js", "site.js"), "function authoredSite() {}\n")

	resolvedRepoRoot, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		resolvedRepoRoot = repoRoot
	}
	registry := parser.DefaultRegistry()
	fileSet, stats, err := resolveNativeSnapshotFileSet(resolvedRepoRoot, registry, NativeRepositorySnapshotter{}.discoveryOptions())
	if err != nil {
		t.Fatalf("resolveNativeSnapshotFileSet() error = %v", err)
	}

	for _, wantSuffix := range []string{
		"public/index.php",
		"public/archive/custom-authored/kept.php",
		"public/js/site.js",
	} {
		if !fileSetContainsSuffix(fileSet.Files, wantSuffix) {
			t.Fatalf("fileSet missing %q; files=%v", wantSuffix, fileSet.Files)
		}
	}
	for _, unwantedSuffix := range []string{
		"public/archive/old.php",
		"public/js/fotorama.js",
	} {
		if fileSetContainsSuffix(fileSet.Files, unwantedSuffix) {
			t.Fatalf("fileSet unexpectedly contains %q; files=%v", unwantedSuffix, fileSet.Files)
		}
	}
	if got := stats.DirsSkippedByUser["archived-site-copy"]; got != 0 {
		t.Fatalf("DirsSkippedByUser[archived-site-copy] = %d, want 0", got)
	}
	if got := stats.FilesSkippedByUser["archived-site-copy"]; got != 1 {
		t.Fatalf("FilesSkippedByUser[archived-site-copy] = %d, want 1", got)
	}
	if got := stats.FilesSkippedByUser["static-browser-library"]; got != 1 {
		t.Fatalf("FilesSkippedByUser[static-browser-library] = %d, want 1", got)
	}
}

func TestResolveNativeSnapshotFileSetMergesLegacyAndStructuredDiscoveryConfig(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoRoot, ".git", "HEAD"), "ref: refs/heads/main\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, ".eshu", "vendor-roots.json"), `{
  "vendor_roots": [
    {"path": "legacy/third_party/**", "reason": "legacy-vendor"}
  ]
}
`)
	writeCollectorTestFile(t, filepath.Join(repoRoot, ".eshu", "discovery.json"), `{
  "ignored_path_globs": [
    {"path": "public/old_site/**", "reason": "archived-site-copy"}
  ]
}
`)
	writeCollectorTestFile(t, filepath.Join(repoRoot, "src", "app.php"), "<?php\nfunction app_authored() {}\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "legacy", "third_party", "lib.php"), "<?php\nfunction legacy_vendor() {}\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "public", "old_site", "old.php"), "<?php\nfunction old_archive() {}\n")

	resolvedRepoRoot, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		resolvedRepoRoot = repoRoot
	}
	registry := parser.DefaultRegistry()
	fileSet, stats, err := resolveNativeSnapshotFileSet(resolvedRepoRoot, registry, NativeRepositorySnapshotter{}.discoveryOptions())
	if err != nil {
		t.Fatalf("resolveNativeSnapshotFileSet() error = %v", err)
	}

	if got, want := len(fileSet.Files), 1; got != want {
		t.Fatalf("file count = %d, want %d; files=%v", got, want, fileSet.Files)
	}
	if !fileSetContainsSuffix(fileSet.Files, "src/app.php") {
		t.Fatalf("fileSet missing authored file; files=%v", fileSet.Files)
	}
	if got := stats.DirsSkippedByUser["legacy-vendor"]; got != 1 {
		t.Fatalf("DirsSkippedByUser[legacy-vendor] = %d, want 1", got)
	}
	if got := stats.DirsSkippedByUser["archived-site-copy"]; got != 1 {
		t.Fatalf("DirsSkippedByUser[archived-site-copy] = %d, want 1", got)
	}
}

func TestNativeRepositorySnapshotterBuildsDiscoveryAdvisory(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoRoot, ".git", "HEAD"), "ref: refs/heads/main\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, ".eshu", "discovery.json"), `{
  "ignored_path_globs": [
    {"path": "old/**", "reason": "archived-site-copy"}
  ]
}
`)
	writeCollectorTestFile(t, filepath.Join(repoRoot, "src", "app.py"), "def handler():\n    return 1\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "src", "models.py"), "class Widget:\n    pass\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "old", "copy.py"), "def old_handler():\n    return 1\n")

	resolvedRepoRoot, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		resolvedRepoRoot = repoRoot
	}
	now := time.Date(2026, time.April, 26, 10, 30, 0, 0, time.UTC)
	snapshotter := NativeRepositorySnapshotter{
		Now: func() time.Time { return now },
	}

	got, err := snapshotter.SnapshotRepository(context.Background(), SelectedRepository{RepoPath: resolvedRepoRoot})
	if err != nil {
		t.Fatalf("SnapshotRepository() error = %v, want nil", err)
	}
	report := got.DiscoveryAdvisory
	if report == nil {
		t.Fatal("DiscoveryAdvisory = nil, want report")
	}
	if got, want := report.SchemaVersion, "discovery_advisory.v1"; got != want {
		t.Fatalf("SchemaVersion = %q, want %q", got, want)
	}
	if got, want := report.GeneratedAt, now; !got.Equal(want) {
		t.Fatalf("GeneratedAt = %v, want %v", got, want)
	}
	if got, want := report.Summary.DiscoveredFiles, 2; got != want {
		t.Fatalf("Summary.DiscoveredFiles = %d, want %d", got, want)
	}
	if got, want := report.Summary.ContentEntities, 2; got != want {
		t.Fatalf("Summary.ContentEntities = %d, want %d", got, want)
	}
	if got, want := report.SkipBreakdown.DirsByUser["archived-site-copy"], 1; got != want {
		t.Fatalf("DirsByUser[archived-site-copy] = %d, want %d", got, want)
	}
	if got, want := report.EntityCounts.ByType["Function"], 1; got != want {
		t.Fatalf("EntityCounts.ByType[Function] = %d, want %d", got, want)
	}
	if got, want := report.EntityCounts.ByLanguage["python"], 2; got != want {
		t.Fatalf("EntityCounts.ByLanguage[python] = %d, want %d", got, want)
	}
	if len(report.TopNoisyFiles) == 0 || report.TopNoisyFiles[0].Path != "src/app.py" {
		t.Fatalf("TopNoisyFiles = %#v, want src/app.py first", report.TopNoisyFiles)
	}
}

func largeAuthoredJavaScriptFixture() string {
	header := "export function authoredLargeModule() { return 'authored'; }\n"
	return header + strings.Repeat("export const authoredSymbol = 1;\n", 12000)
}

func fileSetContainsSuffix(files []string, suffix string) bool {
	for _, file := range files {
		if strings.HasSuffix(filepath.ToSlash(file), suffix) {
			return true
		}
	}
	return false
}

func smallWebpackBootstrapLikeFixture() string {
	return "/******/ (function(modules) { // webpackBootstrap\n" +
		"/******/ \tvar installedModules = {};\n" +
		"/******/ \tfunction __webpack_require__(moduleId) { return modules[moduleId]; }\n" +
		"export function tinyRuntime() { return __webpack_require__; }\n"
}

func largeWebpackBootstrapFixture() string {
	header := "/******/ (function(modules) { // webpackBootstrap\n" +
		"/******/ \tvar installedModules = {};\n" +
		"/******/ \tfunction __webpack_require__(moduleId) { return modules[moduleId]; }\n"
	return header + strings.Repeat("var generatedBundleChunk = 1;\n", 12000)
}

func largeWebpack5BootstrapFixture() string {
	header := "/******/ (() => { // webpackBootstrap\n" +
		"/******/ \tvar __webpack_modules__ = ({})\n" +
		"/******/ \tvar __webpack_module_cache__ = {};\n" +
		"/******/ \tfunction __webpack_require__(moduleId) { return __webpack_modules__[moduleId]; }\n"
	return header + strings.Repeat("var generatedBundleChunk = 1;\n", 12000)
}

func largeRollupBootstrapFixture() string {
	header := "var commonjsGlobal = typeof globalThis !== 'undefined' ? globalThis : typeof window !== 'undefined' ? window : global;\n" +
		"function getDefaultExportFromCjs (x) { return x && x.__esModule && Object.prototype.hasOwnProperty.call(x, 'default') ? x['default'] : x; }\n" +
		"function getAugmentedNamespace(n) { var a = Object.create(null); if (n) Object.keys(n).forEach(function (k) { a[k] = n[k]; }); Object.defineProperty(a, '__esModule', { value: true }); return a; }\n" +
		"function commonjsRequire(path) { throw new Error('Could not dynamically require ' + path); }\n"
	return header + strings.Repeat("var generatedBundleChunk = 1;\n", 12000)
}

func largeESBuildBootstrapFixture() string {
	header := "var __defProp = Object.defineProperty;\n" +
		"var __getOwnPropNames = Object.getOwnPropertyNames;\n" +
		"var __commonJS = (cb, mod) => function __require() { return mod || cb((mod = { exports: {} }).exports, mod), mod.exports; };\n" +
		"var __copyProps = (to, from, except, desc) => { if (from && typeof from === 'object') for (let key of __getOwnPropNames(from)) if (!Object.prototype.hasOwnProperty.call(to, key) && key !== except) __defProp(to, key, { get: () => from[key], enumerable: !(desc = Object.getOwnPropertyDescriptor(from, key)) || desc.enumerable }); return to; };\n" +
		"var __toESM = (mod, isNodeMode, target) => (target = mod != null ? Object.create(Object.getPrototypeOf(mod)) : {}, __copyProps(isNodeMode || !mod || !mod.__esModule ? __defProp(target, 'default', { value: mod, enumerable: true }) : target, mod));\n"
	return header + strings.Repeat("var generatedBundleChunk = 1;\n", 12000)
}

func largeParcelBootstrapFixture() string {
	header := "var $parcel$global = typeof globalThis !== 'undefined' ? globalThis : typeof self !== 'undefined' ? self : window;\n" +
		"parcelRequire = (function (modules, cache, entry, globalName) {\n" +
		"function Module(moduleName) { this.id = moduleName; this.bundle = newRequire; this.exports = {}; }\n" +
		"function newRequire(name, jumped) { if(!cache[name]) { var module = cache[name] = new newRequire.Module(name); modules[name][0].call(module.exports, function(x){ return newRequire(modules[name][1][x] || x); }, module, module.exports); } return cache[name].exports; }\n" +
		"newRequire.isParcelRequire = true;\n" +
		"newRequire.Module = Module;\n" +
		"for (var i = 0; i < entry.length; i++) newRequire(entry[i]);\n"
	return header + strings.Repeat("var generatedBundleChunk = 1;\n", 12000)
}
