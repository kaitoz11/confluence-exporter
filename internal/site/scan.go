// Package site builds an offline website from a folder of exported Markdown
// pages: one HTML file next to each .md file, an index.html landing page,
// a navigation tree, search and an outline. It needs no server: everything
// works by double-clicking index.html.
package site

import (
	"bufio"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// TOCFile lists the pages in Confluence's order; the exporter writes it.
const TOCFile = "Table of Contents.md"

// Node is a page (or a folder without its own page) of the site.
type Node struct {
	Title    string
	MD       string // absolute path of the Markdown file; empty for folders
	Rel      string // HTML path relative to the root, with forward slashes
	Parent   *Node
	Children []*Node
	order    int
}

// IsPage reports whether the node has content.
func (n *Node) IsPage() bool { return n.MD != "" }

// Scan reads the folder tree. A folder whose .md file has the folder's name
// (as written by the exporter) is one page; its sub-folders are its
// sub-pages. Any other .md file is a page of its own.
func Scan(root string) (*Node, error) {
	top := &Node{Title: filepath.Base(root)}
	order := tocOrder(root)
	if err := scanDir(root, root, top, order); err != nil {
		return nil, err
	}
	return top, nil
}

func scanDir(root, dir string, parent *Node, order map[string]int) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	isRoot := dir == root
	base := filepath.Base(dir)
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
			continue // hidden files, _attachments, _viewer
		}
		p := filepath.Join(dir, name)
		if e.IsDir() {
			child := &Node{Parent: parent, Title: name}
			if own := filepath.Join(p, name+".md"); isFile(own) {
				child.MD = own
				child.Title = mdTitle(own, name)
			}
			if err := scanDir(root, p, child, order); err != nil {
				return err
			}
			if child.IsPage() || len(child.Children) > 0 {
				parent.Children = append(parent.Children, child)
			}
			continue
		}
		stem := strings.TrimSuffix(name, filepath.Ext(name))
		if !strings.EqualFold(filepath.Ext(name), ".md") ||
			(isRoot && name == TOCFile) ||
			(!isRoot && stem == base) { // the folder's own page, handled above
			continue
		}
		parent.Children = append(parent.Children, &Node{Parent: parent, MD: p, Title: mdTitle(p, stem)})
	}

	for _, c := range parent.Children {
		key := c.MD
		if key == "" {
			// A folder sorts by its first page.
			key = firstPage(c)
		}
		rel, _ := filepath.Rel(root, key)
		c.order = len(order) + 1 // unknown pages go last
		if i, ok := order[filepath.ToSlash(rel)]; ok {
			c.order = i
		}
		if c.MD != "" {
			r, _ := filepath.Rel(root, c.MD)
			c.Rel = filepath.ToSlash(strings.TrimSuffix(r, filepath.Ext(r)) + ".html")
		}
	}
	sort.SliceStable(parent.Children, func(i, j int) bool {
		a, b := parent.Children[i], parent.Children[j]
		if a.order != b.order {
			return a.order < b.order
		}
		return strings.ToLower(a.Title) < strings.ToLower(b.Title)
	})
	return nil
}

func firstPage(n *Node) string {
	for _, c := range n.Children {
		if c.MD != "" {
			return c.MD
		}
		if p := firstPage(c); p != "" {
			return p
		}
	}
	return ""
}

var tocLinkRe = regexp.MustCompile(`\]\(([^)]+?\.md)\)`)

// tocOrder maps each page (relative .md path) to its position in the table
// of contents, which follows the Confluence sidebar.
func tocOrder(root string) map[string]int {
	order := map[string]int{}
	data, err := os.ReadFile(filepath.Join(root, TOCFile))
	if err != nil {
		return order
	}
	for i, m := range tocLinkRe.FindAllStringSubmatch(string(data), -1) {
		if p, err := url.PathUnescape(m[1]); err == nil {
			if _, seen := order[p]; !seen {
				order[p] = i
			}
		}
	}
	return order
}

// mdTitle is the first "# " heading of the file, or fallback.
func mdTitle(path, fallback string) string {
	f, err := os.Open(path)
	if err != nil {
		return fallback
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for i := 0; i < 40 && sc.Scan(); i++ {
		if line := strings.TrimSpace(sc.Text()); strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
	}
	return fallback
}

func isFile(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

// Walk visits nodes depth-first in order.
func (n *Node) Walk(fn func(*Node)) {
	for _, c := range n.Children {
		fn(c)
		c.Walk(fn)
	}
}
