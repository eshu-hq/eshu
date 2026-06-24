// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relguard

import (
	"errors"
	"fmt"
	"go/ast"
	goparser "go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// resourceTypeConstPrefix is the identifier prefix every shared AWS resource
// type constant carries (for example ResourceTypeEC2VPC). The static layer
// collects the string value of every such constant so the guard knows the full
// set of resource families a relationship may legitimately target.
const resourceTypeConstPrefix = "ResourceType"

// KnownTargetTypeAllowlist is the explicit, reviewed set of relationship
// target_type values that are deliberately NOT backed by an awscloud
// ResourceType constant. Each entry is a forward reference to a resource family
// Eshu does not scan yet, or a synthetic/non-AWS join anchor. Adding an entry is
// a deliberate decision: it documents that the dangling target is expected, not
// a typo. The map value records why the value is allowed so a future reader
// (and the guard's own failure message) can tell an intentional forward
// reference from an accidental one.
//
// This list is the documented "aws_resource fallback for not-yet-scanned
// targets" escape hatch from issue #804, made explicit instead of implicit.
var KnownTargetTypeAllowlist = map[string]string{
	// ResourceTypeGeneric. The honest fallback when a reported identifier does
	// not match a known resource family; scanners still record the original
	// service-reported type in relationship attributes.
	"aws_resource": "generic fallback (awscloud.ResourceTypeGeneric) for identifiers with no known resource family",
	// CloudWatch metrics are an identity, not a scanned resource. The alarm
	// observes-metric edge anchors to a namespace/name identity that no scanner
	// publishes as a CloudResource.
	"aws_cloudwatch_metric": "synthetic identity: CloudWatch metrics are not a scanned resource",
	// VPC endpoint services are referenced by VPC endpoints but Eshu does not
	// yet emit a dedicated endpoint-service resource.
	"aws_vpc_endpoint_service": "forward reference: no VPC endpoint-service scanner yet",
	// EC2 instances are referenced as relationship targets (VPC, Global
	// Accelerator) but Eshu does not yet emit an EC2 instance CloudResource; the
	// ec2/awssdk mapper and globalaccelerator helper both key this value.
	"aws_ec2_instance": "forward reference: no EC2 instance resource scanner yet",
	// IAM server certificates are referenced by Classic ELB HTTPS/SSL listeners
	// (and IAM-uploaded certs predating ACM) but Eshu does not scan an IAM
	// server-certificate resource yet. The classic ELB scanner keys this value
	// for listeners whose SSLCertificateId is an :iam: server-certificate ARN.
	"aws_iam_server_certificate": "forward reference: IAM server certificate (classic ELB HTTPS listeners), no IAM server-certificate scanner yet",
	// Container images are OCI references shared by apprunner, ecs, lambda, and
	// sagemaker. They are correlated through the image-reference fact stream,
	// not as an AWS resource family.
	"container_image": "non-AWS join anchor: OCI container image reference resolved via image facts",
	// Git repositories are external (non-AWS) CodeBuild source endpoints; the
	// codebuild scanner labels them with a stable non-AWS-resource type, mirroring
	// container_image.
	"git_repository": "non-AWS join anchor: external git source endpoint (codebuild), see services/codebuild/relationships.go",
	// Public package registries (npmjs, PyPI, Maven Central, NuGet gallery,
	// crates.io, etc.) are external (non-AWS) endpoints a CodeArtifact
	// repository connects to through an external connection. The codeartifact
	// scanner labels them with a stable non-AWS-resource type, mirroring
	// container_image and git_repository.
	"public_package_registry": "non-AWS join anchor: external public package registry endpoint (codeartifact external connection), see services/codeartifact/relationships.go",
	// CodePipeline source providers (GitHub, CodeStarSourceConnection, Bitbucket)
	// are a synthetic provider category, not a scanned resource; the constant
	// guarantees the edge is never empty. See ResourceTypeCodePipelineSourceProvider.
	"aws_codepipeline_source_provider": "synthetic provider category for CodePipeline sources, see services/codepipeline/relationships.go",
	// IAM SAML/OIDC identity providers are referenced as Cognito identity-pool
	// trust evidence; Eshu does not scan an IAM identity-provider resource type.
	"aws_iam_identity_provider": "forward reference: IAM SAML/OIDC provider trust evidence (cognito), no IAM identity-provider scanner yet",
	// CloudFront associates either a classic (v1) WAF web ACL or a WAFv2 web ACL
	// through one WebACLID field; the edge cannot pick a v1/v2 type without more
	// signal. Allowlisted pending a modeling decision rather than mis-keyed to
	// aws_wafv2_web_acl (which would dangle for classic-WAF associations).
	"aws_waf_web_acl": "ambiguous classic-WAF/WAFv2 association from CloudFront WebACLID; pending modeling decision, see services/cloudfront/relationships.go",
}

// DeclaredResourceTypeValues parses every Go source file directly under
// awscloudDir and returns the sorted, de-duplicated set of string values
// assigned to constants whose name starts with ResourceType. These are the
// resource families a relationship target_type may name. The walk is source
// based (go/parser, no type checking) so it stays fast and has no dependency on
// the packages it derives the set from.
func DeclaredResourceTypeValues(awscloudDir string) ([]string, error) {
	entries, err := os.ReadDir(awscloudDir)
	if err != nil {
		return nil, fmt.Errorf("read awscloud dir %q: %w", awscloudDir, err)
	}
	seen := map[string]struct{}{}
	fset := token.NewFileSet()
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		if strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(awscloudDir, entry.Name())
		file, parseErr := goparser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			return nil, fmt.Errorf("parse %q: %w", path, parseErr)
		}
		collectResourceTypeConstants(file, seen)
	}
	return sortedKeys(seen), nil
}

