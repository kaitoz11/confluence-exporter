package site

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildPlainFolder(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// A folder that was not made by the exporter: loose files, no TOC.
	write("b-notes.md", "# Notes\n\nSee [guide](guides/guide.md).\n")
	write("a-readme.md", "No heading here. <script>alert(1)</script> <img src=x onerror=alert(1)>\n\nline<br>break\n")
	write("guides/guide.md", "# The guide\n\n| a | b |\n|---|---|\n| 1<br />2 | 3 |\n")
	write("_attachments/ignored.md", "# hidden\n")

	index, errs := Build(context.Background(), root, "", nil)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	if filepath.Base(index) != "index.html" {
		t.Fatalf("index = %s", index)
	}
	read := func(rel string) string {
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}

	readme := read("a-readme.html")
	if strings.Contains(readme, "<script>alert") || strings.Contains(readme, "<img src=x") {
		t.Errorf("raw HTML was not neutralized:\n%s", readme)
	}
	if !strings.Contains(readme, "line<br>break") {
		t.Errorf("line breaks should be kept")
	}
	if !strings.Contains(read("b-notes.html"), `href="guides/guide.html"`) {
		t.Errorf("link not rewritten to .html")
	}
	if !strings.Contains(read("guides/guide.html"), "1<br>2") {
		t.Errorf("table line break lost")
	}
	tree := read("_viewer/tree.js")
	// Folder "guides" has no own page, so it is a plain folder in the tree.
	for _, s := range []string{`"t":"a-readme"`, `"t":"Notes"`, `"t":"guides","c":[{"t":"The guide"`} {
		if !strings.Contains(tree, s) {
			t.Errorf("tree.js lacks %s: %s", s, tree)
		}
	}
	if strings.Contains(tree, "hidden") {
		t.Errorf("_attachments must not be scanned")
	}
	// No diagrams in this folder: the 5 MB library must not be copied.
	if _, err := os.Stat(filepath.Join(root, ViewerDir, "mermaid.min.js")); err == nil {
		t.Errorf("mermaid.min.js written without any diagram")
	}
}
