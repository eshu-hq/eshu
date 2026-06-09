package archivepreflight

import (
	"archive/tar"
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

const (
	// FormatZIP identifies a ZIP archive.
	FormatZIP = "zip"
	// FormatTAR identifies an uncompressed tar archive.
	FormatTAR = "tar"
)

const (
	defaultMaxSourceBytes      = int64(100 << 20)
	defaultMaxExpandedBytes    = int64(256 << 20)
	defaultMaxEntries          = 10000
	defaultMaxCompressionRatio = 100
	ratioCheckMinSize          = uint64(1024)
)

// WarningClass is a stable, low-cardinality archive preflight failure class.
type WarningClass string

const (
	// WarningUnsupportedFormat marks archive formats outside this preflight.
	WarningUnsupportedFormat WarningClass = "unsupported_format"
	// WarningMalformedContainer marks archive parse failures.
	WarningMalformedContainer WarningClass = "malformed_container"
	// WarningResourceLimitExceeded marks source, entry-count, or expanded-byte limits.
	WarningResourceLimitExceeded WarningClass = "resource_limit_exceeded"
	// WarningCompressionRatioExceeded marks ZIP members over the compression-ratio cap.
	WarningCompressionRatioExceeded WarningClass = "compression_ratio_exceeded"
	// WarningTimeout marks caller cancellation or deadline during preflight.
	WarningTimeout WarningClass = "timeout"
	// WarningArchivePathEscape marks absolute, parent-traversing, or non-local member names.
	WarningArchivePathEscape WarningClass = "archive_path_escape"
	// WarningArchiveSymlinkSkipped marks archive symlink members.
	WarningArchiveSymlinkSkipped WarningClass = "archive_symlink_skipped"
	// WarningArchiveSpecialFileSkipped marks archive device, FIFO, or other special members.
	WarningArchiveSpecialFileSkipped WarningClass = "archive_special_file_skipped"
	// WarningArchiveNestedSkipped marks nested archive members.
	WarningArchiveNestedSkipped WarningClass = "archive_nested_skipped"
	// WarningCredentialFileSkipped marks credential-like archive members.
	WarningCredentialFileSkipped WarningClass = "credential_file_skipped"
)

// Options bounds archive preflight work.
type Options struct {
	MaxSourceBytes      int64
	MaxExpandedBytes    int64
	MaxEntries          int
	MaxCompressionRatio float64
}

// Warning records one bounded archive preflight failure class.
type Warning struct {
	Class WarningClass `json:"class"`
	Count int          `json:"count"`
}

// Result summarizes metadata-only archive preflight.
type Result struct {
	Format           string    `json:"format"`
	Safe             bool      `json:"safe"`
	Warnings         []Warning `json:"warnings,omitempty"`
	EntryCount       int       `json:"entry_count"`
	RegularFileCount int       `json:"regular_file_count"`
	DirectoryCount   int       `json:"directory_count"`
	SourceBytes      int64     `json:"source_bytes"`
	ExpandedBytes    int64     `json:"expanded_bytes"`
	NestedCount      int       `json:"nested_count"`
	CredentialCount  int       `json:"credential_count"`
	SymlinkCount     int       `json:"symlink_count"`
	SpecialFileCount int       `json:"special_file_count"`
}

type recorder struct {
	result               *Result
	seen                 map[WarningClass]int
	expandedBytesWarning bool
	entryCountWarning    bool
}

// Preflight classifies an archive package without extracting member content.
func Preflight(ctx context.Context, sourceName string, reader io.ReaderAt, size int64, options Options) (Result, error) {
	if reader == nil {
		return Result{}, fmt.Errorf("reader must not be nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	opts := normalizeOptions(options)
	result := Result{
		Format:      formatForSource(sourceName),
		Safe:        true,
		SourceBytes: size,
	}
	rec := recorder{result: &result, seen: map[WarningClass]int{}}

	if err := ctx.Err(); err != nil {
		rec.warn(WarningTimeout)
		return rec.finalize(), err
	}
	if result.Format == "" {
		rec.warn(WarningUnsupportedFormat)
		return rec.finalize(), nil
	}
	if size < 0 {
		rec.warn(WarningMalformedContainer)
		return rec.finalize(), nil
	}
	if size > opts.MaxSourceBytes {
		rec.warn(WarningResourceLimitExceeded)
		return rec.finalize(), nil
	}

	switch result.Format {
	case FormatZIP:
		return preflightZip(ctx, reader, size, opts, &rec), nil
	case FormatTAR:
		return preflightTar(ctx, reader, size, opts, &rec), nil
	default:
		rec.warn(WarningUnsupportedFormat)
		return rec.finalize(), nil
	}
}

func normalizeOptions(options Options) Options {
	if options.MaxSourceBytes <= 0 {
		options.MaxSourceBytes = defaultMaxSourceBytes
	}
	if options.MaxExpandedBytes <= 0 {
		options.MaxExpandedBytes = defaultMaxExpandedBytes
	}
	if options.MaxEntries <= 0 {
		options.MaxEntries = defaultMaxEntries
	}
	if options.MaxCompressionRatio <= 0 {
		options.MaxCompressionRatio = defaultMaxCompressionRatio
	}
	return options
}

func preflightZip(ctx context.Context, reader io.ReaderAt, size int64, options Options, rec *recorder) Result {
	zr, err := zip.NewReader(reader, size)
	if err != nil {
		if !errors.Is(err, zip.ErrInsecurePath) || zr == nil {
			rec.warn(WarningMalformedContainer)
			return rec.finalize()
		}
		rec.warn(WarningArchivePathEscape)
	}
	for _, file := range zr.File {
		if err := ctx.Err(); err != nil {
			rec.warn(WarningTimeout)
			return rec.finalize()
		}
		rec.observeEntry(options)
		name := file.Name
		if unsafeMemberName(name) {
			rec.warn(WarningArchivePathEscape)
		}
		if file.FileInfo().IsDir() {
			rec.result.DirectoryCount++
			continue
		}
		if rec.classifyZipMode(file.FileInfo().Mode()) {
			continue
		}
		rec.result.RegularFileCount++
		rec.observeExpandedBytes(int64(file.UncompressedSize64), options)
		if zipCompressionRatioExceeded(file, options.MaxCompressionRatio) {
			rec.warn(WarningCompressionRatioExceeded)
		}
		rec.classifyMemberName(name)
	}
	return rec.finalize()
}

func preflightTar(ctx context.Context, reader io.ReaderAt, size int64, options Options, rec *recorder) Result {
	tr := tar.NewReader(io.NewSectionReader(reader, 0, size))
	seenAny := false
	for {
		if err := ctx.Err(); err != nil {
			rec.warn(WarningTimeout)
			return rec.finalize()
		}
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			rec.warn(WarningMalformedContainer)
			return rec.finalize()
		}
		seenAny = true
		rec.observeEntry(options)
		rec.classifyTarHeader(header, options)
	}
	if !seenAny {
		rec.warn(WarningMalformedContainer)
	}
	return rec.finalize()
}

func (r *recorder) observeEntry(options Options) {
	r.result.EntryCount++
	if r.result.EntryCount > options.MaxEntries && !r.entryCountWarning {
		r.warn(WarningResourceLimitExceeded)
		r.entryCountWarning = true
	}
}

func (r *recorder) observeExpandedBytes(size int64, options Options) {
	if size < 0 {
		r.warn(WarningMalformedContainer)
		return
	}
	r.result.ExpandedBytes += size
	if r.result.ExpandedBytes > options.MaxExpandedBytes && !r.expandedBytesWarning {
		r.warn(WarningResourceLimitExceeded)
		r.expandedBytesWarning = true
	}
}

func (r *recorder) classifyZipMode(mode fs.FileMode) bool {
	if mode&fs.ModeSymlink != 0 {
		r.result.SymlinkCount++
		r.warn(WarningArchiveSymlinkSkipped)
		return true
	}
	if mode&(fs.ModeDevice|fs.ModeCharDevice|fs.ModeNamedPipe|fs.ModeSocket|fs.ModeIrregular) != 0 {
		r.result.SpecialFileCount++
		r.warn(WarningArchiveSpecialFileSkipped)
		return true
	}
	return false
}

func (r *recorder) classifyTarHeader(header *tar.Header, options Options) {
	if unsafeMemberName(header.Name) {
		r.warn(WarningArchivePathEscape)
	}
	switch header.Typeflag {
	case tar.TypeDir:
		r.result.DirectoryCount++
	case tar.TypeReg, 0:
		r.result.RegularFileCount++
		r.observeExpandedBytes(header.Size, options)
		r.classifyMemberName(header.Name)
	case tar.TypeSymlink, tar.TypeLink:
		r.result.SymlinkCount++
		r.warn(WarningArchiveSymlinkSkipped)
	case tar.TypeChar, tar.TypeBlock, tar.TypeFifo:
		r.result.SpecialFileCount++
		r.warn(WarningArchiveSpecialFileSkipped)
	default:
		r.result.SpecialFileCount++
		r.warn(WarningArchiveSpecialFileSkipped)
	}
}

func (r *recorder) classifyMemberName(name string) {
	if isNestedArchiveName(name) {
		r.result.NestedCount++
		r.warn(WarningArchiveNestedSkipped)
	}
	if isCredentialLikeName(name) {
		r.result.CredentialCount++
		r.warn(WarningCredentialFileSkipped)
	}
}

func (r *recorder) warn(class WarningClass) {
	if count, ok := r.seen[class]; ok {
		r.seen[class] = count + 1
		for i := range r.result.Warnings {
			if r.result.Warnings[i].Class == class {
				r.result.Warnings[i].Count++
				break
			}
		}
	} else {
		r.seen[class] = 1
		r.result.Warnings = append(r.result.Warnings, Warning{Class: class, Count: 1})
	}
	r.result.Safe = false
}

func (r *recorder) finalize() Result {
	if len(r.result.Warnings) > 0 {
		r.result.Safe = false
		sort.Slice(r.result.Warnings, func(left, right int) bool {
			return r.result.Warnings[left].Class < r.result.Warnings[right].Class
		})
	}
	return *r.result
}

func formatForSource(sourceName string) string {
	lower := strings.ToLower(sourceName)
	switch {
	case strings.HasSuffix(lower, ".zip"):
		return FormatZIP
	case strings.HasSuffix(lower, ".tar"):
		return FormatTAR
	default:
		return ""
	}
}

func zipCompressionRatioExceeded(file *zip.File, maxRatio float64) bool {
	if file.UncompressedSize64 < ratioCheckMinSize {
		return false
	}
	if file.CompressedSize64 == 0 {
		return file.UncompressedSize64 > 0
	}
	return float64(file.UncompressedSize64)/float64(file.CompressedSize64) > maxRatio
}

func unsafeMemberName(name string) bool {
	if name == "" || strings.ContainsRune(name, 0) || strings.Contains(name, "\\") {
		return true
	}
	trimmed := strings.TrimSuffix(name, "/")
	if trimmed == "" || strings.HasPrefix(trimmed, "/") || hasWindowsDrivePrefix(trimmed) {
		return true
	}
	for _, part := range strings.Split(trimmed, "/") {
		if part == "" || part == "." || part == ".." {
			return true
		}
	}
	cleaned := path.Clean(trimmed)
	return cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../")
}

func hasWindowsDrivePrefix(name string) bool {
	if len(name) < 2 || name[1] != ':' {
		return false
	}
	first := name[0]
	return (first >= 'A' && first <= 'Z') || (first >= 'a' && first <= 'z')
}

func isNestedArchiveName(name string) bool {
	lower := strings.ToLower(name)
	for _, suffix := range []string{".zip", ".tar", ".tar.gz", ".tgz", ".tar.bz2", ".tbz2", ".tar.xz", ".txz"} {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	return false
}

func isCredentialLikeName(name string) bool {
	lower := strings.ToLower(filepath.ToSlash(name))
	for _, segment := range strings.Split(lower, "/") {
		if credentialSegment(segment) {
			return true
		}
	}
	return false
}

func credentialSegment(segment string) bool {
	switch segment {
	case ".env", ".netrc", "credentials", "credential", "secrets", "secret",
		"passwd", "shadow", "id_rsa", "id_dsa", "id_ecdsa", "id_ed25519":
		return true
	}
	return strings.HasSuffix(segment, ".pem") ||
		strings.HasSuffix(segment, ".key") ||
		strings.HasSuffix(segment, ".p12") ||
		strings.HasSuffix(segment, ".pfx")
}
