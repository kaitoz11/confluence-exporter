package pdf

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"golang.org/x/image/bmp"
)

func writeImage(t *testing.T, path string, enc func(f *os.File) error) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := enc(f); err != nil {
		t.Fatal(err)
	}
}

// tricky builds a Markdown document with everything that tends to break a
// PDF layout engine, plus the images it references.
func tricky(t *testing.T, dir string) string {
	att := filepath.Join(dir, "_attachments")
	if err := os.MkdirAll(att, 0o755); err != nil {
		t.Fatal(err)
	}
	// 16-bit PNG: fpdf cannot read it directly, so it must be converted.
	img16 := image.NewRGBA64(image.Rect(0, 0, 300, 120))
	for x := 0; x < 300; x++ {
		for y := 0; y < 120; y++ {
			img16.Set(x, y, color.RGBA64{uint16(x * 200), 20000, 50000, 65535})
		}
	}
	writeImage(t, filepath.Join(att, "deep color.png"), func(f *os.File) error { return png.Encode(f, img16) })
	// A large photo: must be shrunk before embedding.
	big := image.NewRGBA(image.Rect(0, 0, 6000, 4000))
	for x := 0; x < 6000; x += 7 {
		for y := 0; y < 4000; y += 7 {
			big.Set(x, y, color.RGBA{uint8(x), uint8(y), 120, 255})
		}
	}
	writeImage(t, filepath.Join(att, "photo.jpg"), func(f *os.File) error { return jpeg.Encode(f, big, nil) })
	writeImage(t, filepath.Join(att, "old.bmp"), func(f *os.File) error {
		return bmp.Encode(f, image.NewRGBA(image.Rect(0, 0, 80, 40)))
	})

	var b strings.Builder
	b.WriteString("# Tiếng Việt: Hướng dẫn sử dụng\n\n*Exported from Confluence — [view](https://example.com)*\n\n")
	b.WriteString("#### Skipped heading levels\n\n")
	b.WriteString("Emoji 🚀✅ and 中文 and Ελληνικά and русский, `inline code`, **bold**, *italic*, ***both***, ~~struck~~.\n\n")
	b.WriteString("Long URL: https://example.com/" + strings.Repeat("abcdefghij", 60) + "\n\n")
	b.WriteString("Hard  \nbreak and a <br /> raw break and &amp; &lt;entities&gt; and \\*escaped\\*.\n\n")
	b.WriteString(strings.Repeat("Một đoạn văn rất dài để kiểm tra việc ngắt dòng. ", 600) + "\n\n")
	b.WriteString("- level 1\n  - level 2\n    - level 3 with a long line " + strings.Repeat("word ", 40) + "\n- back\n\n")
	b.WriteString("3. three\n4. four\n\n- [x] done\n- [ ] todo\n\n")
	b.WriteString("> **Note**\n>\n> Quoted text that is long enough to wrap " + strings.Repeat("quote ", 30) + "\n\n")
	b.WriteString("Inline `mã nguồn: lời giải` code and a code block in Vietnamese:\n\n```\n// Hỏi nhóm: Đã có câu trả lời?\nflowchart TD\n```\n\n")
	b.WriteString("```go\nfunc main() {\n\tfmt.Println(\"" + strings.Repeat("x", 300) + "\")\n}\n```\n\n")
	b.WriteString("```\n" + strings.Repeat("line of code\n", 120) + "```\n\n")
	b.WriteString("| A | B | C | D | E | F |\n|---|:-:|--:|---|---|---|\n")
	for i := 0; i < 80; i++ {
		fmt.Fprintf(&b, "| row %d | **centre** | %d | %s | a<br />b | [link](../Other/Other.md) |\n", i, i*1000, strings.Repeat("long cell text ", i%7))
	}
	b.WriteString("\n![deep](_attachments/deep%20color.png)\n\n![photo](_attachments/photo.jpg)\n\n![bmp](_attachments/old.bmp)\n\n")
	b.WriteString("![missing](_attachments/nope.png) ![remote](https://example.com/x.png)\n\n")
	b.WriteString("<div>raw <b>html</b> block</div>\n\n---\n\nThe end.\n")

	p := filepath.Join(dir, "Tricky page.md")
	if err := os.WriteFile(p, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestConvertTricky(t *testing.T) {
	dir := t.TempDir()
	if d := os.Getenv("PDF_TEST_OUT"); d != "" {
		dir = d // keep the result for manual inspection
	}
	md := tricky(t, dir)

	var before runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	start := time.Now()
	out, err := New(Options{}).Convert(md)
	if err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	fi, err := os.Stat(out)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(out)
	pages := strings.Count(string(data), "/Type /Page\n")
	t.Logf("%s: %d KB, %d pages, %s, %d MB allocated in total", filepath.Base(out), fi.Size()>>10, pages, elapsed.Round(time.Millisecond), (after.TotalAlloc-before.TotalAlloc)>>20)
	if !strings.HasPrefix(string(data), "%PDF-") || pages < 5 {
		t.Fatalf("unexpected PDF: %d pages", pages)
	}
	if elapsed > 30*time.Second {
		t.Errorf("too slow: %s", elapsed)
	}
}

func TestConvertAllThrottled(t *testing.T) {
	dir := t.TempDir()
	var files []string
	for i := 0; i < 6; i++ {
		p := filepath.Join(dir, fmt.Sprintf("page %d.md", i))
		if err := os.WriteFile(p, []byte(fmt.Sprintf("# Page %d\n\nHello.\n", i)), 0o644); err != nil {
			t.Fatal(err)
		}
		files = append(files, p)
	}
	start := time.Now()
	n, errs := New(Options{Workers: 1, Pause: 50 * time.Millisecond}).ConvertAll(context.Background(), files, nil)
	if n != 6 || len(errs) > 0 {
		t.Fatalf("converted %d, errors %v", n, errs)
	}
	if time.Since(start) < 250*time.Millisecond {
		t.Errorf("pause between files was not applied")
	}

	// Canceling stops the batch early.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	n, _ = New(Options{Workers: 1}).ConvertAll(ctx, files, nil)
	if n > 1 {
		t.Errorf("converted %d files after cancel", n)
	}
}
