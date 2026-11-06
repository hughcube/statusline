// Command statusline is the single entry point for the agent status bar and
// terminal title.
//
// Usage:
//
//	statusline bar   [--agent codebuddy|antigravity|claude|generic|auto] [--width N] [--debug]
//	statusline title [--agent ...] [--debug]
//	statusline --version
//
// Both subcommands read the host payload from stdin.
//
// Debugging: build with "-tags diag" to get a binary that also writes every
// frame to <cache>/statusline/diag/. See AGENTS.md for the full procedure.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/hughcube/statusline/internal/app"
	"github.com/hughcube/statusline/internal/diag"
	"github.com/hughcube/statusline/internal/gitstats"
)

// version is injected at build time: -ldflags "-X main.version=1.2.3"
var version = "dev"

func main() {
	diag.SetVersion(version)
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "bar":
		os.Exit(runBar(os.Args[2:]))
	case "title":
		os.Exit(runTitle(os.Args[2:]))
	case "__gitcache":
		if len(os.Args) < 4 {
			os.Exit(2)
		}
		gitstats.CacheWorker(os.Args[2], os.Args[3])
		return
	case "--version", "-v", "version":
		fmt.Println(versionString())
		return
	case "--help", "-h", "help":
		usage()
		return
	}
	usage()
	os.Exit(2)
}

// versionString marks diagnostic builds so the deployed binary is never
// ambiguous during a debugging session.
func versionString() string {
	if diag.Enabled() {
		return version + "+diag"
	}
	return version
}

func usage() {
	fmt.Fprint(os.Stderr, "statusline — agent status bar / terminal title\n\n"+
		"  statusline bar   [--agent NAME] [--width N] [--debug]\n"+
		"  statusline title [--agent NAME] [--debug]\n"+
		"  statusline --version\n\n"+
		"agent: auto(default) | codebuddy | antigravity | claude | generic\n"+
		"payload is read from stdin.\n")
}

func runBar(args []string) int {
	fs := flag.NewFlagSet("bar", flag.ContinueOnError)
	name := fs.String("agent", "auto", "agent name or auto")
	width := fs.Int("width", 0, "override the terminal width (debugging)")
	verbose := fs.Bool("debug", false, "dump the parsed payload to stderr")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	raw, _ := io.ReadAll(os.Stdin)
	start := time.Now()
	res := app.Bar(raw, *name, *width)
	if *verbose {
		dumpDebug(res.Agent, res.ProbeW, string(res.WidthFrom), res.Fields, res.Git)
	}
	diag.Capture(diag.Info{
		Kind:       "bar",
		Agent:      res.Agent,
		Width:      res.ProbeW,
		WidthSrc:   string(res.WidthFrom),
		DurationMS: time.Since(start).Milliseconds(),
		Payload:    raw,
		Output:     res.Output,
		Extra:      res.Git,
	})
	fmt.Println(res.Output)
	return 0
}

func runTitle(args []string) int {
	fs := flag.NewFlagSet("title", flag.ContinueOnError)
	name := fs.String("agent", "auto", "agent name or auto")
	verbose := fs.Bool("debug", false, "dump the parsed payload to stderr")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	raw, _ := io.ReadAll(os.Stdin)
	start := time.Now()
	out, resolved, fields := app.Title(raw, *name)
	if *verbose {
		dumpDebug(resolved, 0, "", fields, nil)
	}
	diag.Capture(diag.Info{
		Kind:       "title",
		Agent:      resolved,
		DurationMS: time.Since(start).Milliseconds(),
		Payload:    raw,
		Output:     out,
	})
	fmt.Println(out)
	return 0
}

func dumpDebug(agentName string, probeW int, src string, f any, g any) {
	b, _ := json.MarshalIndent(map[string]any{
		"agent":     agentName,
		"probe_w":   probeW,
		"width_src": src,
		"fields":    f,
		"git":       g,
	}, "", "  ")
	fmt.Fprintln(os.Stderr, string(b))
}
