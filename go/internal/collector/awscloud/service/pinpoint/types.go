// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pinpoint

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// Client snapshots metadata-only Amazon Pinpoint application, segment, and
// channel-settings observations for one AWS claim. Implementations read
// control-plane metadata through the Pinpoint management APIs and never read
// endpoint records, addresses, message content, targeting criteria values, or
// channel credentials.
type Client interface {
	// Snapshot returns every Pinpoint application visible to the configured AWS
	// credentials, each carrying its segments and channel settings.
	Snapshot(ctx context.Context) (Snapshot, error)
}

// Snapshot captures Pinpoint application metadata plus non-fatal scan warnings.
type Snapshot struct {
	// Applications is the metadata-only set of Pinpoint applications, each
	// carrying its segments and channels.
	Applications []Application
	// Warnings carries non-fatal partial-scan observations such as sustained
	// throttling that omitted a metadata component.
	Warnings []awscloud.WarningObservation
}

// Application is the scanner-owned Pinpoint application (project) model. It
// carries control-plane metadata only.
type Application struct {
	// ID is the Pinpoint application id (the console Project ID).
	ID string
	// ARN is the Amazon Resource Name that uniquely identifies the application.
	ARN string
	// Name is the Pinpoint application display name.
	Name string
	// CreationTime is when the application was created, when AWS reports it.
	CreationTime time.Time
	// Tags carries the application resource tags.
	Tags map[string]string
	// Segments are the metadata-only segments that live under this application.
	Segments []Segment
	// Channels are the metadata-only channel settings for this application.
	Channels []Channel
}

// Segment is the scanner-owned Pinpoint segment model. It carries control-plane
// metadata only and intentionally excludes targeting dimensions, segment group
// criteria, the import S3 URL, the import external id, and any endpoint
// attribute value.
type Segment struct {
	// ID is the Pinpoint segment id.
	ID string
	// ARN is the Amazon Resource Name that uniquely identifies the segment.
	ARN string
	// Name is the Pinpoint segment name.
	Name string
	// ApplicationID is the id of the parent Pinpoint application.
	ApplicationID string
	// SegmentType is the segment type (DIMENSIONAL or IMPORT).
	SegmentType string
	// Version is the segment version AWS reports.
	Version int32
	// ImportedFromS3 reports whether the segment was created from an imported
	// endpoint file in Amazon S3. The import S3 URL itself is never recorded.
	ImportedFromS3 bool
	// ImportFormat is the import file format (CSV or JSON) when the segment was
	// imported. It is metadata about the import, not import content.
	ImportFormat string
	// ImportSize is the count of endpoints AWS reports as imported, an aggregate
	// metadata count and never the endpoint records themselves.
	ImportSize int32
	// CreationTime is when the segment was created.
	CreationTime time.Time
	// LastModifiedTime is when the segment was last modified.
	LastModifiedTime time.Time
	// Tags carries the segment resource tags.
	Tags map[string]string
}

// Channel is the scanner-owned Pinpoint channel-settings model for one channel
// type under an application. It carries the channel status and references only;
// it never carries channel credentials, API keys, certificates, tokens, or the
// email from-address.
type Channel struct {
	// ApplicationID is the id of the parent Pinpoint application.
	ApplicationID string
	// ChannelType is the channel type (for example EMAIL, SMS, APNS, GCM, VOICE).
	ChannelType string
	// Enabled reports whether the channel is enabled for the application.
	Enabled bool
	// Archived reports whether the channel is archived.
	Archived bool
	// Version is the current channel version AWS reports.
	Version int32
	// CreationTime is when the channel was enabled.
	CreationTime time.Time
	// LastModifiedTime is when the channel was last modified.
	LastModifiedTime time.Time
	// SESConfigurationSet is the SES configuration set name applied to email sent
	// through the channel. It is populated for the email channel only and is a
	// configuration set name, not an ARN.
	SESConfigurationSet string
	// SESIdentityARN is the ARN of the SES identity used to send email through
	// the email channel. It is populated for the email channel only. The
	// from-address (an email address) is never recorded.
	SESIdentityARN string
}
