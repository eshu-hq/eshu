// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

const (
	envRealTool = "ESHU_TAGGED_VET_REAL_TOOL"
	envCacheDir = "ESHU_TAGGED_VET_CACHE_DIR"
	envGoCache  = "ESHU_TAGGED_VET_GOCACHE"

	cacheKeyVersion = "v1"
)

// vetConfig mirrors golang.org/x/tools/go/analysis/unitchecker.Config using
// only standard library types.
type vetConfig struct {
	ID                        string            `json:"ID"`
	Compiler                  string            `json:"Compiler"`
	ImportPath                string            `json:"ImportPath"`
	GoVersion                 string            `json:"GoVersion"`
	GoFiles                   []string          `json:"GoFiles"`
	NonGoFiles                []string          `json:"NonGoFiles"`
	IgnoredFiles              []string          `json:"IgnoredFiles"`
	ModulePath                string            `json:"ModulePath"`
	ModuleVersion             string            `json:"ModuleVersion"`
	Module                    json.RawMessage   `json:"Module"`
	ImportMap                 map[string]string `json:"ImportMap"`
	PackageFile               map[string]string `json:"PackageFile"`
	Standard                  map[string]bool   `json:"Standard"`
	PackageVetx               map[string]string `json:"PackageVetx"`
	VetxOnly                  bool              `json:"VetxOnly"`
	VetxOutput                string            `json:"VetxOutput"`
	Stdout                    string            `json:"Stdout"`
	FixArchive                string            `json:"FixArchive"`
	SucceedOnTypecheckFailure bool              `json:"SucceedOnTypecheckFailure"`
}

func main() {
	os.Exit(run(os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
}

func run(args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	realVet := getenv(envRealTool)
	if realVet == "" {
		_, _ = fmt.Fprintf(stderr, "tagged-vet-cache: %s is not set\n", envRealTool)
		return 2
	}

	cacheDir := getenv(envCacheDir)
	if cacheDir == "" || len(args) == 0 || !strings.HasSuffix(args[len(args)-1], ".cfg") {
		return execReal(realVet, args, stdout, stderr)
	}

	cfgPath := args[len(args)-1]
	raw, err := os.ReadFile(cfgPath) // #nosec G304 -- cfgPath is the .cfg path supplied by go vet.
	if err != nil {
		return execReal(realVet, args, stdout, stderr)
	}

	var cfg vetConfig
	if err := json.Unmarshal(raw, &cfg); err != nil || !isCacheable(&cfg) {
		return execReal(realVet, args, stdout, stderr)
	}

	goCache := resolveGoCache(getenv)
	key, ok := computeCacheKey(realVet, args[:len(args)-1], &cfg, goCache, getenv)
	if !ok {
		return execReal(realVet, args, stdout, stderr)
	}

	cachedPath := filepath.Join(cacheDir, key+".vetx")
	if copyFile(cachedPath, cfg.VetxOutput) == nil {
		if cfg.Stdout == "" || os.WriteFile(cfg.Stdout, nil, 0o600) == nil {
			return 0
		}
		_ = os.Remove(cfg.VetxOutput)
	}

	code := execReal(realVet, args, stdout, stderr)
	if code == 0 {
		// Best-effort atomic publish; vet already succeeded.
		_ = storeAtomic(cfg.VetxOutput, cachedPath)
	}
	return code
}

func isCacheable(cfg *vetConfig) bool {
	return cfg.VetxOnly && cfg.VetxOutput != "" && cfg.FixArchive == ""
}

func resolveGoCache(getenv func(string) string) string {
	dir := getenv(envGoCache)
	if dir == "" {
		dir = getenv("GOCACHE")
	}
	if dir == "" || dir == "off" || !filepath.IsAbs(dir) {
		return ""
	}
	return filepath.Clean(dir) + string(filepath.Separator)
}

func execReal(realVet string, args []string, stdout, stderr io.Writer) int {
	cmd := exec.Command(realVet, args...) // #nosec G204 -- realVet is the operator-configured go tool vet binary.
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode()
		}
		_, _ = fmt.Fprintf(stderr, "tagged-vet-cache: run %s: %v\n", realVet, err)
		return 1
	}
	return 0
}

type keyHasher struct {
	h        hash.Hash
	goCache  string
	workRoot string
}

func computeCacheKey(realVet string, flagArgs []string, cfg *vetConfig, goCache string, getenv func(string) string) (string, bool) {
	vetInfo, err := os.Stat(realVet)
	if err != nil {
		return "", false
	}

	kh := &keyHasher{
		h:        sha256.New(),
		goCache:  goCache,
		workRoot: detectWorkRoot(cfg.VetxOutput),
	}

	kh.writeStr("ver", cacheKeyVersion)
	kh.writeStr("vet", fmt.Sprintf("%s=%d:%d", filepath.Clean(realVet), vetInfo.Size(), vetInfo.ModTime().UnixNano()))
	kh.writeStr("goos", effectiveEnvOrDefault(getenv, "GOOS", runtime.GOOS))
	kh.writeStr("goarch", effectiveEnvOrDefault(getenv, "GOARCH", runtime.GOARCH))
	for _, flagArg := range filterCacheKeyFlags(flagArgs) {
		kh.writeStr("flag", flagArg)
	}

	kh.writeStr("id", cfg.ID)
	kh.writeStr("compiler", cfg.Compiler)
	kh.writeStr("import", cfg.ImportPath)
	kh.writeStr("goversion", cfg.GoVersion)
	kh.writeStr("modpath", cfg.ModulePath)
	kh.writeStr("modver", cfg.ModuleVersion)
	kh.writeStr("module", strings.TrimSpace(string(cfg.Module)))
	kh.writeStr("succeed_typecheck_fail", strconv.FormatBool(cfg.SucceedOnTypecheckFailure))

	if !kh.writeSourceFiles("go", cfg.GoFiles) || !kh.writeSourceFiles("nongo", cfg.NonGoFiles) {
		return "", false
	}
	for _, p := range cfg.IgnoredFiles {
		kh.writeStr("ignored", p)
	}
	kh.writeStringMap("imap", cfg.ImportMap)
	kh.writeBoolMap("std", cfg.Standard)
	if !kh.writeArtifactMap("pfile", cfg.PackageFile) || !kh.writeArtifactMap("pvetx", cfg.PackageVetx) {
		return "", false
	}

	return hex.EncodeToString(kh.h.Sum(nil)), true
}

