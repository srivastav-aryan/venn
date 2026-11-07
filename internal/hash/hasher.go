package hash

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

type fileInfo struct {
	path        string
	isExec      bool
	contentHash string
}

func FileHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}

	defer f.Close()
	h := sha256.New()

	_, errC := io.Copy(h, f)

	if errC != nil {
		return "", errC
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

func HashDirectoryTree(root string) (string, error) {
	var entries []fileInfo

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, Entryerr error) error {
		if Entryerr != nil {
			return Entryerr
		}
		if d.IsDir() {
			return nil
		}

		if !d.Type().IsRegular() { // symlink check edge  cases needed
			return nil
		}

		rel, err := filepath.Rel(root, path) // 3. relative path
		if err != nil {
			return err
		}

		rel = filepath.ToSlash(rel)

		info, err := d.Info()
		if err != nil {
			return err
		}
		isExec := info.Mode()&0o111 != 0

		hash, hashErr := FileHash(path)

		if hashErr != nil {
			return hashErr
		}

		entries = append(entries, fileInfo{path: rel, isExec: isExec, contentHash: hash})

		return nil
	})
	if err != nil {
		return "", err
	}

	sort.Slice(entries, func(i int, j int) bool {
		return entries[i].path < entries[j].path
	})

	mainHasher := sha256.New()

	for _, e := range entries {
		mainHasher.Write([]byte(e.path))
		mainHasher.Write([]byte{0xFF}) // ← end of path marker

		mainHasher.Write([]byte(e.contentHash))
		mainHasher.Write([]byte{0xFF}) // ← end of contentHash marker

		if e.isExec {
			mainHasher.Write([]byte{1})
		} else {
			mainHasher.Write([]byte{0})
		}
		mainHasher.Write([]byte{0xFF}) // ← end of entry marker
	}

	return hex.EncodeToString(mainHasher.Sum(nil)), nil
}
