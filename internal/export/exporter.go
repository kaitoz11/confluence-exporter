package export

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/kaitoz11/confluence-exporter/internal/confluence"
	"github.com/kaitoz11/confluence-exporter/internal/pdf"
)

// Options control an export.
type Options struct {
	// OutputDir is the parent folder; the space gets its own folder inside.
	OutputDir   string
	Attachments bool
	PDF         bool
	Speed       Speed
}

// Phase names shown to the user.
const (
	PhasePages = "Downloading pages"
	PhasePDF   = "Creating PDF files"
)

// Event reports progress to the UI.
type Event struct {
	Phase   string
	Done    int
	Total   int
	Message string // what just happened, for the activity log
	Warning string // non-fatal problem, for the activity log and summary
}

// Result summarizes a finished export.
type Result struct {
	Root            string // absolute path of the space folder
	Pages           int
	Attachments     int
	AttachmentBytes int64
	PDFs            int
	Warnings        []string
	Canceled        bool
	Duration        time.Duration
}

// SpaceFolder returns where a space is exported inside outputDir.
func SpaceFolder(outputDir string, space confluence.Space) string {
	name := SanitizeName(space.Name)
	if space.Key != "" && !strings.EqualFold(space.Name, space.Key) {
		name = SanitizeName(space.Name + " (" + space.Key + ")")
	}
	return filepath.Join(outputDir, name)
}

// Run exports the selected nodes of the tree. It returns when everything is
// done or ctx is canceled; emit is called from several goroutines.
func Run(ctx context.Context, client confluence.Client, tree *Tree, opts Options, emit func(Event)) Result {
	start := time.Now()
	root, _ := filepath.Abs(SpaceFolder(opts.OutputDir, tree.Space))
	res := Result{Root: root}
	var mu sync.Mutex
	warn := func(msg string) {
		mu.Lock()
		res.Warnings = append(res.Warnings, msg)
		mu.Unlock()
		emit(Event{Warning: msg})
	}

	if err := os.MkdirAll(root, 0o755); err != nil {
		warn(fmt.Sprintf("Cannot create the output folder: %v", err))
		res.Duration = time.Since(start)
		return res
	}

	nodes := tree.Selected()
	th := opts.Speed.throttle()
	workers := th.downloads
	client.SetRateLimit(th.requestRate)

	var (
		wg      sync.WaitGroup
		done    int
		mdFiles []string
		jobs    = make(chan *Node)
	)
	emit(Event{Phase: PhasePages, Total: len(nodes)})
	for i := 0; i < workers; i++ {
		wg.Go(func() {
			for n := range jobs {
				r := exportNode(ctx, client, tree, n, root, opts, warn)
				mu.Lock()
				done++
				if r.written {
					res.Pages++
					mdFiles = append(mdFiles, filepath.Join(root, filepath.FromSlash(n.MarkdownPath())))
				}
				res.Attachments += r.attachments
				res.AttachmentBytes += r.bytes
				d := done
				mu.Unlock()
				msg := "✓ " + n.Title
				if n.IsFolder() {
					msg = "▸ " + n.Title + " (folder)"
				} else if r.attachments > 0 {
					msg += fmt.Sprintf(" (+%d files)", r.attachments)
				}
				emit(Event{Phase: PhasePages, Done: d, Total: len(nodes), Message: msg})
			}
		})
	}
feed:
	for _, n := range nodes {
		select {
		case jobs <- n:
		case <-ctx.Done():
			break feed
		}
	}
	close(jobs)
	wg.Wait()

	if ctx.Err() == nil {
		if err := writeTOC(tree, root); err != nil {
			warn(fmt.Sprintf("Table of contents: %v", err))
		}
	}

	if opts.PDF && ctx.Err() == nil && len(mdFiles) > 0 {
		emit(Event{Phase: PhasePDF, Total: len(mdFiles)})
		res.PDFs = convertPDFs(ctx, root, mdFiles, opts.Speed, emit, warn)
	}

	res.Canceled = ctx.Err() != nil
	res.Duration = time.Since(start)
	return res
}

// ConvertFolderToPDF converts every Markdown file under root. It is used both
// after an export and on its own from the main menu.
func ConvertFolderToPDF(ctx context.Context, root string, speed Speed, emit func(Event)) Result {
	start := time.Now()
	res := Result{Root: root}
	warn := func(msg string) {
		res.Warnings = append(res.Warnings, msg)
		emit(Event{Warning: msg})
	}
	files, err := pdf.FindMarkdown(root)
	switch {
	case err != nil:
		warn(fmt.Sprintf("Cannot read the folder: %v", err))
	case len(files) == 0:
		warn("No Markdown (.md) files were found in this folder.")
	default:
		emit(Event{Phase: PhasePDF, Total: len(files), Message: fmt.Sprintf("Found %d Markdown files", len(files))})
		res.PDFs = convertPDFs(ctx, root, files, speed, emit, warn)
	}
	res.Canceled = ctx.Err() != nil
	res.Duration = time.Since(start)
	return res
}

