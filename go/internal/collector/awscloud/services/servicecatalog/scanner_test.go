package servicecatalog

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// fakeClient is a deterministic in-memory Service Catalog client for scanner
// tests. Per-portfolio principals and per-product portfolios are keyed by the
// AWS identifier the scanner passes to the lookup methods.
type fakeClient struct {
	portfolios          []Portfolio
	products            []Product
	provisioned         []ProvisionedProduct
	principalsByID      map[string][]Principal
	portfoliosByProduct map[string][]Portfolio
	err                 error
}

func (f *fakeClient) ListPortfolios(context.Context) ([]Portfolio, error) {
	return f.portfolios, f.err
}

func (f *fakeClient) ListProducts(context.Context) ([]Product, error) {
	return f.products, f.err
}

func (f *fakeClient) ListProvisionedProducts(context.Context) ([]ProvisionedProduct, error) {
	return f.provisioned, f.err
}

func (f *fakeClient) PortfoliosForProduct(_ context.Context, productID string) ([]Portfolio, error) {
	return f.portfoliosByProduct[productID], f.err
}

func (f *fakeClient) PrincipalsForPortfolio(_ context.Context, portfolioID string) ([]Principal, error) {
	return f.principalsByID[portfolioID], f.err
}

func boundaryFor(serviceKind string) awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         serviceKind,
		ScopeID:             "scope-1",
		GenerationID:        "gen-1",
		CollectorInstanceID: "collector-aws-1",
		FencingToken:        1,
		ObservedAt:          time.Date(2026, 5, 27, 0, 0, 0, 0, time.UTC),
	}
}

func sampleClient() *fakeClient {
	return &fakeClient{
		portfolios: []Portfolio{{
			ID:           "port-abc123",
			ARN:          "arn:aws:catalog:us-east-1:123456789012:portfolio/port-abc123",
			DisplayName:  "Platform Portfolio",
			ProviderName: "Platform Team",
			Description:  "Shared platform products",
			CreatedTime:  time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		}},
		products: []Product{{
			ID:          "prod-xyz789",
			ARN:         "arn:aws:catalog:us-east-1:123456789012:product/prod-xyz789",
			Name:        "Bucket Product",
			ProductType: "CLOUD_FORMATION_TEMPLATE",
			Owner:       "Platform Team",
			Distributor: "Internal",
			Status:      "AVAILABLE",
			CreatedTime: time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC),
		}},
		provisioned: []ProvisionedProduct{{
			ID:                     "pp-stack001",
			ARN:                    "arn:aws:servicecatalog:us-east-1:123456789012:stack/team/pp-stack001",
			Name:                   "team-bucket",
			Status:                 "AVAILABLE",
			Type:                   "CFN_STACK",
			ProductID:              "prod-xyz789",
			ProvisioningArtifactID: "pa-001",
			PhysicalID:             "arn:aws:cloudformation:us-east-1:123456789012:stack/SC-team-bucket/abcd-1234",
			CreatedTime:            time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC),
		}},
		principalsByID: map[string][]Principal{
			"port-abc123": {{
				ARN:  "arn:aws:iam::123456789012:role/ServiceCatalogLaunchRole",
				Type: "IAM",
			}},
		},
		portfoliosByProduct: map[string][]Portfolio{
			"prod-xyz789": {{
				ID:  "port-abc123",
				ARN: "arn:aws:catalog:us-east-1:123456789012:portfolio/port-abc123",
			}},
		},
	}
}

func scanFixture(t *testing.T, client Client) []facts.Envelope {
	t.Helper()
	envelopes, err := Scanner{Client: client}.Scan(context.Background(), boundaryFor(awscloud.ServiceServiceCatalog))
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	return envelopes
}

func findResource(t *testing.T, envelopes []facts.Envelope, resourceType string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if envelope.Payload["resource_type"] == resourceType {
			return envelope
		}
	}
	t.Fatalf("no resource envelope of type %q in %d envelopes", resourceType, len(envelopes))
	return facts.Envelope{}
}

func findRelationship(t *testing.T, envelopes []facts.Envelope, relationshipType string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if envelope.Payload["relationship_type"] == relationshipType {
			return envelope
		}
	}
	t.Fatalf("no relationship envelope of type %q in %d envelopes", relationshipType, len(envelopes))
	return facts.Envelope{}
}

