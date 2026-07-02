// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/discovery"
)

const (
	generatedJavaScriptBundleMinBytes = 256 * 1024
	vendoredBrowserLibraryPrefixBytes = 16 * 1024
)

func filterGeneratedNativeSnapshotFiles(files []string, stats *discovery.DiscoveryStats) []string {
	filtered := files[:0]
	for _, path := range files {
		if reason, ok := generatedNativeSnapshotSkipReason(path); ok {
			if stats.FilesSkippedByContent == nil {
				stats.FilesSkippedByContent = make(map[string]int)
			}
			stats.FilesSkippedByContent[reason]++
			continue
		}
		filtered = append(filtered, path)
	}
	return filtered
}

func generatedNativeSnapshotSkipReason(path string) (string, bool) {
	if isVendoredZendFrameworkFile(path) {
		return "vendored-zend-framework", true
	}
	if isVendoredPluploadFile(path) {
		return "vendored-plupload", true
	}
	if isVendoredAurigmaFile(path) {
		return "vendored-aurigma", true
	}
	if isVendoredPHPCASFile(path) {
		return "vendored-phpcas", true
	}
	if isVendoredMinifyFile(path) {
		return "vendored-minify", true
	}
	if isVendoredFusionChartsFile(path) {
		return "vendored-fusioncharts", true
	}
	if isVendoredMapControlFile(path) {
		return "vendored-mapcontrol", true
	}
	if isVendoredBrowserLibraryFile(path) {
		return "vendored-browser-library", true
	}
	if isVendoredFPDFFile(path) {
		return "vendored-fpdf", true
	}
	if isVendoredPEARFile(path) {
		return "vendored-pear", true
	}
	prefix, ok := largeJavaScriptBundlePrefix(path)
	if !ok {
		return "", false
	}
	switch {
	case isWebpackBootstrapPrefix(prefix):
		return "generated-webpack", true
	case isRollupBootstrapPrefix(prefix):
		return "generated-rollup", true
	case isESBuildBootstrapPrefix(prefix):
		return "generated-esbuild", true
	case isParcelBootstrapPrefix(prefix):
		return "generated-parcel", true
	}
	return "", false
}

