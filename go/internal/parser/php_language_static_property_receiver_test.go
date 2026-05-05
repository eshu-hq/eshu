package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathPHPInfersStaticPropertyReceiverChains(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "static_property_receiver.php")
	writeTestFile(
		t,
		filePath,
		`<?php
class Service {
    public function info(string $message): void {}
}

class Registry {
    private static Service $service;

    public static function boot(): void {
        self::$service->info('ready');
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

	call := assertBucketItemByFieldValue(t, got, "function_calls", "full_name", "self::$service.info")
	phpAssertStringFieldValue(t, call, "inferred_obj_type", "Service")
}

func TestDefaultEngineParsePathPHPInfersParentAndStaticPropertyReceiverChains(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "parent_static_property_receiver.php")
	writeTestFile(
		t,
		filePath,
		`<?php
class Service {
    public function info(string $message): void {}
}

class BaseRegistry {
    protected static Service $service;
}

class ChildRegistry extends BaseRegistry {
    public static function boot(string $message): void {
        parent::$service->info($message);
        static::$service->info($message);
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

	want := map[string]string{
		"parent::$service.info": "Service",
		"static::$service.info": "Service",
	}
	items, ok := got["function_calls"].([]map[string]any)
	if !ok {
		t.Fatalf("function_calls = %T, want []map[string]any", got["function_calls"])
	}
	for _, item := range items {
		fullName, _ := item["full_name"].(string)
		if wantType, ok := want[fullName]; ok {
			phpAssertStringFieldValue(t, item, "inferred_obj_type", wantType)
			delete(want, fullName)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing inferred receiver calls: %#v in %#v", want, items)
	}
}

func TestDefaultEngineParsePathPHPInfersDeepStaticPropertyReceiverChains(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "deep_static_property_receiver.php")
	writeTestFile(
		t,
		filePath,
		`<?php
class Service {
    public function info(string $message): void {}
}

class Factory {
    public function createService(): Service {
        return new Service();
    }
}

class BaseRegistry {
    protected static Factory $factory;
}

class ChildRegistry extends BaseRegistry {
    public static function boot(string $message): void {
        self::$factory->createService()->info($message);
        parent::$factory->createService()->info($message);
        static::$factory->createService()->info($message);
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

	want := map[string]string{
		"self::$factory->createService().info":   "Service",
		"parent::$factory->createService().info": "Service",
		"static::$factory->createService().info": "Service",
	}
	items, ok := got["function_calls"].([]map[string]any)
	if !ok {
		t.Fatalf("function_calls = %T, want []map[string]any", got["function_calls"])
	}
	for _, item := range items {
		fullName, _ := item["full_name"].(string)
		if wantType, ok := want[fullName]; ok {
			phpAssertStringFieldValue(t, item, "inferred_obj_type", wantType)
			delete(want, fullName)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing inferred deep receiver calls: %#v in %#v", want, items)
	}
}
