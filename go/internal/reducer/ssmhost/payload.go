// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ssmhost

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu-sdk/factschema"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/cloud"
	"github.com/eshu-hq/eshu/go/internal/reducer/factdecode"
)

// Closed SSM inventory source vocabularies (eshu-hq/eshu#9556, accepted
// record #9302). The application kind admits exactly one source type:
// allowlisted AWS:Application rows. Custom inventory, file and network
// entries, registry contents, and arbitrary command output are excluded.
// Platform evidence carries one of two provenances: inventory alongside the
// application capture, or DescribeInstanceInformation with its own
// observation time and no invented capture time.
const (
	ssmInventoryTypeApplication = "AWS:Application"

	ssmPlatformSourceInventory                   = "ssm_inventory"
	ssmPlatformSourceDescribeInstanceInformation = "describe_instance_information"

	ssmEC2ResourceType = "AWS::EC2::Instance"

	ssmSchemaMajor = "1"
)

// Application is one allowlisted AWS:Application inventory row for one EC2
// managed node in one dated capture. The field spelling mirrors the #9556
// typed contract (sdk/go/factschema/aws/v1 SSMHostApplication) until the
// eshu-sdk release carrying these kinds lets this seam decode through
// schemadecode like the other AWS kinds. It is an observation, not a package
// finding: no vulnerability verdict, supply-chain impact, PURL, or image
// digest may be derived from it (#9209 owns correlation).
type Application struct {
	Partition     string
	AccountID     string
	Region        string
	InstanceID    string
	ResourceType  string
	SourceType    string
	SchemaVersion string
	CaptureTime   string
	Name          *string
	Version       *string
	Release       *string
	Epoch         *string
	PackageID     *string
	Architecture  *string
}

// Platform is the platform observation for one EC2 managed node with its
// provenance. The field spelling mirrors the #9556 typed contract
// (sdk/go/factschema/aws/v1 SSMHostPlatform) until the eshu-sdk release
// carrying these kinds lets this seam decode through schemadecode.
type Platform struct {
	Partition       string
	AccountID       string
	Region          string
	InstanceID      string
	ResourceType    string
	PlatformType    *string
	PlatformName    *string
	PlatformVersion *string
	SourceType      string
	CaptureTime     *string
	ObservationTime string
}

// DecodeApplication decodes one aws_ssm_host_application envelope through
// strict field reads. A missing or mistyped required field, a non-EC2
// resource type, or a non-Application source type is a self-classifying
// input_invalid failure the extractor quarantines per fact. An unsupported
// schema major is version skew, not a malformed fact: it carries the SDK
// sentinel so PartitionDecodeFailures fails the whole intent for durable
// triage.
func DecodeApplication(env facts.Envelope) (Application, error) {
	payload := env.Payload
	kind := cloud.SSMHostApplicationFactKind
	get := func(key string) (string, error) {
		value, err := requiredField(payload, key)
		if err != nil {
			return "", factdecode.NewFactDecodeError(kind, err)
		}
		return value, nil
	}
	var app Application
	var err error
	if app.Partition, err = get("partition"); err != nil {
		return Application{}, err
	}
	if app.AccountID, err = get("account_id"); err != nil {
		return Application{}, err
	}
	if app.Region, err = get("region"); err != nil {
		return Application{}, err
	}
	if app.InstanceID, err = get("instance_id"); err != nil {
		return Application{}, err
	}
	if app.ResourceType, err = get("resource_type"); err != nil {
		return Application{}, err
	}
	if app.ResourceType != ssmEC2ResourceType {
		return Application{}, factdecode.NewFactDecodeError(kind, &factschema.DecodeError{
			FactKind:       kind,
			Classification: factschema.ClassificationInputInvalid,
			Field:          "resource_type",
			Err:            fmt.Errorf("ssm host application resource type %q is not EC2", app.ResourceType),
		})
	}
	if app.SourceType, err = get("source_type"); err != nil {
		return Application{}, err
	}
	if app.SourceType != ssmInventoryTypeApplication {
		return Application{}, factdecode.NewFactDecodeError(kind, &factschema.DecodeError{
			FactKind:       kind,
			Classification: factschema.ClassificationInputInvalid,
			Field:          "source_type",
			Err:            fmt.Errorf("ssm host application source type %q is not allowlisted", app.SourceType),
		})
	}
	if app.SchemaVersion, err = get("schema_version"); err != nil {
		return Application{}, err
	}
	if major := schemaMajor(app.SchemaVersion); major != ssmSchemaMajor {
		return Application{}, factdecode.NewFactDecodeError(kind, &factschema.DecodeError{
			FactKind:       kind,
			Classification: factschema.ClassificationInputInvalid,
			Field:          "schema_version",
			Err:            fmt.Errorf("%w: ssm host application schema %q", factschema.ErrUnsupportedSchemaMajor, app.SchemaVersion),
		})
	}
	if app.CaptureTime, err = get("capture_time"); err != nil {
		return Application{}, err
	}
	optionals := []struct {
		key    string
		target **string
	}{
		{"name", &app.Name},
		{"version", &app.Version},
		{"release", &app.Release},
		{"epoch", &app.Epoch},
		{"package_id", &app.PackageID},
		{"architecture", &app.Architecture},
	}
	for _, optional := range optionals {
		value, err := optionalField(kind, payload, optional.key)
		if err != nil {
			return Application{}, err
		}
		*optional.target = value
	}
	return app, nil
}