func largeJavaScriptBundlePrefix(path string) (string, bool) {
	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".js" && ext != ".cjs" && ext != ".mjs" {
		return "", false
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() < generatedJavaScriptBundleMinBytes {
		return "", false
	}
	file, err := os.Open(path) // #nosec G304 -- reads indexed repo file at a path derived from the scan target, not user-supplied input
	if err != nil {
		return "", false
	}
	defer func() { _ = file.Close() }()

	buf := make([]byte, 8192)
	n, err := file.Read(buf)
	if err != nil && n == 0 {
		return "", false
	}
	return string(buf[:n]), true
}

func isWebpackBootstrapPrefix(prefix string) bool {
	return strings.Contains(prefix, "webpackBootstrap") &&
		strings.Contains(prefix, "/******/") &&
		(strings.Contains(prefix, "installedModules") ||
			(strings.Contains(prefix, "__webpack_modules__") &&
				strings.Contains(prefix, "__webpack_module_cache__") &&
				strings.Contains(prefix, "function __webpack_require__")))
}

func isRollupBootstrapPrefix(prefix string) bool {
	return strings.Contains(prefix, "var commonjsGlobal = typeof globalThis") &&
		strings.Contains(prefix, "function getDefaultExportFromCjs") &&
		strings.Contains(prefix, "function getAugmentedNamespace") &&
		strings.Contains(prefix, "Object.defineProperty(a, '__esModule'")
}

func isESBuildBootstrapPrefix(prefix string) bool {
	return strings.Contains(prefix, "var __defProp = Object.defineProperty") &&
		strings.Contains(prefix, "var __getOwnPropNames = Object.getOwnPropertyNames") &&
		strings.Contains(prefix, "var __commonJS =") &&
		strings.Contains(prefix, "var __copyProps =") &&
		(strings.Contains(prefix, "var __toESM =") || strings.Contains(prefix, "var __toCommonJS ="))
}

func isParcelBootstrapPrefix(prefix string) bool {
	return strings.Contains(prefix, "$parcel$global") &&
		strings.Contains(prefix, "parcelRequire") &&
		strings.Contains(prefix, "newRequire.isParcelRequire = true") &&
		strings.Contains(prefix, "newRequire.Module = Module")
}

func isVendoredZendFrameworkFile(path string) bool {
	normalized := filepath.ToSlash(path)
	if !strings.Contains(normalized, "/library/Zend/") && !strings.Contains(normalized, "/Zend/") {
		return false
	}
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".php"
}

func isVendoredPluploadFile(path string) bool {
	if ext := strings.ToLower(filepath.Ext(path)); ext != ".js" && ext != ".mjs" && ext != ".cjs" {
		return false
	}
	normalizedPath := strings.ToLower(filepath.ToSlash(path))
	name := strings.ToLower(filepath.Base(path))
	if !strings.Contains(normalizedPath, "/plupload/js/") || !strings.HasPrefix(name, "plupload.") {
		return false
	}
	prefix, ok := javascriptFilePrefix(path, vendoredBrowserLibraryPrefixBytes)
	if !ok {
		return false
	}
	normalizedPrefix := strings.ToLower(prefix)
	return strings.Contains(normalizedPrefix, "window.plupload") &&
		strings.Contains(normalizedPrefix, "version")
}

func isVendoredAurigmaFile(path string) bool {
	if ext := strings.ToLower(filepath.Ext(path)); ext != ".js" && ext != ".mjs" && ext != ".cjs" {
		return false
	}
	name := strings.ToLower(filepath.Base(path))
	return strings.HasPrefix(name, "aurigma.htmluploader.") ||
		strings.HasPrefix(name, "aurigma.imageuploader")
}

func isVendoredPHPCASFile(path string) bool {
	if ext := strings.ToLower(filepath.Ext(path)); ext != ".php" {
		return false
	}
	normalized := strings.ToLower(filepath.ToSlash(path))
	return strings.Contains(normalized, "/library/cas/cas/")
}

func isVendoredMinifyFile(path string) bool {
	normalized := strings.ToLower(filepath.ToSlash(path))
	return strings.Contains(normalized, "/library/minify/min/")
}

func isVendoredFusionChartsFile(path string) bool {
	normalized := strings.ToLower(filepath.ToSlash(path))
	return strings.Contains(normalized, "/library/fusioncharts/") ||
		strings.Contains(normalized, "/library/powercharts/")
}

func isVendoredMapControlFile(path string) bool {
	if ext := strings.ToLower(filepath.Ext(path)); ext != ".js" && ext != ".mjs" && ext != ".cjs" {
		return false
	}
	if strings.ToLower(filepath.Base(path)) != "mapcontrol.js" {
		return false
	}
	prefix, ok := javascriptFilePrefix(path, vendoredBrowserLibraryPrefixBytes)
	if !ok {
		return false
	}
	normalized := strings.ToLower(prefix)
	return strings.Contains(normalized, "bing maps") &&
		strings.Contains(normalized, "virtual earth") &&
		strings.Contains(normalized, "mapcontrol.features")
}

func isVendoredBrowserLibraryFile(path string) bool {
	if ext := strings.ToLower(filepath.Ext(path)); ext != ".js" && ext != ".mjs" && ext != ".cjs" {
		return false
	}
	name := strings.ToLower(filepath.Base(path))
	switch {
	case name == "jquery.js" || name == "jquery-ui.js":
		return true
	case strings.HasPrefix(name, "jquery.") || strings.HasPrefix(name, "jquery-"):
		return true
	case strings.HasPrefix(name, "galleria") && strings.HasSuffix(name, ".js"):
		return true
	case strings.HasPrefix(name, "shadowbox") && strings.HasSuffix(name, ".js"):
		return true
	case strings.HasPrefix(name, "sizzle") && strings.HasSuffix(name, ".js"):
		return true
	case strings.HasPrefix(name, "swfobject") && strings.HasSuffix(name, ".js"):
		return true
	case strings.Contains("/"+filepath.ToSlash(path)+"/", "/jwplayer/"):
		return true
	default:
		return hasVendoredBrowserLibrarySignature(path)
	}
}

func hasVendoredBrowserLibrarySignature(path string) bool {
	prefix, ok := javascriptFilePrefix(path, vendoredBrowserLibraryPrefixBytes)
	if !ok {
		return false
	}
	normalized := strings.ToLower(prefix)
	name := strings.ToLower(filepath.Base(path))
	switch {
	case strings.Contains(normalized, "jquery foundation") &&
		strings.Contains(normalized, "jquery.org/license"):
		return true
	case strings.Contains(normalized, "bootstrap v") &&
		strings.Contains(normalized, "getbootstrap.com"):
		return true
	case strings.Contains(normalized, "fullcalendar v") &&
		strings.Contains(normalized, "fullcalendar.io"):
		return true
	case strings.Contains(normalized, "fotorama") &&
		strings.Contains(normalized, "fotorama.io/license"):
		return true
	case strings.Contains(normalized, "gmaps.js") &&
		strings.Contains(normalized, "hpneo.github.com/gmaps"):
		return true
	case strings.Contains(normalized, "masonry packaged") &&
		strings.Contains(normalized, "masonry.desandro.com"):
		return true
	case strings.Contains(normalized, "jwplayer.version") ||
		(strings.Contains(normalized, "d.html5.version") &&
			strings.Contains(normalized, "jwplayer")):
		return true
	case strings.Contains(normalized, "filepond") &&
		strings.Contains(normalized, "pqina.nl/filepond"):
		return true
	case strings.Contains(normalized, "prototype javascript framework") &&
		strings.Contains(normalized, "prototypejs.org"):
		return true
	case strings.Contains(normalized, "script.aculo.us") &&
		strings.Contains(normalized, "effects.js"):
		return true
	case strings.Contains(normalized, "reveal.js") &&
		strings.Contains(normalized, "lab.hakim.se/reveal-js"):
		return true
	case strings.Contains(normalized, "camera slideshow") &&
		(strings.Contains(normalized, "pixedelic.com") ||
			strings.Contains(normalized, "jquery slideshow")):
		return true
	case strings.Contains(name, "jssor") &&
		strings.Contains(normalized, "jssor") &&
		(strings.Contains(normalized, "$jssorslider$") ||
			strings.Contains(normalized, "/*! jssor") ||
			strings.Contains(normalized, "jssor slider")):
		return true
	case strings.Contains(normalized, "mootools") &&
		(strings.Contains(normalized, "my object oriented javascript tools") ||
			strings.Contains(normalized, "mootools core") ||
			strings.Contains(normalized, "mootools more")):
		return true
	case strings.Contains(normalized, "less - leaner css") &&
		strings.Contains(normalized, "lesscss"):
		return true
	case strings.Contains(normalized, "uglifyjs") &&
		strings.Contains(normalized, "javascript parser/compressor/beautifier"):
		return true
	case strings.Contains(normalized, "window.__sharethis__") &&
		strings.Contains(normalized, "sharethis"):
		return true
	default:
		return false
	}
}

func javascriptFilePrefix(path string, limit int) (string, bool) {
	if limit <= 0 {
		return "", false
	}
	file, err := os.Open(path) // #nosec G304 -- reads indexed repo file at a path derived from the scan target, not user-supplied input
	if err != nil {
		return "", false
	}
	defer func() { _ = file.Close() }()

	buf := make([]byte, limit)
	n, err := file.Read(buf)
	if err != nil && n == 0 {
		return "", false
	}
	return string(buf[:n]), true
}

func isVendoredFPDFFile(path string) bool {
	name := strings.ToLower(filepath.Base(path))
	return name == "fpdf.php"
}

func isVendoredPEARFile(path string) bool {
	if ext := strings.ToLower(filepath.Ext(path)); ext != ".php" {
		return false
	}
	normalized := strings.ToLower(filepath.ToSlash(path))
	return strings.Contains(normalized, "/pear/php/")
}