// TestClientInterfaceExcludesMutationAPIs is the primary metadata-only guard for
// the Service Catalog scanner. The scanner-owned Client interface must never
// expose a provisioning, association, constraint-mutation, or template/record
// read API. The forbidden set is excluded by construction; a maintainer adding
// one of these methods to Client breaks the metadata-only contract and this
// test fails.
func TestClientInterfaceExcludesMutationAPIs(t *testing.T) {
	clientType := reflect.TypeOf((*Client)(nil)).Elem()
	forbidden := []string{
		// Provisioned-product mutation.
		"ProvisionProduct",
		"UpdateProvisionedProduct",
		"TerminateProvisionedProduct",
		"ExecuteProvisionedProductPlan",
		"ExecuteProvisionedProductServiceAction",
		"CreateProvisionedProductPlan",
		"DeleteProvisionedProductPlan",
		"ImportAsProvisionedProduct",
		// Product mutation.
		"CreateProduct",
		"UpdateProduct",
		"DeleteProduct",
		// Portfolio mutation.
		"CreatePortfolio",
		"UpdatePortfolio",
		"DeletePortfolio",
		"CreatePortfolioShare",
		"DeletePortfolioShare",
		// Association mutation.
		"AssociatePrincipalWithPortfolio",
		"DisassociatePrincipalFromPortfolio",
		"AssociateProductWithPortfolio",
		"DisassociateProductFromPortfolio",
		// Constraint mutation.
		"CreateConstraint",
		"UpdateConstraint",
		"DeleteConstraint",
		// Template-body and record-output reads.
		"DescribeProvisioningArtifact",
		"DescribeRecord",
		"GetProvisionedProductOutputs",
		"DescribeProvisioningParameters",
	}
	for _, name := range forbidden {
		if _, ok := clientType.MethodByName(name); ok {
			t.Fatalf("Client exposes forbidden method %q; Service Catalog scanner must stay metadata-only", name)
		}
	}
}

func TestScanRejectsForeignServiceKind(t *testing.T) {
	_, err := Scanner{Client: sampleClient()}.Scan(context.Background(), boundaryFor("ec2"))
	if err == nil {
		t.Fatal("Scan() error = nil, want service_kind rejection")
	}
}

func TestScanRequiresClient(t *testing.T) {
	_, err := Scanner{}.Scan(context.Background(), boundaryFor(awscloud.ServiceServiceCatalog))
	if err == nil {
		t.Fatal("Scan() error = nil, want client-required rejection")
	}
}

func TestScanEmitsPortfolioResource(t *testing.T) {
	envelope := findResource(t, scanFixture(t, sampleClient()), awscloud.ResourceTypeServiceCatalogPortfolio)
	wantID := "arn:aws:catalog:us-east-1:123456789012:portfolio/port-abc123"
	if got := envelope.Payload["resource_id"]; got != wantID {
		t.Fatalf("portfolio resource_id = %v, want %v", got, wantID)
	}
	if got := envelope.Payload["name"]; got != "Platform Portfolio" {
		t.Fatalf("portfolio name = %v, want Platform Portfolio", got)
	}
}

func TestScanEmitsProductResource(t *testing.T) {
	envelope := findResource(t, scanFixture(t, sampleClient()), awscloud.ResourceTypeServiceCatalogProduct)
	wantID := "arn:aws:catalog:us-east-1:123456789012:product/prod-xyz789"
	if got := envelope.Payload["resource_id"]; got != wantID {
		t.Fatalf("product resource_id = %v, want %v", got, wantID)
	}
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("product attributes type = %T, want map", envelope.Payload["attributes"])
	}
	if got := attributes["product_type"]; got != "CLOUD_FORMATION_TEMPLATE" {
		t.Fatalf("product type attribute = %v, want CLOUD_FORMATION_TEMPLATE", got)
	}
	if got := attributes["owner"]; got != "Platform Team" {
		t.Fatalf("product owner attribute = %v, want Platform Team", got)
	}
}

func TestScanEmitsProvisionedProductResource(t *testing.T) {
	envelope := findResource(t, scanFixture(t, sampleClient()), awscloud.ResourceTypeServiceCatalogProvisionedProduct)
	wantID := "arn:aws:servicecatalog:us-east-1:123456789012:stack/team/pp-stack001"
	if got := envelope.Payload["resource_id"]; got != wantID {
		t.Fatalf("provisioned product resource_id = %v, want %v", got, wantID)
	}
	if got := envelope.Payload["state"]; got != "AVAILABLE" {
		t.Fatalf("provisioned product state = %v, want AVAILABLE", got)
	}
}

