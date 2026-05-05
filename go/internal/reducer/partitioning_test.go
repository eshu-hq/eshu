package reducer

import (
	"testing"
)

func TestPartitionForKeyStableResult(t *testing.T) {
	t.Parallel()

	p1, err := PartitionForKey("pk-1", 4)
	if err != nil {
		t.Fatalf("PartitionForKey: %v", err)
	}
	p2, err := PartitionForKey("pk-1", 4)
	if err != nil {
		t.Fatalf("PartitionForKey: %v", err)
	}
	if p1 != p2 {
		t.Errorf("same key returned different partitions: %d vs %d", p1, p2)
	}
}

func TestPartitionForKeyDistributes(t *testing.T) {
	t.Parallel()

	keys := []string{
		"key-0", "key-1", "key-2", "key-3", "key-4",
		"key-5", "key-6", "key-7", "key-8", "key-9",
	}
	partitionCount := 4
	seen := make(map[int]bool)
	for _, key := range keys {
		p, err := PartitionForKey(key, partitionCount)
		if err != nil {
			t.Fatalf("PartitionForKey(%q): %v", key, err)
		}
		seen[p] = true
	}
	if len(seen) < 2 {
		t.Errorf("expected distribution across multiple partitions, got %d unique", len(seen))
	}
}

func TestPartitionForKeyRejectsZeroOrNegative(t *testing.T) {
	t.Parallel()

	for _, count := range []int{0, -1, -100} {
		_, err := PartitionForKey("any-key", count)
		if err == nil {
			t.Errorf("expected error for partitionCount=%d, got nil", count)
		}
	}
}

func TestPartitionForKeyInRange(t *testing.T) {
	t.Parallel()

	keys := []string{"a", "b", "c", "longer-key", "another"}
	for _, count := range []int{1, 2, 4, 8, 16} {
		for _, key := range keys {
			p, err := PartitionForKey(key, count)
			if err != nil {
				t.Fatalf("PartitionForKey(%q, %d): %v", key, count, err)
			}
			if p < 0 || p >= count {
				t.Errorf("PartitionForKey(%q, %d) = %d, want [0, %d)", key, count, p, count)
			}
		}
	}
}

func TestPartitionForKeyCrossLanguageParity(t *testing.T) {
	t.Parallel()

	// Verified against Python: partition_for_key("pk-1", partition_count=4) == 1
	p, err := PartitionForKey("pk-1", 4)
	if err != nil {
		t.Fatalf("PartitionForKey: %v", err)
	}
	if p != 1 {
		t.Errorf("PartitionForKey(pk-1, 4) = %d, want 1 (Python parity)", p)
	}
}
