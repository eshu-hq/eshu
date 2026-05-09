package terraformstate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ErrStateTooLarge is returned when a source exceeds the configured size ceiling.
var ErrStateTooLarge = errors.New("terraform state exceeds size ceiling")

// LocalSourceConfig configures an exact local state file source.
type LocalSourceConfig struct {
	Path     string
	MaxBytes int64
}

// LocalStateSource reads one exact operator-approved local state file.
type LocalStateSource struct {
	path     string
	maxBytes int64
}

// NewLocalStateSource validates and returns a local Terraform state source.
func NewLocalStateSource(config LocalSourceConfig) (*LocalStateSource, error) {
	path := strings.TrimSpace(config.Path)
	if path == "" {
		return nil, fmt.Errorf("local state path must not be blank")
	}
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("local state path must be absolute")
	}
	if _, err := validateRegularLocalStatePath(path); err != nil {
		return nil, err
	}

	maxBytes := config.MaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultStateSizeCeilingBytes
	}
	return &LocalStateSource{path: path, maxBytes: maxBytes}, nil
}

// Identity returns the durable source identity for this local state file.
func (s *LocalStateSource) Identity() StateKey {
	if s == nil {
		return StateKey{BackendKind: BackendLocal}
	}
	return StateKey{BackendKind: BackendLocal, Locator: s.path}
}

// Open opens the configured file if it is still within the source size ceiling.
func (s *LocalStateSource) Open(ctx context.Context) (io.ReadCloser, SourceMetadata, error) {
	if s == nil {
		return nil, SourceMetadata{}, fmt.Errorf("local state source is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, SourceMetadata{}, err
	}

	info, err := validateRegularLocalStatePath(s.path)
	if err != nil {
		return nil, SourceMetadata{}, err
	}
	if info.Size() > s.maxBytes {
		return nil, SourceMetadata{}, fmt.Errorf("%w: size=%d max=%d", ErrStateTooLarge, info.Size(), s.maxBytes)
	}

	file, err := os.Open(s.path)
	if err != nil {
		return nil, SourceMetadata{}, fmt.Errorf("open local state path: %w", err)
	}
	return newSizeEnforcingReadCloser(file, s.maxBytes), SourceMetadata{
		ObservedAt:   time.Now().UTC(),
		Size:         info.Size(),
		LastModified: info.ModTime().UTC(),
	}, nil
}

func validateRegularLocalStatePath(path string) (os.FileInfo, error) {
	linkInfo, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("stat local state path: %w", err)
	}
	if linkInfo.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("local state path must not be a symlink")
	}
	if !linkInfo.Mode().IsRegular() {
		return nil, fmt.Errorf("local state path must be a regular file")
	}
	return linkInfo, nil
}
