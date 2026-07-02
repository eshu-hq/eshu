// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

func TestResolveNativeSnapshotFileSetSkipsLegacyVendoredLibraries(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoRoot, ".git", "HEAD"), "ref: refs/heads/main\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "src", "jquery_adapter.js"), "export function adaptJQuery() { return true; }\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "src", "bootstrap.js"), "export function bootstrapApplication() { return true; }\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "src", "aurigma", "aurigma.custom.js"), "export function authoredUploader() { return true; }\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "src", "mapcontrol.js"), "export function mapControl() { return true; }\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "src", "plupload", "js", "plupload.app.js"), "export function authoredPluploadIntegration() { return true; }\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "src", "less.js"), "export function compileProjectLess() { return true; }\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "src", "camera.js"), "export function cameraWorkflow() { return true; }\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "src", "jssor.slider.js"), "export function projectSlider() { return true; }\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "src", "mootools.js"), "export function toolCatalog() { return true; }\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "src", "sharethis.js"), "export function shareThisListing() { return true; }\n")
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
	writeCollectorTestFile(t, filepath.Join(repoRoot, "public", "js", "effects.js"), "// script.aculo.us effects.js v1.8.2\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "public", "js", "less.min.js"), "/* LESS - Leaner CSS v1.3.0\n * http://lesscss.org\n */\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "public", "js", "camera.js"), "// Camera slideshow v1.4.0 - a jQuery slideshow\n// www.pixedelic.com\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "public", "js", "jssor.slider.mini.js"), "/* Jssor Slider 22.0.15 */\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "public", "js", "mootools.js"), "/* MooTools Core v1.4.5 */\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "public", "js", "sharethis.js"), "/* ShareThis Buttons */\nwindow.__sharethis__ = {};\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "public", "js", "mapcontrol.js"), "var L_BingLogoTooltip_Text=\"Bing Maps\";MapControl.Features={PlatformName:\"Virtual Earth\"};\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "public", "plupload", "js", "plupload.full.js"), "/*1.5.4*/\n(function(){var g={VERSION:\"1.5.4\"};window.plupload=g})();\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "legacy", "aurigma", "aurigma.htmluploader.control.js"), "(function(){function htmlUploaderControl(){return true;}})();\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "legacy", "aurigma", "aurigma.imageuploaderflash.js"), "(function(){function imageUploaderFlash(){return true;}})();\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "scripts", "fpdf.php"), "<?php\n/* FPDF */\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "framework", "library", "pear", "php", "PEAR", "FixPHP5PEARWarnings.php"), "<?php\n/* PEAR compatibility library */\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "framework", "library", "pear", "php", "phing.php"), "<?php\n/* Phing entrypoint */\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "framework", "library", "pear", "php", "phing", "types", "AbstractFileSet.php"), "<?php\n/* Phing build-tool library */\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "framework", "library", "CAS", "CAS", "client.php"), "<?php\n/* phpCAS client library */\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "framework", "library", "minify", "min", "lib", "JSMinPlus.php"), "<?php\n/** JSMinPlus version 1.1 */\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "framework", "library", "FusionCharts", "Code", "PHPClass", "Class", "FusionCharts_Gen.php"), "<?php\n/* FUSIONCHARTS v3 API PHP CLASS */\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "framework", "library", "FusionCharts", "Code", "JavaScript", "FusionCharts.js"), "function FusionCharts() { return true; }\n")

	resolvedRepoRoot, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		resolvedRepoRoot = repoRoot
	}
	registry := parser.DefaultRegistry()
	fileSet, stats, err := resolveNativeSnapshotFileSet(resolvedRepoRoot, registry, NativeRepositorySnapshotter{}.discoveryOptions())
	if err != nil {
		t.Fatalf("resolveNativeSnapshotFileSet() error = %v", err)
	}

	if got, want := len(fileSet.Files), 10; got != want {
		t.Fatalf("file count = %d, want %d; files=%v", got, want, fileSet.Files)
	}
	for _, wantSuffix := range []string{
		"src/aurigma/aurigma.custom.js",
		"src/bootstrap.js",
		"src/camera.js",
		"src/jssor.slider.js",
		"src/jquery_adapter.js",
		"src/less.js",
		"src/mapcontrol.js",
		"src/mootools.js",
		"src/plupload/js/plupload.app.js",
		"src/sharethis.js",
	} {
		if !fileSetContainsSuffix(fileSet.Files, wantSuffix) {
			t.Fatalf("fileSet missing %q; files=%v", wantSuffix, fileSet.Files)
		}
	}
	if got := stats.FilesSkippedByContent["vendored-zend-framework"]; got != 1 {
		t.Fatalf("FilesSkippedByContent[vendored-zend-framework] = %d, want 1", got)
	}
	if got := stats.FilesSkippedByContent["vendored-browser-library"]; got != 18 {
		t.Fatalf("FilesSkippedByContent[vendored-browser-library] = %d, want 18", got)
	}
	if got := stats.FilesSkippedByContent["vendored-fpdf"]; got != 1 {
		t.Fatalf("FilesSkippedByContent[vendored-fpdf] = %d, want 1", got)
	}
	if got := stats.FilesSkippedByContent["vendored-pear"]; got != 3 {
		t.Fatalf("FilesSkippedByContent[vendored-pear] = %d, want 3", got)
	}
	if got := stats.FilesSkippedByContent["vendored-plupload"]; got != 1 {
		t.Fatalf("FilesSkippedByContent[vendored-plupload] = %d, want 1", got)
	}
	if got := stats.FilesSkippedByContent["vendored-aurigma"]; got != 2 {
		t.Fatalf("FilesSkippedByContent[vendored-aurigma] = %d, want 2", got)
	}
	if got := stats.FilesSkippedByContent["vendored-phpcas"]; got != 1 {
		t.Fatalf("FilesSkippedByContent[vendored-phpcas] = %d, want 1", got)
	}
	if got := stats.FilesSkippedByContent["vendored-minify"]; got != 1 {
		t.Fatalf("FilesSkippedByContent[vendored-minify] = %d, want 1", got)
	}
	if got := stats.FilesSkippedByContent["vendored-fusioncharts"]; got != 2 {
		t.Fatalf("FilesSkippedByContent[vendored-fusioncharts] = %d, want 2", got)
	}
	if got := stats.FilesSkippedByContent["vendored-mapcontrol"]; got != 1 {
		t.Fatalf("FilesSkippedByContent[vendored-mapcontrol] = %d, want 1", got)
	}
}
