package imagecatalog

import "testing"

func TestValidEntryAcceptsOnlyOfficialSystemImageSelectors(t *testing.T) {
	if !validEntry("system-images;android-36;google_apis;x86_64", 36, "google_apis", "x86_64", "16") {
		t.Fatal("expected stable official selector to be valid")
	}
	invalid := []struct {
		packageName string
		api         int
		imageType   string
		abi         string
		revision    string
	}{
		{"https://example.invalid/system.img", 36, "google_apis", "x86_64", "16"},
		{"system-images;android-36;default;x86_64", 36, "default", "x86_64", "16"},
		{"system-images;android-36;google_apis;armeabi-v7a", 36, "google_apis", "armeabi-v7a", "16"},
		{"system-images;android-36;google_apis;x86_64", 36, "google_apis", "x86_64", "16;curl"},
	}
	for _, item := range invalid {
		if validEntry(item.packageName, item.api, item.imageType, item.abi, item.revision) {
			t.Fatalf("unexpected valid entry: %#v", item)
		}
	}
}

func TestValidDigestRequiresCanonicalLowercaseSHA256(t *testing.T) {
	valid := "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if !validDigest(valid) {
		t.Fatal("canonical digest rejected")
	}
	for _, value := range []string{"sha256:abc", "sha256:0123456789ABCDEF0123456789abcdef0123456789abcdef0123456789abcdef", "sha512:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"} {
		if validDigest(value) {
			t.Fatalf("invalid digest accepted: %s", value)
		}
	}
}

func TestCatalogStatusPrecedence(t *testing.T) {
	tests := []struct {
		preparation, image string
		changed            bool
		want               string
	}{
		{"", "", false, "downloadable"},
		{"queued", "", false, "preparing"},
		{"validating", "", false, "validating"},
		{"cached", "ready", false, "cached"},
		{"cached", "ready", true, "official_updated"},
		{"failed", "", false, "failed"},
	}
	for _, test := range tests {
		if got := catalogStatus(test.preparation, test.image, test.changed); got != test.want {
			t.Fatalf("catalogStatus(%q,%q,%t)=%q want %q", test.preparation, test.image, test.changed, got, test.want)
		}
	}
}
