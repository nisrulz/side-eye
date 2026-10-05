// Command side-eye scans a git repository for code that runs on checkout,
// commit, or open.
package main

import (
	"os"

	"side-eye/internal/scan"
)

func main() {
	os.Exit(scan.Run(os.Args[1:], os.Stdout, os.Stderr))
}
