package protocol

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// Exercise the two host import paths against every published artifact, including
// payment packages that the declarative-provider catalog assertions skip.
func TestPublishedPackagesParseAndInspect(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "..", "plugin-packages", "*.yingce-plugin"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no published packages found")
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := ParsePluginPackage(data)
			if err != nil {
				t.Fatal(err)
			}
			inspected, err := InspectPluginPackage(data)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(parsed.ManifestRaw, inspected.ManifestRaw) || parsed.Manifest.Metadata.ID != inspected.Manifest.Metadata.ID {
				t.Fatal("parse and inspect disagree on the imported manifest")
			}
		})
	}
	t.Logf("host Parse/Inspect checked %d published archives", len(paths))
}