func TestScanEmitsProvisionedProductToStackEdge(t *testing.T) {
	envelope := findRelationship(t, scanFixture(t, sampleClient()),
		awscloud.RelationshipServiceCatalogProvisionedProductDeploysCloudFormationStack)
	if got := envelope.Payload["target_type"]; got != awscloud.ResourceTypeCloudFormationStack {
		t.Fatalf("target_type = %v, want %v", got, awscloud.ResourceTypeCloudFormationStack)
	}
	wantTarget := "arn:aws:cloudformation:us-east-1:123456789012:stack/SC-team-bucket/abcd-1234"
	if got := envelope.Payload["target_resource_id"]; got != wantTarget {
		t.Fatalf("target_resource_id = %v, want %v", got, wantTarget)
	}
	if got := envelope.Payload["target_arn"]; got != wantTarget {
		t.Fatalf("target_arn = %v, want %v", got, wantTarget)
	}
	wantSource := "arn:aws:servicecatalog:us-east-1:123456789012:stack/team/pp-stack001"
	if got := envelope.Payload["source_resource_id"]; got != wantSource {
		t.Fatalf("source_resource_id = %v, want %v (must match provisioned-product node id)", got, wantSource)
	}
}

func TestScanEmitsProductToPortfolioEdge(t *testing.T) {
	envelope := findRelationship(t, scanFixture(t, sampleClient()),
		awscloud.RelationshipServiceCatalogProductInPortfolio)
	if got := envelope.Payload["target_type"]; got != awscloud.ResourceTypeServiceCatalogPortfolio {
		t.Fatalf("target_type = %v, want %v", got, awscloud.ResourceTypeServiceCatalogPortfolio)
	}
	wantTarget := "arn:aws:catalog:us-east-1:123456789012:portfolio/port-abc123"
	if got := envelope.Payload["target_resource_id"]; got != wantTarget {
		t.Fatalf("target_resource_id = %v, want %v (must match portfolio node id)", got, wantTarget)
	}
	wantSource := "arn:aws:catalog:us-east-1:123456789012:product/prod-xyz789"
	if got := envelope.Payload["source_resource_id"]; got != wantSource {
		t.Fatalf("source_resource_id = %v, want %v (must match product node id)", got, wantSource)
	}
}

func TestScanEmitsPortfolioToIAMRoleEdge(t *testing.T) {
	envelope := findRelationship(t, scanFixture(t, sampleClient()),
		awscloud.RelationshipServiceCatalogPortfolioGrantsPrincipal)
	if got := envelope.Payload["target_type"]; got != awscloud.ResourceTypeIAMRole {
		t.Fatalf("target_type = %v, want %v", got, awscloud.ResourceTypeIAMRole)
	}
	wantTarget := "arn:aws:iam::123456789012:role/ServiceCatalogLaunchRole"
	if got := envelope.Payload["target_resource_id"]; got != wantTarget {
		t.Fatalf("target_resource_id = %v, want %v (must match IAM role node id)", got, wantTarget)
	}
	if got := envelope.Payload["target_arn"]; got != wantTarget {
		t.Fatalf("target_arn = %v, want %v", got, wantTarget)
	}
}

// TestScanOmitsNonCFNStackProvisionedProductEdge confirms the
// provisioned-product-to-stack edge is gated on the CFN_STACK type. A Terraform
// provisioned product carries a physical id that is not a CloudFormation stack
// ARN, so promoting it to the stack edge would dangle.
func TestScanOmitsNonCFNStackProvisionedProductEdge(t *testing.T) {
	client := sampleClient()
	client.provisioned = []ProvisionedProduct{{
		ID:         "pp-tf001",
		ARN:        "arn:aws:servicecatalog:us-east-1:123456789012:stack/team/pp-tf001",
		Name:       "tf-workspace",
		Status:     "AVAILABLE",
		Type:       "TERRAFORM_OPEN_SOURCE",
		PhysicalID: "arn:aws:s3:::tf-state-bucket",
	}}
	for _, envelope := range scanFixture(t, client) {
		if envelope.FactKind == facts.AWSRelationshipFactKind &&
			envelope.Payload["relationship_type"] ==
				awscloud.RelationshipServiceCatalogProvisionedProductDeploysCloudFormationStack {
			t.Fatal("emitted a stack edge for a non-CFN_STACK provisioned product")
		}
	}
}