// DecodePlatform decodes one aws_ssm_host_platform envelope through strict
// field reads with the same quarantine contract as DecodeApplication. The
// capture time stays optional: DescribeInstanceInformation evidence carries
// only its observation time, and inventing a capture time for it is a
// contract violation (surfaced as incoherent at assessment, never silently
// preferred).
func DecodePlatform(env facts.Envelope) (Platform, error) {
	payload := env.Payload
	kind := cloud.SSMHostPlatformFactKind
	get := func(key string) (string, error) {
		value, err := requiredField(payload, key)
		if err != nil {
			return "", factdecode.NewFactDecodeError(kind, err)
		}
		return value, nil
	}
	var platform Platform
	var err error
	if platform.Partition, err = get("partition"); err != nil {
		return Platform{}, err
	}
	if platform.AccountID, err = get("account_id"); err != nil {
		return Platform{}, err
	}
	if platform.Region, err = get("region"); err != nil {
		return Platform{}, err
	}
	if platform.InstanceID, err = get("instance_id"); err != nil {
		return Platform{}, err
	}
	if platform.ResourceType, err = get("resource_type"); err != nil {
		return Platform{}, err
	}
	if platform.ResourceType != ssmEC2ResourceType {
		return Platform{}, factdecode.NewFactDecodeError(kind, &factschema.DecodeError{
			FactKind:       kind,
			Classification: factschema.ClassificationInputInvalid,
			Field:          "resource_type",
			Err:            fmt.Errorf("ssm host platform resource type %q is not EC2", platform.ResourceType),
		})
	}
	if platform.SourceType, err = get("source_type"); err != nil {
		return Platform{}, err
	}
	switch platform.SourceType {
	case ssmPlatformSourceInventory, ssmPlatformSourceDescribeInstanceInformation:
	default:
		return Platform{}, factdecode.NewFactDecodeError(kind, &factschema.DecodeError{
			FactKind:       kind,
			Classification: factschema.ClassificationInputInvalid,
			Field:          "source_type",
			Err:            fmt.Errorf("ssm host platform source type %q is not a known provenance", platform.SourceType),
		})
	}
	if platform.ObservationTime, err = get("observation_time"); err != nil {
		return Platform{}, err
	}
	// capture_time stays optional but strict when present: DescribeInstance
	// Information evidence carries only its observation time, and a
	// wrong-typed capture time quarantines like any mistyped optional.
	captureTime, err := optionalField(kind, payload, "capture_time")
	if err != nil {
		return Platform{}, err
	}
	platform.CaptureTime = captureTime
	platformOptionals := []struct {
		key    string
		target **string
	}{
		{"platform_type", &platform.PlatformType},
		{"platform_name", &platform.PlatformName},
		{"platform_version", &platform.PlatformVersion},
	}
	for _, optional := range platformOptionals {
		value, err := optionalField(kind, payload, optional.key)
		if err != nil {
			return Platform{}, err
		}
		*optional.target = value
	}
	return platform, nil
}

// ApplicationStableKey returns the stable key for one application
// observation: host tuple plus capture time plus the canonical row hash.
// The encoding mirrors SSMHostApplicationStableKey in
// sdk/go/factschema/aws/v1 (eshu-hq/eshu#9556) field for field so collector,
// reducer, and read paths deduplicate identical rows in a capture while a new
// capture re-observes.
func ApplicationStableKey(app Application) string {
	return strings.Join([]string{
		cloud.SSMHostApplicationFactKind,
		app.Partition,
		app.AccountID,
		app.Region,
		app.InstanceID,
		app.CaptureTime,
		CanonicalApplicationRowHash(app),
	}, "/")
}

// PlatformUID returns the graph uid for one host and platform provenance:
// host tuple plus source type, mirroring SSMHostPlatformStableKey in
// sdk/go/factschema/aws/v1 (eshu-hq/eshu#9556). The uid carries no time, so a
// complete newer generation updates the platform observation on this key.
func PlatformUID(host cloud.SSMHostTuple, source string) string {
	return strings.Join([]string{
		cloud.SSMHostPlatformFactKind,
		host.Partition,
		host.AccountID,
		host.Region,
		host.InstanceID,
		source,
	}, "/")
}

