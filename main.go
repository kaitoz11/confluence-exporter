// Command confluence-exporter exports a whole Confluence space to Markdown
// (with attachments and optional PDFs) through a friendly terminal UI.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kaitoz11/confluence-exporter/internal/config"
	"github.com/kaitoz11/confluence-exporter/internal/export"
	"github.com/kaitoz11/confluence-exporter/internal/tui"
)

var version = "dev"

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `Confluence Space Exporter %s

Run without arguments to start the interactive app.

Usage:
  confluence-exporter              start the interactive app
  confluence-exporter [--speed gentle|normal|fast] pdf <folder>
                                   convert every .md file under <folder> to PDF
  confluence-exporter html <folder>
                                   build a browsable offline website (index.html) for <folder>
  confluence-exporter --version    print the version

Environment (optional, pre-fills the sign-in form):
  CONFLUENCE_URL, CONFLUENCE_EMAIL, CONFLUENCE_API_TOKEN
`, version)
	}
	showVersion := flag.Bool("version", false, "print the version")
	speed := flag.String("speed", "normal", "PDF speed for the pdf command: gentle, normal or fast")
	flag.Parse()

	// A soft memory ceiling: the garbage collector works harder before the
	// app grows past it, so big exports stay light on the computer.
	debug.SetMemoryLimit(512 << 20)

	switch {
	case *showVersion:
		fmt.Println(version)
		return
	case flag.NArg() == 2 && flag.Arg(0) == "pdf":
		os.Exit(runPDF(config.ExpandPath(flag.Arg(1)), export.ParseSpeed(capitalize(*speed))))
	case flag.NArg() == 2 && flag.Arg(0) == "html":
		os.Exit(runSite(config.ExpandPath(flag.Arg(1))))
	case flag.NArg() > 0:
		flag.Usage()
		os.Exit(2)
	}

	if path := os.Getenv("CONFLUENCE_EXPORTER_DEBUG"); path != "" {
		if f, err := tea.LogToFile(path, "debug"); err == nil {
			defer f.Close()
		}
	}
	if _, err := tea.NewProgram(tui.New(version), tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

// runPDF is the non-interactive PDF conversion, for scripts.
func runPDF(dir string, speed export.Speed) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	res := export.ConvertFolderToPDF(ctx, dir, export.NewThrottle(speed), func(e export.Event) {
		switch {
		case e.Warning != "":
			fmt.Fprintln(os.Stderr, "warning:", e.Warning)
		case e.Message != "" && e.Total > 0:
			fmt.Printf("[%d/%d] %s\n", e.Done, e.Total, e.Message)
		}
	})
	fmt.Printf("Created %d PDF files in %s\n", res.PDFs, res.Duration.Round(1e9))
	if len(res.Warnings) > 0 || res.Canceled {
		return 1
	}
	return 0
}

// runSite is the non-interactive website build, for scripts.
func runSite(dir string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	res := export.BuildFolderSite(ctx, dir, func(e export.Event) {
		if e.Warning != "" {
			fmt.Fprintln(os.Stderr, "warning:", e.Warning)
		}
	})
	if res.Website == "" {
		return 1
	}
	fmt.Printf("Website ready: %s\n", res.Website)
	if len(res.Warnings) > 0 || res.Canceled {
		return 1
	}
	return 0
}

func capitalize(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
