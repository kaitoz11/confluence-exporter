// Package export turns a Confluence space into a folder tree of Markdown
// files and attachments that mirrors the space's page tree.
package export

import (
	"path"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/kaitoz11/confluence-exporter/internal/confluence"
	"github.com/kaitoz11/confluence-exporter/internal/site"
)

// AttachmentsDir is the folder, inside each page folder, holding its files.
const AttachmentsDir = "_attachments"

// TOCFile is the table of contents written at the root of the export.
const TOCFile = site.TOCFile

// Node is one page (or folder) of the space tree.
type Node struct {
	confluence.Page
	Parent   *Node
	Children []*Node
	Depth    int
	// Name is the file-system-safe name, unique among siblings.
	Name string
	// Dir is the page folder, relative to the export root, with forward slashes.
	Dir      string
	Selected bool
	Expanded bool
}

// MarkdownPath is the page's .md file, relative to the export root.
func (n *Node) MarkdownPath() string { return path.Join(n.Dir, n.Name+".md") }

// IsFolder reports whether the node is a container without its own content.
func (n *Node) IsFolder() bool { return n.Kind == confluence.KindFolder }

// Tree is the content tree of one space.
type Tree struct {
	Space confluence.Space
	Roots []*Node
	ByID  map[string]*Node
	// byTitle helps resolve Server/DC links of the form /display/KEY/Title.
	byTitle map[string]*Node
}

// BuildTree arranges the flat page list into the space's tree, ordered the
// same way Confluence orders the sidebar, and assigns each node a folder.
func BuildTree(space confluence.Space, pages []confluence.Page) *Tree {
	t := &Tree{Space: space, ByID: map[string]*Node{}, byTitle: map[string]*Node{}}
	for _, p := range pages {
		n := &Node{Page: p, Selected: true}
		t.ByID[p.ID] = n
		// Titles are unique per space on Server/DC; on Cloud keep the first.
		if _, dup := t.byTitle[strings.ToLower(p.Title)]; !dup && p.Kind == confluence.KindPage {
			t.byTitle[strings.ToLower(p.Title)] = n
		}
	}
	for _, p := range pages {
		n := t.ByID[p.ID]
		if parent, ok := t.ByID[p.ParentID]; ok && p.ParentID != p.ID {
			n.Parent = parent
			parent.Children = append(parent.Children, n)
		} else {
			t.Roots = append(t.Roots, n)
		}
	}
	sortNodes(t.Roots, space.HomepageID)
	reserved := []string{strings.TrimSuffix(TOCFile, ".md")}
	assign(t.Roots, "", 0, reserved)
	// Show the top two levels in the preview straight away.
	t.Walk(func(n *Node) bool {
		n.Expanded = n.Depth < 1
		return true
	})
	return t
}

func sortNodes(nodes []*Node, homepageID string) {
	sort.SliceStable(nodes, func(i, j int) bool {
		a, b := nodes[i], nodes[j]
		if (a.ID == homepageID) != (b.ID == homepageID) {
			return a.ID == homepageID
		}
		if a.Position >= 0 && b.Position >= 0 && a.Position != b.Position {
			return a.Position < b.Position
		}
		if (a.Position >= 0) != (b.Position >= 0) {
			return a.Position >= 0
		}
		return strings.ToLower(a.Title) < strings.ToLower(b.Title)
	})
	for _, n := range nodes {
		sortNodes(n.Children, homepageID)
	}
}

func assign(nodes []*Node, dir string, depth int, reserved []string) {
	used := map[string]bool{strings.ToLower(AttachmentsDir): true}
	for _, r := range reserved {
		used[strings.ToLower(r)] = true
	}
	for _, n := range nodes {
		n.Depth = depth
		n.Name = UniqueName(SanitizeName(n.Title), used)
		n.Dir = path.Join(dir, n.Name)
		assign(n.Children, n.Dir, depth+1, nil)
	}
}

// UniqueName returns name, or name (2), name (3)... so that it is unique in
// used. Comparison ignores case because macOS and Windows do.
func UniqueName(name string, used map[string]bool) string {
	candidate := name
	for i := 2; used[strings.ToLower(candidate)]; i++ {
		candidate = name + " (" + strconv.Itoa(i) + ")"
	}
	used[strings.ToLower(candidate)] = true
	return candidate
}

// Walk visits nodes depth-first in tree order. Returning false skips children.
func (t *Tree) Walk(fn func(*Node) bool) {
	var walk func([]*Node)
	walk = func(nodes []*Node) {
		for _, n := range nodes {
			if fn(n) {
				walk(n.Children)
			}
		}
	}
	walk(t.Roots)
}

// Selected returns the selected nodes in tree order.
func (t *Tree) Selected() []*Node {
	var out []*Node
	t.Walk(func(n *Node) bool {
		if n.Selected {
			out = append(out, n)
		}
		return true
	})
	return out
}

// Counts returns the number of selected and total pages (folders excluded).
func (t *Tree) Counts() (selected, total int) {
	t.Walk(func(n *Node) bool {
		if !n.IsFolder() {
			total++
			if n.Selected {
				selected++
			}
		}
		return true
	})
	return
}

// SetSelected selects or deselects a node and all of its descendants.
func (n *Node) SetSelected(v bool) {
	n.Selected = v
	for _, c := range n.Children {
		c.SetSelected(v)
	}
}

// LookupTitle finds a page by its title (case-insensitive).
func (t *Tree) LookupTitle(title string) *Node { return t.byTitle[strings.ToLower(title)] }

var windowsReserved = map[string]bool{
	"con": true, "prn": true, "aux": true, "nul": true,
	"com1": true, "com2": true, "com3": true, "com4": true, "com5": true, "com6": true, "com7": true, "com8": true, "com9": true,
	"lpt1": true, "lpt2": true, "lpt3": true, "lpt4": true, "lpt5": true, "lpt6": true, "lpt7": true, "lpt8": true, "lpt9": true,
}

// maxNameRunes keeps deep trees under the Windows path length limit.
const maxNameRunes = 80

// SanitizeName makes a page title or file name safe to use as a file name on
// Windows, macOS and Linux, while keeping it readable.
func SanitizeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '/' || r == '\\' || r == '|':
			b.WriteRune('-')
		case r == ':':
			b.WriteString(" -")
		case r == '"':
			b.WriteRune('\'')
		case r == '<' || r == '>' || r == '*' || r == '?':
			// dropped
		case unicode.IsControl(r):
			b.WriteRune(' ')
		default:
			b.WriteRune(r)
		}
	}
	out := strings.Join(strings.Fields(b.String()), " ")
	if r := []rune(out); len(r) > maxNameRunes {
		out = strings.TrimSpace(string(r[:maxNameRunes]))
	}
	out = strings.TrimRight(out, ". ")
	out = strings.TrimLeft(out, " ")
	if out == "" {
		out = "Untitled"
	}
	base := strings.ToLower(out)
	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = base[:i]
	}
	if windowsReserved[base] {
		out = "_" + out
	}
	return out
}

// SanitizeFileName is like SanitizeName but keeps the extension intact when
// the name has to be shortened.
func SanitizeFileName(s string) string {
	ext := path.Ext(s)
	if len([]rune(s)) <= maxNameRunes || len(ext) > 10 {
		return SanitizeName(s)
	}
	stem := SanitizeName(strings.TrimSuffix(s, ext))
	return stem + SanitizeName(ext)
}
