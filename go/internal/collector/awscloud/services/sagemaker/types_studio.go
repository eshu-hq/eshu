// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sagemaker

import "time"

// Domain is the scanner-owned representation of one SageMaker Studio domain.
type Domain struct {
	ARN              string
	ID               string
	Name             string
	Status           string
	AuthMode         string
	VPCID            string
	SubnetIDs        []string
	URL              string
	CreationTime     time.Time
	LastModifiedTime time.Time
	Tags             map[string]string
}

// UserProfile is the scanner-owned representation of one SageMaker Studio user
// profile. It reports its parent domain so the user-profile-to-domain
// relationship can be emitted from list evidence alone.
type UserProfile struct {
	Name             string
	DomainID         string
	Status           string
	CreationTime     time.Time
	LastModifiedTime time.Time
}

// App is the scanner-owned representation of one SageMaker Studio app.
type App struct {
	ARN          string
	Name         string
	Type         string
	DomainID     string
	UserProfile  string
	SpaceName    string
	Status       string
	CreationTime time.Time
}

// InferenceComponent is the scanner-owned representation of one SageMaker
// inference component hosted on an endpoint.
type InferenceComponent struct {
	ARN              string
	Name             string
	Status           string
	EndpointName     string
	EndpointARN      string
	VariantName      string
	CreationTime     time.Time
	LastModifiedTime time.Time
}