func effectiveEnvOrDefault(getenv func(string) string, key, fallback string) string {
	if v := getenv(key); v != "" {
		return v
	}
	return fallback
}

// filterCacheKeyFlags removes -tags/--tags arguments from vet CLI flags before
// hashing. cmd/go forwards -tags to cmd/vet even though unitchecker's actual
// compiled file selection is already captured by GoFiles/NonGoFiles/IgnoredFiles
// and PackageFile/PackageVetx.
func filterCacheKeyFlags(flagArgs []string) []string {
	out := make([]string, 0, len(flagArgs))
	for i := 0; i < len(flagArgs); i++ {
		arg := flagArgs[i]
		if arg == "-tags" || arg == "--tags" {
			i++
			continue
		}
		if strings.HasPrefix(arg, "-tags=") || strings.HasPrefix(arg, "--tags=") {
			continue
		}
		out = append(out, arg)
	}
	return out
}

// detectWorkRoot derives the ephemeral $WORK directory root from VetxOutput.
// cmd/go writes VetxOutput as "$WORK/bNNN/vet.out"; flat test layouts use
// "<dir>/vet.out".
func detectWorkRoot(vetxOutput string) string {
	dir := filepath.Dir(filepath.Clean(vetxOutput))
	if isBuildWorkSubdir(filepath.Base(dir)) {
		dir = filepath.Dir(dir)
	}
	if dir == "" || dir == "." || dir == string(filepath.Separator) {
		return ""
	}
	return filepath.Clean(dir) + string(filepath.Separator)
}

func isBuildWorkSubdir(name string) bool {
	if len(name) < 2 || name[0] != 'b' {
		return false
	}
	for i := 1; i < len(name); i++ {
		if name[i] < '0' || name[i] > '9' {
			return false
		}
	}
	return true
}

func (k *keyHasher) writeStr(tag, s string) {
	_, _ = fmt.Fprintf(k.h, "%s:%d:%s\n", tag, len(s), s)
}

func (k *keyHasher) writeSourceFiles(tag string, files []string) bool {
	for _, p := range files {
		clean := filepath.Clean(p)
		if k.goCache != "" && strings.HasPrefix(clean, k.goCache) {
			k.writeStr(tag, "gocache:"+clean[len(k.goCache):])
			continue
		}
		if k.workRoot != "" && strings.HasPrefix(clean, k.workRoot) {
			digest, size, ok := hashFileContent(clean)
			if !ok {
				return false
			}
			k.writeStr(tag, fmt.Sprintf("work:%s:%s:%d", filepath.Base(clean), digest, size))
			continue
		}
		info, err := os.Stat(clean)
		if err != nil {
			return false
		}
		k.writeStr(tag, fmt.Sprintf("src:%s=%d:%d", clean, info.Size(), info.ModTime().UnixNano()))
	}
	return true
}

func (k *keyHasher) writeStringMap(tag string, m map[string]string) {
	keys := sortedKeys(m)
	for _, key := range keys {
		k.writeStr(tag+":"+key, m[key])
	}
}

func (k *keyHasher) writeBoolMap(tag string, m map[string]bool) {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		k.writeStr(tag+":"+key, strconv.FormatBool(m[key]))
	}
}

func (k *keyHasher) writeArtifactMap(tag string, m map[string]string) bool {
	keys := sortedKeys(m)
	for _, key := range keys {
		clean := filepath.Clean(m[key])
		if k.goCache != "" && strings.HasPrefix(clean, k.goCache) {
			k.writeStr(tag+":"+key, "gocache:"+clean[len(k.goCache):])
			continue
		}
		digest, size, ok := hashFileContent(clean)
		if !ok {
			return false
		}
		k.writeStr(tag+":"+key, fmt.Sprintf("hash:%s:%d", digest, size))
	}
	return true
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func hashFileContent(path string) (string, int64, bool) {
	f, err := os.Open(path) // #nosec G304 -- path comes from unitchecker.Config.
	if err != nil {
		return "", 0, false
	}
	defer func() { _ = f.Close() }()

	fh := sha256.New()
	n, err := io.Copy(fh, f)
	if err != nil {
		return "", 0, false
	}
	return hex.EncodeToString(fh.Sum(nil)), n, true
}

func copyFile(src, dst string) error {
	in, err := os.Open(src) // #nosec G304 -- src is a content-addressed file in the ephemeral cache dir.
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.Create(dst) // #nosec G304 -- dst is cfg.VetxOutput from go vet.
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(dst)
		return err
	}
	return nil
}

func storeAtomic(src, dst string) error {
	dir := filepath.Dir(dst)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "vetx-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()

	in, err := os.Open(src) // #nosec G304 -- src is cfg.VetxOutput written by real vet.
	if err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	_, copyErr := io.Copy(tmp, in)
	_ = in.Close()
	closeErr := tmp.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(tmpName)
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	}
	if err := os.Rename(tmpName, dst); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}
