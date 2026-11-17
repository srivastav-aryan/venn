package cas

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestCAS_PutAndGet(t *testing.T) {
	fakeBaseDir := t.TempDir()

	store, er := New(fakeBaseDir)
	if er != nil {
		t.Fatalf("failed to create store: %v", er)
	}

	hash := "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
	data := []byte("build output")

	err := store.Put(hash, bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Put operation failed %v", err)
	}

	path := filepath.Join(store.baseDir, hash[:2], hash)
	_, err2 := os.Stat(path)

	if err2 != nil {
		t.Fatalf("Put operation failed %v", err2)
	}

	f, err3 := store.Get(hash)

	defer f.Close()
	if err3 != nil {
		t.Fatalf("Get operation failed %v", err3)
	}

	gotData, err4 := io.ReadAll(f)

	if err4 != nil {
		t.Fatalf("failed to read from Get stream: %v", err)
	}

	if !bytes.Equal(data, gotData) {
		t.Fatalf("content mismatch: got %q, want %q", gotData, data)
	}
}
