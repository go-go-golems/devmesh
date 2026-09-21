package transport

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListenRefusesRegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devmesh.sock")
	if err := os.WriteFile(path, []byte("valuable file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Listen(path); err == nil {
		t.Fatal("Listen replaced a regular file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "valuable file" {
		t.Fatalf("regular file changed to %q", data)
	}
}

func TestListenRefusesSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("target"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "devmesh.sock")
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if _, err := Listen(path); err == nil {
		t.Fatal("Listen replaced a symlink")
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatal(err)
	}
}
