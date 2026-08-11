package imageprepare

import (
	"context"
	"testing"
)

func TestPrepareRejectsUntrustedPackageAndRevision(t *testing.T) {
	script := &Script{path: "must-not-run"}
	tests := []struct {
		name, packageName, revision string
	}{
		{"URL", "https://example.invalid/image.zip", "1"},
		{"shell fragment", "system-images;android-36;google_apis;x86_64;touch /tmp/pwned", "1"},
		{"unsupported type", "system-images;android-36;default;x86_64", "1"},
		{"revision fragment", "system-images;android-36;google_apis;x86_64", "1;curl"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := script.Prepare(context.Background(), test.packageName, test.revision); err == nil {
				t.Fatal("expected fixed Android SDK selector validation to reject input")
			}
		})
	}
}

func TestDecodeResultUsesLastStructuredResult(t *testing.T) {
	result, err := decodeResult([]byte("progress\nDEVICE_FARM_IMAGE_RESULT={\"entries\":[]}\nDEVICE_FARM_IMAGE_RESULT={\"docker_digest\":\"sha256:abc\",\"image_disk_mb\":8192}\n"))
	if err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if result["docker_digest"] != "sha256:abc" || result["image_disk_mb"] != float64(8192) {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestDecodeResultRejectsMissingOrMalformedResult(t *testing.T) {
	for _, output := range []string{"ordinary logs only", "DEVICE_FARM_IMAGE_RESULT={not-json}"} {
		if _, err := decodeResult([]byte(output)); err == nil {
			t.Fatalf("expected %q to fail", output)
		}
	}
}
