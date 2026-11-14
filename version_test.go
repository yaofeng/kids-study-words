package studywords

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersionMatchesDotVersion(t *testing.T) {
	data, err := os.ReadFile(".version")
	if err != nil {
		t.Fatalf("read .version: %v", err)
	}
	want := strings.TrimSpace(string(data))
	if want == "" {
		t.Fatal(".version is empty")
	}
	if Version != want {
		t.Fatalf("Version = %q, want %q from .version", Version, want)
	}
}

// The release number belongs in .version only; anything else that needs it
// reads Version or the file, so the two can never disagree.
func TestVersionLiteralOnlyInDotVersion(t *testing.T) {
	if Version == "" {
		t.Fatal("Version is empty")
	}
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	skipDirs := map[string]bool{
		".git": true, ".superpowers": true, "node_modules": true,
		"data": true, "data-src": true, "dist": true,
	}
	skipExts := map[string]bool{
		".png": true, ".jpg": true, ".woff2": true, ".db": true, ".sqlite": true,
	}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		if d.Name() == ".version" || skipExts[filepath.Ext(path)] {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() > 1<<20 {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if strings.Contains(string(data), Version) {
			rel, _ := filepath.Rel(root, path)
			t.Errorf("%s hardcodes the version %q; read it from .version instead", rel, Version)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