func convertPDFs(ctx context.Context, root string, files []string, speed Speed, emit func(Event), warn func(string)) int {
	th := speed.throttle()
	conv := pdf.New(pdf.Options{Workers: th.pdfWorkers, Pause: th.pdfPause})
	n, _ := conv.ConvertAll(ctx, files, func(p pdf.Progress) {
		msg := "✓ " + filepath.Base(pdf.PDFPath(p.File))
		if p.Err != nil {
			msg = ""
			rel, _ := filepath.Rel(root, p.File)
			if !errors.Is(p.Err, context.Canceled) {
				warn(fmt.Sprintf("PDF for %s: %v", rel, p.Err))
			}
		}
		emit(Event{Phase: PhasePDF, Done: p.Done, Total: p.Total, Message: msg})
	})
	return n
}

type nodeResult struct {
	written     bool
	attachments int
	bytes       int64
}

func exportNode(ctx context.Context, client confluence.Client, tree *Tree, n *Node, root string, opts Options, warn func(string)) nodeResult {
	var r nodeResult
	dir := filepath.Join(root, filepath.FromSlash(n.Dir))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		warn(fmt.Sprintf("%s: cannot create folder: %v", n.Title, err))
		return r
	}
	if n.IsFolder() {
		return r
	}

	pc := pageContext{tree: tree, node: n, siteURL: client.SiteURL()}
	var attachments []confluence.Attachment
	if opts.Attachments {
		pc.files = map[string]string{}
		list, err := client.ListAttachments(ctx, n.ID)
		if err != nil {
			if ctx.Err() != nil {
				return r
			}
			warn(fmt.Sprintf("%s: cannot list attachments: %v", n.Title, err))
		}
		used := map[string]bool{}
		for _, a := range list {
			saved := UniqueName(SanitizeFileName(a.Title), used)
			size, err := downloadAttachment(ctx, client, a, filepath.Join(dir, AttachmentsDir, saved))
			if err != nil {
				if ctx.Err() != nil {
					return r
				}
				warn(fmt.Sprintf("%s: attachment %q: %v", n.Title, a.Title, err))
				continue
			}
			pc.files[a.Title] = saved
			attachments = append(attachments, confluence.Attachment{Title: saved, MediaType: a.MediaType, Size: size})
			r.attachments++
			r.bytes += size
		}
	}

	body, err := client.PageHTML(ctx, n.ID)
	if err != nil {
		if ctx.Err() == nil {
			warn(fmt.Sprintf("%s: cannot download page: %v", n.Title, err))
		}
		return r
	}
	md, err := htmlToMarkdown(body, pc)
	if err != nil {
		warn(fmt.Sprintf("%s: conversion problem: %v", n.Title, err))
	}

	var b strings.Builder
	b.WriteString("# " + n.Title + "\n\n")
	b.WriteString("*Exported from Confluence — [view the original page](" + pageURL(client, tree.Space, n.ID) + ")*\n\n")
	if md != "" {
		b.WriteString(md + "\n")
	}
	if len(attachments) > 0 {
		b.WriteString("\n## Attachments\n\n")
		for _, a := range attachments {
			fmt.Fprintf(&b, "- [%s](%s/%s) (%s)\n", a.Title, AttachmentsDir, url.PathEscape(a.Title), HumanBytes(a.Size))
		}
	}
	mdPath := filepath.Join(root, filepath.FromSlash(n.MarkdownPath()))
	if err := os.WriteFile(mdPath, []byte(b.String()), 0o644); err != nil {
		warn(fmt.Sprintf("%s: cannot save file: %v", n.Title, err))
		return r
	}
	r.written = true
	return r
}

// downloadAttachment saves a file, skipping it when an identical-size copy
// is already there so an interrupted export can simply be run again.
func downloadAttachment(ctx context.Context, client confluence.Client, a confluence.Attachment, dest string) (int64, error) {
	if fi, err := os.Stat(dest); err == nil && a.Size > 0 && fi.Size() == a.Size {
		return fi.Size(), nil
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return 0, err
	}
	tmp := dest + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return 0, err
	}
	err = client.Download(ctx, a.DownloadURL, f)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return 0, err
	}
	if err := os.Rename(tmp, dest); err != nil {
		return 0, err
	}
	fi, err := os.Stat(dest)
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

func pageURL(client confluence.Client, space confluence.Space, id string) string {
	if client.IsCloud() {
		return fmt.Sprintf("%s/spaces/%s/pages/%s", client.SiteURL(), url.PathEscape(space.Key), id)
	}
	return fmt.Sprintf("%s/pages/viewpage.action?pageId=%s", client.SiteURL(), id)
}

// writeTOC writes a clickable outline of the exported pages at the root.
func writeTOC(tree *Tree, root string) error {
	var b strings.Builder
	b.WriteString("# " + tree.Space.Name + "\n\n")
	b.WriteString("Table of contents of the exported Confluence space.\n\n")
	tree.Walk(func(n *Node) bool {
		indent := strings.Repeat("  ", n.Depth)
		switch {
		case n.IsFolder():
			b.WriteString(indent + "- **" + n.Title + "**\n")
		case n.Selected:
			link := escapePath(n.MarkdownPath())
			b.WriteString(indent + "- [" + strings.ReplaceAll(n.Title, "]", "\\]") + "](" + link + ")\n")
		default:
			b.WriteString(indent + "- " + n.Title + " *(not exported)*\n")
		}
		return true
	})
	return os.WriteFile(filepath.Join(root, TOCFile), []byte(b.String()), 0o644)
}

func escapePath(p string) string {
	parts := strings.Split(p, "/")
	for i, s := range parts {
		parts[i] = url.PathEscape(s)
	}
	return strings.Join(parts, "/")
}

// HumanBytes formats a size like "1.4 MB".
func HumanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}