// collectResourceTypeConstants records the string value of every
// ResourceType-prefixed constant declared in file into seen.
func collectResourceTypeConstants(file *ast.File, seen map[string]struct{}) {
	for _, decl := range file.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.CONST {
			continue
		}
		for _, spec := range genDecl.Specs {
			valueSpec, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range valueSpec.Names {
				if !strings.HasPrefix(name.Name, resourceTypeConstPrefix) {
					continue
				}
				if i >= len(valueSpec.Values) {
					continue
				}
				if value, ok := stringLiteral(valueSpec.Values[i]); ok {
					seen[value] = struct{}{}
				}
			}
		}
	}
}

// KnownTargetTypes returns the union of every declared ResourceType constant
// value and the documented KnownTargetTypeAllowlist. It is the single source of
// truth both guard layers check emitted target types against.
func KnownTargetTypes(awscloudDir string) (map[string]struct{}, error) {
	declared, err := DeclaredResourceTypeValues(awscloudDir)
	if err != nil {
		return nil, err
	}
	known := make(map[string]struct{}, len(declared)+len(KnownTargetTypeAllowlist))
	for _, value := range declared {
		known[value] = struct{}{}
	}
	for value := range KnownTargetTypeAllowlist {
		known[value] = struct{}{}
	}
	return known, nil
}

// Validate asserts every statically resolved literal is non-empty and present
// in known. ConstBacked entries are skipped because the compiler already
// guarantees they name a declared constant. It returns one error per offending
// literal so a guard failure names every defect at once.
func Validate(literals []EmittedTargetType, known map[string]struct{}) []error {
	var errs []error
	for _, lit := range literals {
		if lit.ConstBacked {
			continue
		}
		if strings.TrimSpace(lit.Value) == "" {
			errs = append(errs, fmt.Errorf("%s: empty target_type literal", lit.File))
			continue
		}
		if _, ok := known[lit.Value]; !ok {
			errs = append(errs, fmt.Errorf(
				"%s: target_type %q is not a declared awscloud.ResourceType constant and is not in relguard.KnownTargetTypeAllowlist; "+
					"a relationship to it would dangle. Fix the target_type to the value the target scanner publishes, "+
					"or add a documented allowlist entry if the target is deliberately not scanned yet",
				lit.File, lit.Value,
			))
		}
	}
	return errs
}

// stringLiteral returns the unquoted value of a basic string-literal expression.
func stringLiteral(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return value, true
}

// sortedKeys returns the sorted keys of a string set.
func sortedKeys(set map[string]struct{}) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// errJoin joins guard errors into one for callers that want a single error.
func errJoin(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}

// ValidateEmitted is the one-call static guard: it derives the known target
// types from awscloudDir, walks servicesDir, validates the resolved literals,
// and returns a single joined error naming every offending literal. The number
// of resolved literals and the number of unresolved (runtime-only) target-type
// expressions are returned so the guard test can assert it observed real input
// rather than silently walking an empty tree.
func ValidateEmitted(awscloudDir, servicesDir string) (resolved, unresolved int, err error) {
	known, err := KnownTargetTypes(awscloudDir)
	if err != nil {
		return 0, 0, err
	}
	literals, unresolved, err := EmittedTargetTypeLiterals(servicesDir)
	if err != nil {
		return 0, 0, err
	}
	if errs := Validate(literals, known); len(errs) > 0 {
		return len(literals), unresolved, errJoin(errs)
	}
	return len(literals), unresolved, nil
}
