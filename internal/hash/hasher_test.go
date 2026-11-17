package hash

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHashfunc_KnownContent(t *testing.T) {
	tmpDir := t.TempDir()

	path := filepath.Join(tmpDir, "test.txt")

	content := []byte("hello world\n")

	err := os.WriteFile(path, content, 0o644)
	if err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	gotHash, err := FileHash(path)
	if err != nil {
		t.Fatalf("FileHash returned unexpected error: %v", err)
	}

	wantHash := "a948904f2f0f479b8f8197694b30184b0d2ed1c1cd2a1ec0fb85d299a192a447"
	if gotHash != wantHash {
		t.Fatalf("hash mismatch: got %s, want %s", gotHash, wantHash)
	}
}

func TestFileHash_Determinism(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "test.txt")
	_ = os.WriteFile(filePath, []byte("deterministic content"), 0o644)

	hash1, err1 := FileHash(filePath)
	if err1 != nil {
		t.Fatalf("first hash failed: %v", err1)
	}

	hash2, err2 := FileHash(filePath)
	if err2 != nil {
		t.Fatalf("second hash failed: %v", err2)
	}

	if hash1 != hash2 {
		t.Fatalf("expected hashes to match, got %s vs %s", hash1, hash2)
	}
}

func TestFileHash_Sensitivity(t *testing.T) {
	tempDir := t.TempDir()

	file1 := filepath.Join(tempDir, "file1.txt")
	file2 := filepath.Join(tempDir, "file2.txt")

	_ = os.WriteFile(file1, []byte("hello"), 0o644)
	_ = os.WriteFile(file2, []byte("hellO"), 0o644) // Capital 'O'

	hash1, _ := FileHash(file1)
	hash2, _ := FileHash(file2)

	if hash1 == hash2 {
		t.Fatalf("expected different hashes for different content, but both were: %s", hash1)
	}
}
