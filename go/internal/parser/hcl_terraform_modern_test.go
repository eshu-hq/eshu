package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathHCLTerraformModernBlockMetadata(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "main.tf")
	writeTestFile(
		t,
		filePath,
		`import {
  to       = random_string.test1
  provider = random.thisone
  id       = "importaliased"
}

moved {
  from = test.foo
  to   = module.a.test.foo
}

removed {
  from = module.child.test_resource.baz
  lifecycle {
    destroy = true
  }
}

check "api" {
  assert {
    condition     = null_resource.a.id != ""
    error_message = "A has no id."
  }
}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	importBlock := findNamedBucketItem(t, got, "terraform_imports", "random_string.test1")
	if got, want := importBlock["to"], "random_string.test1"; got != want {
		t.Fatalf("terraform_imports[random_string.test1].to = %#v, want %#v", got, want)
	}
	if got, want := importBlock["provider"], "random.thisone"; got != want {
		t.Fatalf("terraform_imports[random_string.test1].provider = %#v, want %#v", got, want)
	}
	if got, want := importBlock["id"], "importaliased"; got != want {
		t.Fatalf("terraform_imports[random_string.test1].id = %#v, want %#v", got, want)
	}

	movedBlock := findNamedBucketItem(t, got, "terraform_moved_blocks", "test.foo -> module.a.test.foo")
	if got, want := movedBlock["from"], "test.foo"; got != want {
		t.Fatalf("terraform_moved_blocks[test.foo].from = %#v, want %#v", got, want)
	}
	if got, want := movedBlock["to"], "module.a.test.foo"; got != want {
		t.Fatalf("terraform_moved_blocks[test.foo].to = %#v, want %#v", got, want)
	}

	removedBlock := findNamedBucketItem(t, got, "terraform_removed_blocks", "module.child.test_resource.baz")
	if got, want := removedBlock["from"], "module.child.test_resource.baz"; got != want {
		t.Fatalf("terraform_removed_blocks[module.child.test_resource.baz].from = %#v, want %#v", got, want)
	}
	if got, want := removedBlock["lifecycle_destroy"], "true"; got != want {
		t.Fatalf("terraform_removed_blocks[module.child.test_resource.baz].lifecycle_destroy = %#v, want %#v", got, want)
	}

	checkBlock := findNamedBucketItem(t, got, "terraform_checks", "api")
	if got, want := checkBlock["assert_count"], 1; got != want {
		t.Fatalf("terraform_checks[api].assert_count = %#v, want %#v", got, want)
	}
}

func TestDefaultEngineParsePathHCLTerraformLockFileProviderMetadata(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, ".terraform.lock.hcl")
	writeTestFile(
		t,
		filePath,
		`provider "registry.terraform.io/hashicorp/local" {
  version     = "2.2.3"
  constraints = "2.2.3"
  hashes = [
    "h1:FvRIEgCmAezgZUqb2F+PZ9WnSSnR5zbEM2ZI+GLmbMk=",
    "zh:04f0978bb3e052707b8e82e46780c371ac1c66b689b4a23bbc2f58865ab7d5c0",
  ]
}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	lockProvider := findNamedBucketItem(
		t,
		got,
		"terraform_lock_providers",
		"registry.terraform.io/hashicorp/local",
	)
	if got, want := lockProvider["version"], "2.2.3"; got != want {
		t.Fatalf("terraform_lock_providers[local].version = %#v, want %#v", got, want)
	}
	if got, want := lockProvider["constraints"], "2.2.3"; got != want {
		t.Fatalf("terraform_lock_providers[local].constraints = %#v, want %#v", got, want)
	}
	if got, want := lockProvider["hash_count"], 2; got != want {
		t.Fatalf("terraform_lock_providers[local].hash_count = %#v, want %#v", got, want)
	}
	assertEmptyNamedBucket(t, got, "terraform_providers")
}