// TestScanOmitsNonRolePrincipalEdge confirms the portfolio-to-IAM-role edge is
// gated on a fully defined IAM role ARN. IAM users, groups, and IAM_PATTERN
// wildcard principals name no concrete IAM role node, so they are skipped.
func TestScanOmitsNonRolePrincipalEdge(t *testing.T) {
	client := sampleClient()
	client.principalsByID = map[string][]Principal{
		"port-abc123": {
			{ARN: "arn:aws:iam::123456789012:user/alice", Type: "IAM"},
			{ARN: "arn:aws:iam:::role/*", Type: "IAM_PATTERN"},
		},
	}
	for _, envelope := range scanFixture(t, client) {
		if envelope.FactKind == facts.AWSRelationshipFactKind &&
			envelope.Payload["relationship_type"] ==
				awscloud.RelationshipServiceCatalogPortfolioGrantsPrincipal {
			t.Fatalf("emitted a role edge for a non-role / wildcard principal: %v",
				envelope.Payload["target_resource_id"])
		}
	}
}

// TestScanEmitsNoSecretShapedPayload asserts the scanner never persists
// provisioning parameter values, template bodies, or record output values. The
// scanner-owned types carry no such field, so the resource attribute maps must
// contain only the metadata identity keys.
func TestScanEmitsNoSecretShapedPayload(t *testing.T) {
	allowed := map[string]map[string]struct{}{
		awscloud.ResourceTypeServiceCatalogPortfolio: keySet(
			"portfolio_id", "display_name", "provider_name", "description", "created_time",
		),
		awscloud.ResourceTypeServiceCatalogProduct: keySet(
			"product_id", "product_type", "owner", "distributor", "status", "created_time",
		),
		awscloud.ResourceTypeServiceCatalogProvisionedProduct: keySet(
			"provisioned_product_id", "status", "provisioned_product_type", "product_id",
			"provisioning_artifact_id", "provisioning_artifact_name", "physical_id", "created_time",
		),
	}
	for _, envelope := range scanFixture(t, sampleClient()) {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		resourceType, _ := envelope.Payload["resource_type"].(string)
		want, ok := allowed[resourceType]
		if !ok {
			continue
		}
		attributes, _ := envelope.Payload["attributes"].(map[string]any)
		for key := range attributes {
			if _, ok := want[key]; !ok {
				t.Fatalf("%s attribute %q is not in the metadata-only allowlist", resourceType, key)
			}
		}
	}
}

// TestScanEmitsEveryDocumentedAttribute guards the resource-type doc contract
// against drift in the narrowing direction: the doc comments on
// ResourceTypeServiceCatalog{Portfolio,Product,ProvisionedProduct} and the
// service-coverage doc must name every attribute the scanner actually emits. If
// the scanner gains an attribute the documented contract omits, this test fails
// so the doc is updated in lockstep. Paired with TestScanEmitsNoSecretShapedPayload
// (no key outside the allowlist), the two tests pin the emitted set exactly.
func TestScanEmitsEveryDocumentedAttribute(t *testing.T) {
	documented := map[string][]string{
		awscloud.ResourceTypeServiceCatalogPortfolio: {
			"portfolio_id", "display_name", "provider_name", "description", "created_time",
		},
		awscloud.ResourceTypeServiceCatalogProduct: {
			"product_id", "product_type", "owner", "distributor", "status", "created_time",
		},
		awscloud.ResourceTypeServiceCatalogProvisionedProduct: {
			"provisioned_product_id", "status", "provisioned_product_type", "product_id",
			"provisioning_artifact_id", "provisioning_artifact_name", "physical_id", "created_time",
		},
	}
	seen := make(map[string]map[string]struct{}, len(documented))
	for _, envelope := range scanFixture(t, sampleClient()) {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		resourceType, _ := envelope.Payload["resource_type"].(string)
		if _, ok := documented[resourceType]; !ok {
			continue
		}
		attributes, _ := envelope.Payload["attributes"].(map[string]any)
		keys := make(map[string]struct{}, len(attributes))
		for key := range attributes {
			keys[key] = struct{}{}
		}
		seen[resourceType] = keys
	}
	for resourceType, keys := range documented {
		got, ok := seen[resourceType]
		if !ok {
			t.Fatalf("no %s resource emitted; cannot verify documented attributes", resourceType)
		}
		for _, key := range keys {
			if _, ok := got[key]; !ok {
				t.Fatalf("%s emits no documented attribute %q; doc contract claims it", resourceType, key)
			}
		}
	}
}

func keySet(keys ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		set[key] = struct{}{}
	}
	return set
}
