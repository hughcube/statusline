// Package diag implements the out-of-band capture used when a status line has
// to be debugged in production.
//
// The release build contains none of this: capture lives behind the "diag"
// build tag, so shipping a diagnostic binary means compiling one, swapping it
// into place, observing, and putting the release binary back.
//
//	release : go build ./cmd/statusline
//	diagnose: go build -tags diag ./cmd/statusline
package diag

// Info is everything a captured frame needs. The disabled build ignores it.
type Info struct {
	Kind       string // bar | title
	Agent      string
	Width      int
	WidthSrc   string
	DurationMS int64
	Payload    []byte
	Output     string
	Extra      any
}

// Dir is where a diag build writes its artifacts.
const DirName = "diag"
