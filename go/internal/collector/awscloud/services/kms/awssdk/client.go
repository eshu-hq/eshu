package awssdk

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awskms "github.com/aws/aws-sdk-go-v2/service/kms"
	kmstypes "github.com/aws/aws-sdk-go-v2/service/kms/types"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	kmsservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/kms"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the bounded AWS SDK surface this adapter consumes. Every
// method is List- or Describe-class. No cryptographic operation
// (Encrypt/Decrypt/Sign/Verify/Mac/GenerateDataKey/ReEncrypt/DeriveSharedSecret)
// and no lifecycle mutation (CreateKey/ScheduleKeyDeletion/PutKeyPolicy/
// CreateGrant/RevokeGrant/RetireGrant/ReplicateKey/ImportKeyMaterial/
// DeleteImportedKeyMaterial/EnableKey/DisableKey/EnableKeyRotation/
// DisableKeyRotation/UpdateKeyDescription/CreateAlias/UpdateAlias/
// DeleteAlias/TagResource/UntagResource) is reachable from this interface.
// The package's test asserts there is no method whose name matches a
// forbidden operation.
type apiClient interface {
	ListKeys(context.Context, *awskms.ListKeysInput, ...func(*awskms.Options)) (*awskms.ListKeysOutput, error)
	DescribeKey(context.Context, *awskms.DescribeKeyInput, ...func(*awskms.Options)) (*awskms.DescribeKeyOutput, error)
	ListAliases(context.Context, *awskms.ListAliasesInput, ...func(*awskms.Options)) (*awskms.ListAliasesOutput, error)
	ListGrants(context.Context, *awskms.ListGrantsInput, ...func(*awskms.Options)) (*awskms.ListGrantsOutput, error)
	ListKeyPolicies(context.Context, *awskms.ListKeyPoliciesInput, ...func(*awskms.Options)) (*awskms.ListKeyPoliciesOutput, error)
	GetKeyRotationStatus(context.Context, *awskms.GetKeyRotationStatusInput, ...func(*awskms.Options)) (*awskms.GetKeyRotationStatusOutput, error)
	ListResourceTags(context.Context, *awskms.ListResourceTagsInput, ...func(*awskms.Options)) (*awskms.ListResourceTagsOutput, error)
}

