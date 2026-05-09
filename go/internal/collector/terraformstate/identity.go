package terraformstate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

func normalizedParseOptions(options ParseOptions) ParseOptions {
	if options.ObservedAt.IsZero() {
		options.ObservedAt = options.Generation.ObservedAt
	}
	if options.ObservedAt.IsZero() {
		options.ObservedAt = time.Now().UTC()
	}
	options.ObservedAt = options.ObservedAt.UTC()
	return options
}

func resourceAddress(resource resourceContext, instance instanceContext, instanceIndex int) string {
	prefix := ""
	if module := strings.TrimSpace(resource.Module); module != "" {
		prefix = module + "."
	}
	address := prefix + strings.TrimSpace(resource.Type) + "." + strings.TrimSpace(resource.Name)
	if instance.HasIndexKey {
		return fmt.Sprintf("%s[key:%s]", address, instance.IndexKeyHash)
	}
	if instanceIndex > 0 {
		return fmt.Sprintf("%s[index:%d]", address, instanceIndex)
	}
	return address
}

func validateResourceIdentity(resource resourceContext) error {
	if strings.TrimSpace(resource.Mode) == "" {
		return fmt.Errorf("terraform state resource mode must not be blank")
	}
	if strings.TrimSpace(resource.Type) == "" {
		return fmt.Errorf("terraform state resource type must not be blank")
	}
	if strings.TrimSpace(resource.Name) == "" {
		return fmt.Errorf("terraform state resource name must not be blank")
	}
	return nil
}

func expectedSnapshotIdentity(freshnessHint string) (string, int64, error) {
	var lineage string
	var serialValue string
	for _, field := range strings.Fields(freshnessHint) {
		if value, ok := strings.CutPrefix(field, "lineage="); ok {
			lineage = value
		}
		if value, ok := strings.CutPrefix(field, "serial="); ok {
			serialValue = value
		}
	}
	if strings.TrimSpace(lineage) == "" {
		return "", 0, fmt.Errorf("terraform state generation freshness hint must include lineage")
	}
	serial, err := strconv.ParseInt(serialValue, 10, 64)
	if err != nil {
		return "", 0, fmt.Errorf("terraform state generation freshness hint must include serial: %w", err)
	}
	return lineage, serial, nil
}

func instanceIndexHash(value any) string {
	return facts.StableID("TerraformStateInstanceIndexKey", map[string]any{
		"index_key": value,
	})
}

func redactionMap(value redact.Value) map[string]any {
	return map[string]any{
		"marker": value.Marker,
		"reason": value.Reason,
		"source": value.Source,
	}
}

func sourceURI(source StateKey) string {
	return fmt.Sprintf("terraform_state:%s:%s", source.BackendKind, locatorHash(source))
}

func locatorHash(source StateKey) string {
	sum := sha256.Sum256([]byte(string(source.BackendKind) + "\x00" + source.Locator + "\x00" + source.VersionID))
	return hex.EncodeToString(sum[:])
}
