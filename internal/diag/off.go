//go:build !diag

package diag

// Enabled reports whether this build captures diagnostics.
func Enabled() bool { return false }

// Capture is a no-op in release builds; the compiler inlines it away.
func Capture(Info) {}

// SetVersion is a no-op in release builds.
func SetVersion(string) {}
