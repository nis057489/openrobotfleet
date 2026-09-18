package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
func TestDeploymentRejectsUnsafePaths(t *testing.T) {
	base := t.TempDir()
	workspace := filepath.Join(base, "workspace")
	victim := filepath.Join(base, "outside", "keep")
	writeTestFile(t, victim, "precious")
	os.MkdirAll(workspace, 0755)
	if err := os.Symlink(filepath.Dir(victim), filepath.Join(workspace, "link")); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/", filepath.Dir(victim), "../outside", ".", "link", "link/child"} {
		t.Run(path, func(t *testing.T) {
			if err := HandleUpdateRepo(Config{WorkspacePath: workspace}, UpdateRepoData{Repo: "unused", Path: path}); err == nil {
				t.Fatal("unsafe target accepted")
			}
			data, err := os.ReadFile(victim)
			if err != nil || string(data) != "precious" {
				t.Fatal("outside file damaged")
			}
		})
	}
}
func TestFailedClonePreservesDeployment(t *testing.T) {
	base := t.TempDir()
	bin := filepath.Join(base, "bin")
	os.Mkdir(bin, 0755)
	os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\nexit 1\n"), 0755)
	t.Setenv("PATH", bin)
	workspace := filepath.Join(base, "workspace")
	existing := filepath.Join(workspace, "course", "code")
	writeTestFile(t, existing, "working")
	if err := HandleUpdateRepo(Config{WorkspacePath: workspace}, UpdateRepoData{Repo: "unavailable", Path: "course"}); err == nil {
		t.Fatal("expected clone failure")
	}
	data, err := os.ReadFile(existing)
	if err != nil || string(data) != "working" {
		t.Fatal("working deployment lost")
	}
}
func TestSuccessfulCloneReplacesDeployment(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	base := t.TempDir()
	source := filepath.Join(base, "source")
	os.Mkdir(source, 0755)
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = source
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git: %v %s", err, out)
		}
	}
	git("init", "-b", "main")
	writeTestFile(t, filepath.Join(source, "new"), "new code")
	git("add", "new")
	git("-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-m", "initial")
	workspace := filepath.Join(base, "workspace")
	writeTestFile(t, filepath.Join(workspace, "course", "old"), "old code")
	if err := HandleUpdateRepo(Config{WorkspacePath: workspace}, UpdateRepoData{Repo: source, Path: "course"}); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(workspace, "course", "new")); err != nil || string(data) != "new code" {
		t.Fatal("new code missing")
	}
	if _, err := os.Stat(filepath.Join(workspace, "course", "old")); !os.IsNotExist(err) {
		t.Fatal("old deployment still present")
	}
}
