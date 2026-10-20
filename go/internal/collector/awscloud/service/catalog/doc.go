// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package servicecatalog maps AWS Service Catalog portfolio, product, and
// provisioned-product metadata into AWS cloud collector facts.
//
// The scanner emits reported-confidence resources for portfolios, products, and
// provisioned products plus relationships for provisioned-product-to-
// CloudFormation-stack deployment, product-to-portfolio association, and
// portfolio-to-IAM-role principal grants. Provisioning-artifact template
// bodies, launch-constraint policy documents, provisioning parameter values,
// record output values, and every provisioning, association, and constraint
// mutation API stay outside this package contract.
package servicecatalog
