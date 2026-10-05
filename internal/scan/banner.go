package scan

import (
	"fmt"
	"io"
	"strings"
)

// bannerArt is the side-eye wordmark, drawn with Unicode box characters.
var bannerArt = []string{
	"\n",
	" ▗▄▄▖▄    ▐▌▗▞▀▚▖    ▗▄▄▄▖▄   ▄ ▗▞▀▚▖",
	"▐▌   ▄    ▐▌▐▛▀▀▘    ▐▌   █   █ ▐▛▀▀▘",
	" ▝▀▚▖█ ▗▞▀▜▌▝▚▄▄▖    ▐▛▀▀▘ ▀▀▀█ ▝▚▄▄▖",
	"▗▄▄▞▘█ ▝▚▄▟▌         ▐▙▄▄▖▄   █",
	"                           ▀▀▀",
	"\n",
}

// printBanner writes the wordmark and a blank line before the scan output. It
// writes to stderr so the report on stdout, and the JSON output, stay clean.
func printBanner(w io.Writer) {
	for _, line := range bannerArt {
		if strings.TrimSpace(line) == "" {
			fmt.Fprintln(w)
			continue
		}
		fmt.Fprintln(w, yellow(line))
	}
	fmt.Fprintln(w)
}
