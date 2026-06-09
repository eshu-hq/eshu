package googleworkspace

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Client is the mocked Google Workspace evidence boundary used by this package.
type Client interface {
	ListFiles(context.Context, Allowlist) ([]File, error)
	PermissionSummary(context.Context, string) (PermissionSummary, error)
	Export(context.Context, string, string) (Export, error)
}

// Allowlist bounds which Google Workspace files can be inspected.
type Allowlist struct {
	FileIDs          []string
	FolderIDs        []string
	SharedDriveIDs   []string
	SharedDriveQuery string
	AllowAllDrive    bool
}

// Request describes one mocked Google Workspace collection attempt.
type Request struct {
	ScopeID           string
	GenerationID      string
	ObservedAt        time.Time
	SourceName        string
	Allowlist         Allowlist
	Client            Client
	MaxExportBytes    int
	ExpectedRevisions map[string]string
}

// Result contains source-neutral documentation facts for one collection attempt.
type Result struct {
	Envelopes []facts.Envelope
}

// FileKind is the bounded Workspace file family supported by this package.
type FileKind string

const (
	// FileKindDocument identifies a Google Docs source.
	FileKindDocument FileKind = "document"
	// FileKindSpreadsheet identifies a Google Sheets source.
	FileKindSpreadsheet FileKind = "spreadsheet"
	// FileKindPresentation identifies a Google Slides source.
	FileKindPresentation FileKind = "presentation"
)

// File is source metadata returned by the mocked Drive boundary.
type File struct {
	ID         string
	Kind       FileKind
	Name       string
	RevisionID string
	WebURL     string
	Deleted    bool
	Trashed    bool
	Hidden     bool
	ModifiedAt time.Time
}

// PermissionSummary is the source ACL shape accepted from the mocked client.
type PermissionSummary struct {
	Visibility    string
	ReaderGroups  []string
	WriterGroups  []string
	ReaderUsers   []string
	WriterUsers   []string
	HasInherited  bool
	IsPartial     bool
	PartialReason string
}

// Export is a bounded provider export plus already-normalized document evidence.
type Export struct {
	Bytes    []byte
	Sections []Section
	Links    []Link
}

// Section represents one extracted export section.
type Section struct {
	ID            string
	Heading       string
	Content       string
	ContentFormat string
	Hidden        bool
	Metadata      map[string]string
}

// Link represents one export-observed link.
type Link struct {
	ID        string
	SectionID string
	TargetURI string
	Anchor    string
}

// FailureClass is a bounded, low-cardinality Google Workspace failure class.
type FailureClass string

func (f FailureClass) Error() string { return string(f) }

const (
	FailureAllowlistEmpty            FailureClass = "allowlist_empty"
	FailureAllowlistUnsupportedScope FailureClass = "allowlist_unsupported_scope"
	FailureAuthMissing               FailureClass = "auth_missing"
	FailurePermissionDenied          FailureClass = "permission_denied"
	FailureACLPartial                FailureClass = "acl_partial"
	FailureSourceDeleted             FailureClass = "source_deleted"
	FailureSourceTrashed             FailureClass = "source_trashed"
	FailureSourceRevisionStale       FailureClass = "source_revision_stale"
	FailureProviderRateLimited       FailureClass = "provider_rate_limited"
	FailureProviderQuotaExceeded     FailureClass = "provider_quota_exceeded"
	FailureDownloadNotAllowed        FailureClass = "download_not_allowed"
	FailureResourceLimitExceeded     FailureClass = "resource_limit_exceeded"
	FailureExportFormatUnsupported   FailureClass = "export_format_unsupported"
)
