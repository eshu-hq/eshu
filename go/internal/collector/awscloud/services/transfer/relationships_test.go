package transfer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestPartitionDerivesFromBoundaryRegion(t *testing.T) {
	cases := map[string]string{
		"us-east-1":      "aws",
		"eu-west-3":      "aws",
		"us-gov-west-1":  "aws-us-gov",
		"us-gov-east-1":  "aws-us-gov",
		"cn-north-1":     "aws-cn",
		"cn-northwest-1": "aws-cn",
		"":               "aws",
	}
	for region, want := range cases {
		if got := partition(awscloud.Boundary{Region: region}); got != want {
			t.Fatalf("partition(region=%q) = %q, want %q", region, got, want)
		}
	}
}

func TestFirstPathSegmentSplitsHomeDirectory(t *testing.T) {
	cases := []struct {
		path          string
		wantSegment   string
		wantRemainder string
		wantOK        bool
	}{
		{"/landing-bucket/home/user", "landing-bucket", "home/user", true},
		{"/landing-bucket", "landing-bucket", "", true},
		{"landing-bucket/x", "landing-bucket", "x", true},
		{"/fs-0a1b/home", "fs-0a1b", "home", true},
		{"/", "", "", false},
		{"", "", "", false},
		{"   ", "", "", false},
	}
	for _, tc := range cases {
		segment, remainder, ok := firstPathSegment(tc.path)
		if segment != tc.wantSegment || remainder != tc.wantRemainder || ok != tc.wantOK {
			t.Fatalf("firstPathSegment(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tc.path, segment, remainder, ok, tc.wantSegment, tc.wantRemainder, tc.wantOK)
		}
	}
}

func TestLooksLikeEFSFileSystemID(t *testing.T) {
	if !looksLikeEFSFileSystemID("fs-0a1b2c3d") {
		t.Fatalf("looksLikeEFSFileSystemID(fs-...) = false, want true")
	}
	if looksLikeEFSFileSystemID("landing-bucket") {
		t.Fatalf("looksLikeEFSFileSystemID(bucket) = true, want false")
	}
}

func TestUserResourceIDFallsBackToServerUserComposite(t *testing.T) {
	if got := userResourceID(User{ARN: "arn:aws:transfer:::user/s-1/u"}); got != "arn:aws:transfer:::user/s-1/u" {
		t.Fatalf("userResourceID(arn) = %q, want the ARN", got)
	}
	if got := userResourceID(User{ServerID: "s-1", UserName: "u"}); got != "s-1/u" {
		t.Fatalf("userResourceID(no arn) = %q, want %q", got, "s-1/u")
	}
	if got := userResourceID(User{}); got != "" {
		t.Fatalf("userResourceID(empty) = %q, want empty", got)
	}
}
