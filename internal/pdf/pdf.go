// Package pdf converts Markdown files to PDF in pure Go: the Markdown is
// parsed with goldmark and laid out directly with fpdf, using embedded
// Unicode fonts and the images found next to the file. No browser or other
// external program is needed, and memory use stays small.
package pdf

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"
)

// Options throttle a batch conversion so it never overloads the computer.
type Options struct {
	// Workers is how many files are converted at the same time.
	Workers int
	// Pause is a rest taken by each worker between two files.
	Pause time.Duration
	// Throttle, when set, can change both of the above while a batch runs:
	// Workers is then the most that can ever run, and each file waits for
	// a free slot.
	Throttle Throttle
}

// Throttle is a live limit on how many files are converted at once.
type Throttle interface {
	Acquire(ctx context.Context) error
	Release()
	Pause() time.Duration
}

// Converter turns Markdown files into PDF files.
type Converter struct {
	opts Options
}

// New returns a converter. It cannot fail: everything it needs is built in.
func New(opts Options) *Converter {
	if opts.Workers < 1 {
		opts.Workers = 1
	}
	return &Converter{opts: opts}
}

// PDFPath is where the PDF for a Markdown file is written.
func PDFPath(mdPath string) string {
	return strings.TrimSuffix(mdPath, filepath.Ext(mdPath)) + ".pdf"
}

// Convert writes the PDF next to the Markdown file and returns its path.
// Images are read from paths relative to the Markdown file.
func (c *Converter) Convert(mdPath string) (out string, err error) {
	// A malformed page must never take the whole app down.
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("could not lay out this page: %v", r)
		}
	}()
	src, err := os.ReadFile(mdPath)
	if err != nil {
		return "", err
	}
	title := strings.TrimSuffix(filepath.Base(mdPath), filepath.Ext(mdPath))
	out = PDFPath(mdPath)
	tmp := out + ".part"
	if err := render(src, filepath.Dir(mdPath), title, tmp); err != nil {
		os.Remove(tmp)
		return "", err
	}
	return out, os.Rename(tmp, out)
}

// Progress reports the result of one file of a batch conversion.
type Progress struct {
	File  string
	Done  int
	Total int
	Err   error
}

// ConvertAll converts files with the configured number of workers, resting
// between files, and stops early when ctx is canceled.
func (c *Converter) ConvertAll(ctx context.Context, files []string, report func(Progress)) (converted int, errs []error) {
	var (
		mu   sync.Mutex
		wg   sync.WaitGroup
		done int
		jobs = make(chan string)
	)
	for i := 0; i < c.opts.Workers; i++ {
		wg.Go(func() {
			for f := range jobs {
				if t := c.opts.Throttle; t != nil {
					if t.Acquire(ctx) != nil {
						continue // canceled: drain the remaining jobs
					}
				}
				_, err := c.Convert(f)
				if t := c.opts.Throttle; t != nil {
					t.Release()
				}
				mu.Lock()
				done++
				if err != nil {
					errs = append(errs, fmt.Errorf("%s: %w", f, err))
				} else {
					converted++
				}
				p := Progress{File: f, Done: done, Total: len(files), Err: err}
				mu.Unlock()
				if report != nil {
					report(p)
				}
				// Give memory back to the system between big documents.
				debug.FreeOSMemory()
				pause := c.opts.Pause
				if t := c.opts.Throttle; t != nil {
					pause = t.Pause()
				}
				if pause > 0 {
					select {
					case <-time.After(pause):
					case <-ctx.Done():
					}
				}
			}
		})
	}
feed:
	for _, f := range files {
		select {
		case jobs <- f:
		case <-ctx.Done():
			break feed
		}
	}
	close(jobs)
	wg.Wait()
	return converted, errs
}

// FindMarkdown lists the Markdown files under root, skipping attachment
// folders and hidden folders.
func FindMarkdown(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if p != root && (strings.HasPrefix(name, ".") || name == "_attachments") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.EqualFold(filepath.Ext(name), ".md") && !strings.HasPrefix(name, ".") {
			files = append(files, p)
		}
		return nil
	})
	sort.Strings(files)
	return files, err
}
