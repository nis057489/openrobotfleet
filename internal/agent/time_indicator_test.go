package agent

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestIndicatorFilesInstallWithModes(t *testing.T) {
	// Every embedded file has an install target, and every target a file.
	embedded := map[string]bool{}
	fs.WalkDir(indicatorFiles, "indicator", func(name string, d fs.DirEntry, err error) error {
		if !d.IsDir() {
			embedded[filepath.Base(name)] = true
		}
		return nil
	})
	for name := range indicatorInstall {
		if !embedded[name] {
			t.Errorf("no embedded file for %s", name)
		}
	}
	for name := range embedded {
		if _, ok := indicatorInstall[name]; !ok {
			t.Errorf("embedded %s has no install target", name)
		}
	}

	root := t.TempDir()
	for i := 0; i < 2; i++ { // second run finds everything current
		if err := installIndicatorFiles(root); err != nil {
			t.Fatal(err)
		}
	}
	for name, target := range indicatorInstall {
		info, err := os.Stat(filepath.Join(root, target.path))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if info.Mode().Perm() != target.mode {
			t.Errorf("%s mode %v, want %v", target.path, info.Mode().Perm(), target.mode)
		}
	}
}