// Client adapts AWS SDK KMS control-plane calls into metadata-only scanner
// records.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a KMS SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awskms.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListKeys returns metadata-only KMS key snapshots visible to the
// configured AWS credentials. It paginates ListKeys, then for each key id
// pulls DescribeKey, ListKeyPolicies, GetKeyRotationStatus (when supported),
// ListGrants, and ListResourceTags. ListAliases is paginated once and
// indexed by target key id so callers do not need a per-key alias call.
func (c *Client) ListKeys(ctx context.Context) ([]kmsservice.Key, error) {
	ids, err := c.listKeyIDs(ctx)
	if err != nil {
		return nil, err
	}
	aliasesByKey, err := c.listAllAliasesByKey(ctx)
	if err != nil {
		return nil, err
	}
	keys := make([]kmsservice.Key, 0, len(ids))
	for _, id := range ids {
		key, err := c.keyMetadata(ctx, id, aliasesByKey[id])
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, nil
}

func (c *Client) listKeyIDs(ctx context.Context) ([]string, error) {
	var ids []string
	var marker *string
	for {
		var page *awskms.ListKeysOutput
		err := c.recordAPICall(ctx, "ListKeys", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListKeys(callCtx, &awskms.ListKeysInput{
				Marker: marker,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return ids, nil
		}
		for _, entry := range page.Keys {
			if trimmed := strings.TrimSpace(aws.ToString(entry.KeyId)); trimmed != "" {
				ids = append(ids, trimmed)
			}
		}
		marker = page.NextMarker
		if !page.Truncated || aws.ToString(marker) == "" {
			return ids, nil
		}
	}
}

func (c *Client) listAllAliasesByKey(ctx context.Context) (map[string][]kmsservice.Alias, error) {
	aliasesByKey := map[string][]kmsservice.Alias{}
	var marker *string
	for {
		var page *awskms.ListAliasesOutput
		err := c.recordAPICall(ctx, "ListAliases", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListAliases(callCtx, &awskms.ListAliasesInput{
				Marker: marker,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return aliasesByKey, nil
		}
		for _, entry := range page.Aliases {
			targetID := strings.TrimSpace(aws.ToString(entry.TargetKeyId))
			if targetID == "" {
				continue
			}
			aliasesByKey[targetID] = append(aliasesByKey[targetID], kmsservice.Alias{
				Name:        strings.TrimSpace(aws.ToString(entry.AliasName)),
				ARN:         strings.TrimSpace(aws.ToString(entry.AliasArn)),
				TargetKeyID: targetID,
				LastUpdated: formatTime(entry.LastUpdatedDate),
			})
		}
		marker = page.NextMarker
		if !page.Truncated || aws.ToString(marker) == "" {
			return aliasesByKey, nil
		}
	}
}

func (c *Client) keyMetadata(ctx context.Context, keyID string, aliases []kmsservice.Alias) (kmsservice.Key, error) {
	metadata, err := c.describeKey(ctx, keyID)
	if err != nil {
		return kmsservice.Key{}, err
	}
	tags, err := c.listResourceTags(ctx, keyID)
	if err != nil {
		return kmsservice.Key{}, err
	}
	policyNames, err := c.listKeyPolicies(ctx, keyID)
	if err != nil {
		return kmsservice.Key{}, err
	}
	rotation := c.keyRotationStatus(ctx, keyID, metadata)
	grants, err := c.listGrants(ctx, keyID)
	if err != nil {
		return kmsservice.Key{}, err
	}
	return mapKey(keyID, metadata, aliases, grants, tags, policyNames, rotation), nil
}

func (c *Client) describeKey(ctx context.Context, keyID string) (*kmstypes.KeyMetadata, error) {
	var output *awskms.DescribeKeyOutput
	err := c.recordAPICall(ctx, "DescribeKey", func(callCtx context.Context) error {
		var err error
		output, err = c.client.DescribeKey(callCtx, &awskms.DescribeKeyInput{
			KeyId: aws.String(keyID),
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	if output == nil || output.KeyMetadata == nil {
		return &kmstypes.KeyMetadata{KeyId: aws.String(keyID)}, nil
	}
	return output.KeyMetadata, nil
}

func (c *Client) listKeyPolicies(ctx context.Context, keyID string) ([]string, error) {
	var names []string
	var marker *string
	for {
		var page *awskms.ListKeyPoliciesOutput
		err := c.recordAPICall(ctx, "ListKeyPolicies", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListKeyPolicies(callCtx, &awskms.ListKeyPoliciesInput{
				KeyId:  aws.String(keyID),
				Marker: marker,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return names, nil
		}
		for _, name := range page.PolicyNames {
			if trimmed := strings.TrimSpace(name); trimmed != "" {
				names = append(names, trimmed)
			}
		}
		marker = page.NextMarker
		if aws.ToString(marker) == "" {
			return names, nil
		}
	}
}

// keyRotationStatus probes GetKeyRotationStatus for the keys that support it.
// AWS responds with UnsupportedOperationException for asymmetric, HMAC, and
// AWS-managed keys. Treat that as "rotation status unknown" so the scanner
// can omit the rotation_enabled attribute rather than report a false answer.
func (c *Client) keyRotationStatus(ctx context.Context, keyID string, metadata *kmstypes.KeyMetadata) rotationStatus {
	if !rotationCheckSupported(metadata) {
		return rotationStatus{known: false}
	}
	var output *awskms.GetKeyRotationStatusOutput
	err := c.recordAPICall(ctx, "GetKeyRotationStatus", func(callCtx context.Context) error {
		var err error
		output, err = c.client.GetKeyRotationStatus(callCtx, &awskms.GetKeyRotationStatusInput{
			KeyId: aws.String(keyID),
		})
		return err
	})
	if err != nil {
		if isUnsupportedOperation(err) || isAccessDeniedError(err) {
			return rotationStatus{known: false}
		}
		return rotationStatus{known: false, err: err}
	}
	if output == nil {
		return rotationStatus{known: false}
	}
	return rotationStatus{known: true, enabled: output.KeyRotationEnabled}
}

func (c *Client) listGrants(ctx context.Context, keyID string) ([]kmsservice.Grant, error) {
	var grants []kmsservice.Grant
	var marker *string
	for {
		var page *awskms.ListGrantsOutput
		err := c.recordAPICall(ctx, "ListGrants", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListGrants(callCtx, &awskms.ListGrantsInput{
				KeyId:  aws.String(keyID),
				Marker: marker,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return grants, nil
		}
		for _, entry := range page.Grants {
			grants = append(grants, mapGrant(entry))
		}
		marker = page.NextMarker
		if !page.Truncated || aws.ToString(marker) == "" {
			return grants, nil
		}
	}
}

func (c *Client) listResourceTags(ctx context.Context, keyID string) (map[string]string, error) {
	tags := map[string]string{}
	var marker *string
	for {
		var page *awskms.ListResourceTagsOutput
		err := c.recordAPICall(ctx, "ListResourceTags", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListResourceTags(callCtx, &awskms.ListResourceTagsInput{
				KeyId:  aws.String(keyID),
				Marker: marker,
			})
			return err
		})
		if err != nil {
			if isAccessDeniedError(err) {
				return nil, nil
			}
			return nil, err
		}
		if page == nil {
			return tags, nil
		}
		for _, tag := range page.Tags {
			key := strings.TrimSpace(aws.ToString(tag.TagKey))
			if key == "" {
				continue
			}
			tags[key] = aws.ToString(tag.TagValue)
		}
		marker = page.NextMarker
		if !page.Truncated || aws.ToString(marker) == "" {
			if len(tags) == 0 {
				return nil, nil
			}
			return tags, nil
		}
	}
}

var _ kmsservice.Client = (*Client)(nil)

var _ apiClient = (*awskms.Client)(nil)
