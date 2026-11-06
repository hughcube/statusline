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
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/hughcube/statusline/internal/app"
	"github.com/hughcube/statusline/internal/gitstats"
)

// version 由构建时注入:-ldflags "-X main.version=1.2.3"
var version = "dev"

func main() {
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
		fmt.Println(version)
		return
	case "--help", "-h", "help":
		usage()
		return
	}
	usage()
	os.Exit(2)
}

func usage() {
	fmt.Fprint(os.Stderr, "statusline —— agent 状态栏 / 终端标题\n\n"+
		"  statusline bar   [--agent NAME] [--width N] [--debug]\n"+
		"  statusline title [--agent NAME] [--debug]\n"+
		"  statusline --version\n\n"+
		"agent: auto(默认) | codebuddy | antigravity | claude | generic\n"+
		"payload 从 stdin 读取。\n")
}

func runBar(args []string) int {
	fs := flag.NewFlagSet("bar", flag.ContinueOnError)
	name := fs.String("agent", "auto", "agent 名称或 auto")
	width := fs.Int("width", 0, "覆盖终端列数(调试用)")
	debug := fs.Bool("debug", false, "把解析结果打印到 stderr")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	raw, _ := io.ReadAll(os.Stdin)
	res := app.Bar(raw, *name, *width)
	if *debug {
		dumpDebug(res.Agent, res.ProbeW, string(res.WidthFrom), res.Fields, res.Git)
	}
	fmt.Println(res.Output)
	return 0
}

func runTitle(args []string) int {
	fs := flag.NewFlagSet("title", flag.ContinueOnError)
	name := fs.String("agent", "auto", "agent 名称或 auto")
	debug := fs.Bool("debug", false, "把解析结果打印到 stderr")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	raw, _ := io.ReadAll(os.Stdin)
	out, fields := app.Title(raw, *name)
	if *debug {
		dumpDebug(*name, 0, "", fields, nil)
	}
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
