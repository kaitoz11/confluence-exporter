package site

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ViewerDir holds the shared files of the website.
const ViewerDir = "_viewer"

// IndexFile is the landing page at the root of the website.
const IndexFile = "index.html"

var (
	//go:embed assets/viewer.css
	viewerCSS []byte
	//go:embed assets/viewer.js
	viewerJS []byte
	//go:embed assets/page.html
	pageHTML string
)

var pageTmpl = template.Must(template.New("page").Funcs(template.FuncMap{
	// initial is the first letter of the space name, for its badge.
	"initial": func(s string) string {
		for _, r := range strings.TrimSpace(s) {
			return strings.ToUpper(string(r))
		}
		return "?"
	},
}).Parse(pageHTML))

// Progress reports one built page.
type Progress struct {
	File  string
	Done  int
	Total int
	Err   error
}

// treeItem is the navigation data given to the browser.
type treeItem struct {
	T string     `json:"t"`           // title
	H string     `json:"h,omitempty"` // root-relative link, empty for folders
	C []treeItem `json:"c,omitempty"` // children
}

type searchItem struct {
	T string `json:"t"`
	H string `json:"h"`
	X string `json:"x"` // plain text
}

type link struct {
	Title string
	Href  string
}

type pageData struct {
	Space    string
	Title    string
	Root     string // relative path from the page to the site root, "" or "../../"
	Path     string // root-relative path of this page
	Crumbs   []link
	Original string
	Content  template.HTML
	Prev     *link
	Next     *link
	IsIndex  bool
	Count    int
	Built    string
}

// Build writes the website for the folder root and returns the path of its
// index.html. title is the name shown in the header (the space name).
func Build(ctx context.Context, root, title string, report func(Progress)) (string, []error) {
	tree, err := Scan(root)
	if err != nil {
		return "", []error{err}
	}
	if title == "" {
		title = tree.Title
	}
	var pages []*Node
	tree.Walk(func(n *Node) {
		if n.IsPage() {
			pages = append(pages, n)
		}
	})
	if len(pages) == 0 {
		return "", []error{fmt.Errorf("no Markdown (.md) pages were found in this folder")}
	}

	viewer := filepath.Join(root, ViewerDir)
	if err := os.MkdirAll(viewer, 0o755); err != nil {
		return "", []error{err}
	}
	for name, data := range map[string][]byte{"viewer.css": viewerCSS, "viewer.js": viewerJS} {
		if err := os.WriteFile(filepath.Join(viewer, name), data, 0o644); err != nil {
			return "", []error{err}
		}
	}
	if err := writeJS(filepath.Join(viewer, "tree.js"), "SPACE", map[string]any{"title": title, "tree": items(tree.Children)}); err != nil {
		return "", []error{err}
	}

	var errs []error
	var search []searchItem
	built := time.Now().Format("2 January 2006")
	for i, n := range pages {
		if ctx.Err() != nil {
			break
		}
		src, err := os.ReadFile(n.MD)
		var r rendered
		if err == nil {
			r, err = renderPage(src)
		}
		if err == nil {
			d := pageData{
				Space: title, Title: n.Title, Root: rootPrefix(n.Rel), Path: n.Rel,
				Crumbs: crumbs(n), Original: r.original, Content: template.HTML(r.body),
				Count: len(pages), Built: built,
			}
			if i > 0 {
				d.Prev = &link{pages[i-1].Title, escapePath(pages[i-1].Rel)}
			}
			if i+1 < len(pages) {
				d.Next = &link{pages[i+1].Title, escapePath(pages[i+1].Rel)}
			}
			err = writePage(filepath.Join(root, filepath.FromSlash(n.Rel)), d)
			search = append(search, searchItem{T: n.Title, H: escapePath(n.Rel), X: r.text})
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", n.Title, err))
		}
		if report != nil {
			report(Progress{File: n.MD, Done: i + 1, Total: len(pages), Err: err})
		}
	}
	if err := writeJS(filepath.Join(viewer, "search.js"), "SEARCH", search); err != nil {
		errs = append(errs, err)
	}

	index := filepath.Join(root, IndexFile)
	d := pageData{
		Space: title, Title: title, Path: IndexFile, IsIndex: true,
		Content: template.HTML(outline(tree.Children)), Count: len(pages), Built: built,
		Next: &link{pages[0].Title, escapePath(pages[0].Rel)},
	}
	if err := writePage(index, d); err != nil {
		errs = append(errs, err)
	}
	return index, errs
}

func writePage(path string, d pageData) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := pageTmpl.Execute(f, d); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// writeJS writes `window.NAME = <json>;` so the data loads from a <script>
// tag, which browsers allow for local files (unlike fetch).
func writeJS(path, name string, v any) error {
	b, err := json.Marshal(v) // escapes <, > and & for safe embedding
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte("window."+name+" = "+string(b)+";\n"), 0o644)
}

func items(nodes []*Node) []treeItem {
	out := make([]treeItem, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, treeItem{T: n.Title, H: escapePath(n.Rel), C: items(n.Children)})
	}
	return out
}

func crumbs(n *Node) []link {
	var out []link
	for p := n.Parent; p != nil && p.Parent != nil; p = p.Parent {
		out = append([]link{{p.Title, escapePath(p.Rel)}}, out...)
	}
	return out
}

// outline is the clickable table of contents on the landing page.
func outline(nodes []*Node) string {
	var b strings.Builder
	b.WriteString(`<ul class="toc">`)
	for _, n := range nodes {
		b.WriteString("<li>")
		if n.IsPage() {
			fmt.Fprintf(&b, `<a href="%s">%s</a>`, template.HTMLEscapeString(escapePath(n.Rel)), template.HTMLEscapeString(n.Title))
		} else {
			fmt.Fprintf(&b, `<span class="folder">%s</span>`, template.HTMLEscapeString(n.Title))
		}
		if len(n.Children) > 0 {
			b.WriteString(outline(n.Children))
		}
		b.WriteString("</li>")
	}
	b.WriteString("</ul>")
	return b.String()
}

// rootPrefix is "../" once per folder level of rel.
func rootPrefix(rel string) string {
	return strings.Repeat("../", strings.Count(rel, "/"))
}

func escapePath(p string) string {
	if p == "" {
		return ""
	}
	parts := strings.Split(p, "/")
	for i, s := range parts {
		parts[i] = url.PathEscape(s)
	}
	return strings.Join(parts, "/")
}
