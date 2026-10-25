package site

import (
	"bytes"
	"html"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	gmhtml "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

// safeHTML renders raw HTML found in Markdown as text, except line breaks,
// which the exporter uses inside table cells. Pages can therefore never run
// scripts, whatever their content.
type safeHTML struct{}

var brRe = regexp.MustCompile(`(?i)^<br\s*/?>$`)

func (safeHTML) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindRawHTML, func(w util.BufWriter, src []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		raw := n.(*ast.RawHTML)
		var b strings.Builder
		for i := 0; i < raw.Segments.Len(); i++ {
			seg := raw.Segments.At(i)
			b.Write(seg.Value(src))
		}
		if brRe.MatchString(strings.TrimSpace(b.String())) {
			_, _ = w.WriteString("<br>")
		} else {
			_, _ = w.WriteString(html.EscapeString(b.String()))
		}
		return ast.WalkContinue, nil
	})
	reg.Register(ast.KindHTMLBlock, func(w util.BufWriter, src []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		_, _ = w.WriteString("<p>")
		for i := 0; i < n.Lines().Len(); i++ {
			seg := n.Lines().At(i)
			_, _ = w.WriteString(html.EscapeString(string(seg.Value(src))))
		}
		_, _ = w.WriteString("</p>\n")
		return ast.WalkContinue, nil
	})
}

var md = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithParserOptions(parser.WithAutoHeadingID()),
	goldmark.WithRendererOptions(
		gmhtml.WithXHTML(),
		renderer.WithNodeRenderers(util.Prioritized(safeHTML{}, 100)),
	),
)

var (
	mdHrefRe     = regexp.MustCompile(`href="([^"#:]+)\.md(#[^"]*)?"`)
	originalRe   = regexp.MustCompile(`(?s)<p><em>Exported from Confluence — <a href="([^"]+)">[^<]*</a></em></p>\s*`)
	firstH1Re    = regexp.MustCompile(`(?s)^\s*<h1[^>]*>.*?</h1>\s*`)
	attachRe     = regexp.MustCompile(`(?s)(<h2 id="attachments">Attachments</h2>\s*)<ul>`)
	tagRe        = regexp.MustCompile(`(?s)<[^>]*>`)
	spaceRe      = regexp.MustCompile(`\s+`)
	externalLink = regexp.MustCompile(`<a href="(https?://[^"]+)"`)
)

type rendered struct {
	body     string // HTML of the page content, without the title
	original string // link to the page in Confluence, if known
	text     string // plain text for search
}

// renderPage converts one exported Markdown page.
func renderPage(src []byte) (rendered, error) {
	var buf bytes.Buffer
	if err := md.Convert(src, &buf); err != nil {
		return rendered{}, err
	}
	out := buf.String()
	var r rendered
	if m := originalRe.FindStringSubmatch(out); m != nil {
		r.original = html.UnescapeString(m[1])
		out = strings.Replace(out, m[0], "", 1)
	}
	// The title is shown by the page header.
	out = firstH1Re.ReplaceAllString(out, "")
	out = mdHrefRe.ReplaceAllString(out, `href="$1.html$2"`)
	out = attachRe.ReplaceAllString(out, `$1<ul class="attachments">`)
	out = externalLink.ReplaceAllString(out, `<a href="$1" target="_blank" rel="noopener"`)
	r.body = out
	r.text = plain(out, 4000)
	return r, nil
}

// plain returns at most n characters of the text of an HTML fragment.
func plain(h string, n int) string {
	t := html.UnescapeString(tagRe.ReplaceAllString(h, " "))
	t = strings.TrimSpace(spaceRe.ReplaceAllString(t, " "))
	if r := []rune(t); len(r) > n {
		t = string(r[:n])
	}
	return t
}
