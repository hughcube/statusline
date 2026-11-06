// Package diag implements the out-of-band capture used when a status line has
// to be debugged in production.
//
// The release build contains none of this: capture lives behind the "diag"
// build tag, so shipping a diagnostic binary means compiling one, swapping it
// into place, observing, and putting the release binary back.
//
//	release : go build ./cmd/statusline
//	diagnose: go build -tags diag ./cmd/statusline
//
// Artifacts are written per agent+session so several CLIs (or several windows
// of the same CLI) never overwrite each other:
//
//	<cache>/statusline/diag/<agent>-<session>/latest.json
//	<cache>/statusline/diag/<agent>-<session>/frames/NNNNN.json
//	<cache>/statusline/diag/latest.json   (last frame overall)
package diag

// Info is everything a captured frame needs. The disabled build ignores it.
type Info struct {
	Kind       string // bar | title
	Agent      string
	Session    string // session id when the payload carries one
	Width      int
	WidthSrc   string
	DurationMS int64
	Payload    []byte
	Output     string
	Extra      any
}

// DirName is the cache sub-directory that holds captures.
const DirName = "diag"