// PlatformStableKey returns the stable key for one platform observation:
// host tuple plus source type, mirroring SSMHostPlatformStableKey in
// sdk/go/factschema/aws/v1 (eshu-hq/eshu#9556). A complete newer generation
// updates the platform observation on this key.
func PlatformStableKey(platform Platform) string {
	return strings.Join([]string{
		cloud.SSMHostPlatformFactKind,
		platform.Partition,
		platform.AccountID,
		platform.Region,
		platform.InstanceID,
		platform.SourceType,
	}, "/")
}

// canonicalApplicationRow is the fixed-order canonical encoding of exactly
// the six allowlisted row fields, mirroring ssmHostApplicationCanonicalRow in
// sdk/go/factschema/aws/v1 (eshu-hq/eshu#9556). Nil decodes to "" so absent
// and present-but-empty hash identically.
type canonicalApplicationRow struct {
	Name         string `json:"name"`
	Version      string `json:"version"`
	Release      string `json:"release"`
	Epoch        string `json:"epoch"`
	PackageID    string `json:"package_id"`
	Architecture string `json:"architecture"`
}

// CanonicalApplicationRowHash returns the hex SHA-256 of the canonical
// allowlisted row. Hashing the canonical selected row is for identity, not a
// claim that the row is an OS package or that its contents are secret.
func CanonicalApplicationRowHash(app Application) string {
	raw, err := json.Marshal(canonicalApplicationRow{
		Name:         deref(app.Name),
		Version:      deref(app.Version),
		Release:      deref(app.Release),
		Epoch:        deref(app.Epoch),
		PackageID:    deref(app.PackageID),
		Architecture: deref(app.Architecture),
	})
	if err != nil {
		// A six-string struct always marshals; a failure here is a
		// programming error, not a runtime condition.
		panic(fmt.Sprintf("ssmhost: marshal canonical application row: %v", err))
	}
	sum := sha256.Sum256(raw)
	return fmt.Sprintf("%x", sum[:])
}

// HostTuple returns the EC2 managed-node observation boundary for one
// application row.
func (a Application) HostTuple() cloud.SSMHostTuple {
	return cloud.SSMHostTuple{
		Partition:  a.Partition,
		AccountID:  a.AccountID,
		Region:     a.Region,
		InstanceID: a.InstanceID,
	}
}

// HostTuple returns the EC2 managed-node observation boundary for one
// platform observation.
func (p Platform) HostTuple() cloud.SSMHostTuple {
	return cloud.SSMHostTuple{
		Partition:  p.Partition,
		AccountID:  p.AccountID,
		Region:     p.Region,
		InstanceID: p.InstanceID,
	}
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func requiredField(payload map[string]any, key string) (string, error) {
	raw, ok := payload[key]
	if !ok || raw == nil {
		return "", &factschema.DecodeError{
			Classification: factschema.ClassificationInputInvalid,
			Field:          key,
			Err:            fmt.Errorf("ssm host payload is missing required field %q", key),
		}
	}
	text, ok := raw.(string)
	if !ok {
		return "", &factschema.DecodeError{
			Classification: factschema.ClassificationInputInvalid,
			Field:          key,
			Err:            fmt.Errorf("ssm host payload field %q is not a string", key),
		}
	}
	if strings.TrimSpace(text) == "" {
		return "", &factschema.DecodeError{
			Classification: factschema.ClassificationInputInvalid,
			Field:          key,
			Err:            fmt.Errorf("ssm host payload field %q is blank", key),
		}
	}
	return text, nil
}

// optionalField reads one optional string field. Absent and null stay
// absent (nil decodes to "" downstream, so absent and present-but-empty hash
// identically). A present-but-non-string value is an input_invalid decode
// failure the extractor quarantines per fact: it must never silently
// normalize to absent and hash like an empty field.
func optionalField(kind string, payload map[string]any, key string) (*string, error) {
	raw, ok := payload[key]
	if !ok || raw == nil {
		return nil, nil
	}
	text, ok := raw.(string)
	if !ok {
		return nil, factdecode.NewFactDecodeError(kind, &factschema.DecodeError{
			FactKind:       kind,
			Classification: factschema.ClassificationInputInvalid,
			Field:          key,
			Err:            fmt.Errorf("ssm host payload field %q is not a string", key),
		})
	}
	return &text, nil
}

func schemaMajor(version string) string {
	if i := strings.Index(version, "."); i >= 0 {
		return version[:i]
	}
	return version
}
