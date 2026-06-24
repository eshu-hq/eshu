// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsidentitystore "github.com/aws/aws-sdk-go-v2/service/identitystore"

	ssoadminservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/ssoadmin"
)

// principalSet collects the unique (type, id) principals referenced by account
// assignments so the adapter resolves each identity store record at most once.
type principalSet struct {
	types map[string]string
}

func newPrincipalSet() *principalSet {
	return &principalSet{types: map[string]string{}}
}

func (p *principalSet) add(principalType, principalID string) {
	id := strings.TrimSpace(principalID)
	if id == "" {
		return
	}
	if _, ok := p.types[id]; ok {
		return
	}
	p.types[id] = strings.TrimSpace(principalType)
}

// resolvePrincipals resolves each unique assignment principal to a display
// name. Resolution stops at the display name; no membership, address, email,
// phone, or structured identity attribute is read.
func (c *Client) resolvePrincipals(
	ctx context.Context,
	instances []ssoadminservice.Instance,
	principals *principalSet,
) ([]ssoadminservice.Principal, error) {
	if len(principals.types) == 0 {
		return nil, nil
	}
	identityStoreID := firstIdentityStoreID(instances)
	if identityStoreID == "" {
		return nil, nil
	}
	ids := make([]string, 0, len(principals.types))
	for id := range principals.types {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	resolved := make([]ssoadminservice.Principal, 0, len(ids))
	for _, id := range ids {
		principal, err := c.describePrincipal(ctx, identityStoreID, principals.types[id], id)
		if err != nil {
			return nil, err
		}
		resolved = append(resolved, principal)
	}
	return resolved, nil
}

func (c *Client) describePrincipal(
	ctx context.Context,
	identityStoreID string,
	principalType string,
	principalID string,
) (ssoadminservice.Principal, error) {
	principal := ssoadminservice.Principal{ID: principalID, Type: principalType}
	switch strings.ToUpper(strings.TrimSpace(principalType)) {
	case "GROUP":
		display, err := c.describeGroup(ctx, identityStoreID, principalID)
		if err != nil {
			return ssoadminservice.Principal{}, err
		}
		principal.DisplayName = display
	case "USER":
		display, err := c.describeUser(ctx, identityStoreID, principalID)
		if err != nil {
			return ssoadminservice.Principal{}, err
		}
		principal.DisplayName = display
	}
	return principal, nil
}

func (c *Client) describeGroup(ctx context.Context, identityStoreID, groupID string) (string, error) {
	var output *awsidentitystore.DescribeGroupOutput
	err := c.recordAPICall(ctx, "DescribeGroup", func(callCtx context.Context) error {
		var err error
		output, err = c.identityStore.DescribeGroup(callCtx, &awsidentitystore.DescribeGroupInput{
			IdentityStoreId: aws.String(identityStoreID),
			GroupId:         aws.String(groupID),
		})
		return err
	})
	if err != nil {
		if isAccessSkipError(err) {
			return "", nil
		}
		return "", err
	}
	if output == nil {
		return "", nil
	}
	return strings.TrimSpace(aws.ToString(output.DisplayName)), nil
}

func (c *Client) describeUser(ctx context.Context, identityStoreID, userID string) (string, error) {
	var output *awsidentitystore.DescribeUserOutput
	err := c.recordAPICall(ctx, "DescribeUser", func(callCtx context.Context) error {
		var err error
		output, err = c.identityStore.DescribeUser(callCtx, &awsidentitystore.DescribeUserInput{
			IdentityStoreId: aws.String(identityStoreID),
			UserId:          aws.String(userID),
		})
		return err
	})
	if err != nil {
		if isAccessSkipError(err) {
			return "", nil
		}
		return "", err
	}
	if output == nil {
		return "", nil
	}
	// Only DisplayName is read. Addresses, emails, phone numbers, birthdate,
	// structured Name, and other identity attributes are never accessed.
	return strings.TrimSpace(aws.ToString(output.DisplayName)), nil
}

func firstIdentityStoreID(instances []ssoadminservice.Instance) string {
	for _, instance := range instances {
		if id := strings.TrimSpace(instance.IdentityStoreID); id != "" {
			return id
		}
	}
	return ""
}
