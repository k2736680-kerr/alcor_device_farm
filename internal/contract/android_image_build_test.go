package contract

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestAndroidImagePrepareAndBuildUseSameTag(t *testing.T) {
	const want = `tag="${android_version}-api${api_level}-${image_type}-${abi}-sdk${revision}"`
	for _, name := range []string{"prepare.sh", "build.sh"} {
		path := filepath.Join("..", "..", "deploy", "docker-emulator", "images", name)
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(want) + `$`).Match(content) {
			t.Fatalf("%s must use the Build Agent image tag %q", name, want)
		}
	}
}
