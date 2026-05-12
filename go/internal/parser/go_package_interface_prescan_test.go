package parser

import "testing"

func TestPackagePrescanPassWorkerCount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		workers   int
		fileCount int
		want      int
	}{
		{
			name:      "empty file set disables workers",
			workers:   4,
			fileCount: 0,
			want:      0,
		},
		{
			name:      "nonpositive worker request defaults to one",
			workers:   0,
			fileCount: 3,
			want:      1,
		},
		{
			name:      "negative worker request defaults to one",
			workers:   -2,
			fileCount: 3,
			want:      1,
		},
		{
			name:      "requested workers below file count are preserved",
			workers:   2,
			fileCount: 5,
			want:      2,
		},
		{
			name:      "requested workers are capped at file count",
			workers:   8,
			fileCount: 3,
			want:      3,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := packagePrescanPassWorkerCount(test.workers, test.fileCount)
			if got != test.want {
				t.Fatalf("packagePrescanPassWorkerCount(%d, %d) = %d, want %d", test.workers, test.fileCount, got, test.want)
			}
		})
	}
}
