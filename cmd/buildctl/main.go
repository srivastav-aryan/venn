package main

import (
	"fmt"
	"os"

	"github.com/aryansrivastava/venn/internal/hash"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: buildctl <directory> not provided as a argument")
		os.Exit(1)
	}

	root := os.Args[1]
	hash, err := hash.HashDirectoryTree(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	fmt.Println(hash)
}
