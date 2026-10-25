package export

import (
	"net/url"
	"path"
	"regexp"
	"strings"

	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/base"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/commonmark"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/strikethrough"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/table"
	"github.com/PuerkitoBio/goquery"
)

// pageContext carries what the converter needs to rewrite links of one page.
type pageContext struct {
	tree    *Tree
	node    *Node
	siteURL string // Confluence web base (with /wiki on Cloud)
	// files maps an attachment's original name to its saved name, for every
	// attachment downloaded for this page. Nil when attachments are skipped.
	files map[string]string
}

var (
	attachmentPathRe = regexp.MustCompile(`/download/(?:attachments|thumbnails)/(\d+)/([^/?#]+)`)
	pageIDPathRe     = regexp.MustCompile(`/pages/(\d+)(?:/|$)`)
	pageIDQueryRe    = regexp.MustCompile(`[?&]pageId=(\d+)`)
	displayPathRe    = regexp.MustCompile(`/display/([^/?#]+)/([^?#]+)`)
	brushRe          = regexp.MustCompile(`brush:\s*([\w#+-]+)`)
	repeatedBreakRe  = regexp.MustCompile(`(?:<br />[ \t]*){2,}`)
)

// htmlToMarkdown converts a Confluence export_view body into Markdown, with
// links to attachments and to other exported pages made relative.
func htmlToMarkdown(body string, pc pageContext) (string, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader("<div>" + body + "</div>"))
	if err != nil {
		return "", err
	}
	cleanup(doc)

	doc.Find("img").Each(func(_ int, img *goquery.Selection) {
		src := img.AttrOr("data-image-src", img.AttrOr("src", ""))
		img.RemoveAttr("srcset")
		if src == "" {
			return
		}
		img.SetAttr("src", pc.rewrite(src))
		if strings.TrimSpace(img.AttrOr("alt", "")) == "" {
			if alias := img.AttrOr("data-linked-resource-default-alias", ""); alias != "" {
				img.SetAttr("alt", alias)
			}
		}
	})
	doc.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		a.SetAttr("href", pc.rewrite(a.AttrOr("href", "")))
	})

	html, err := doc.Find("body > div").First().Html()
	if err != nil {
		return "", err
	}
	conv := converter.NewConverter(converter.WithPlugins(
		base.NewBasePlugin(),
		commonmark.NewCommonmarkPlugin(),
		strikethrough.NewStrikethroughPlugin(),
		table.NewTablePlugin(
			table.WithSpanCellBehavior(table.SpanBehaviorMirror),
			// Confluence cells often hold several paragraphs or lists.
			table.WithNewlineBehavior(table.NewlineBehaviorPreserve),
			table.WithHeaderPromotion(true),
			table.WithSkipEmptyRows(true),
			table.WithCellPaddingBehavior(table.CellPaddingBehaviorMinimal),
		),
	))
	md, err := conv.ConvertString(html)
	if err != nil {
		return "", err
	}
	// Paragraphs inside table cells become runs of <br />; one is enough.
	md = repeatedBreakRe.ReplaceAllString(md, "<br />")
	return strings.TrimSpace(md), nil
}

// cleanup rewrites Confluence-specific markup into plain HTML that converts
// nicely.
func cleanup(doc *goquery.Document) {
	doc.Find("script, style, noscript").Remove()

	// Emoticons are images on the server; use the emoji character instead.
	doc.Find("img.emoticon").Each(func(_ int, img *goquery.Selection) {
		text := img.AttrOr("data-emoji-fallback", "")
		if text == "" || strings.HasPrefix(text, ":") {
			text = img.AttrOr("alt", "")
		}
		img.ReplaceWithHtml(escapeText(text))
	})

	// Mermaid diagrams keep their source as a ```mermaid block, which the
	// website draws. Diagrams already drawn as SVG are left alone.
	doc.Find("div.mermaid, pre.mermaid, code.mermaid").Each(func(_ int, m *goquery.Selection) {
		if m.Find("svg").Length() > 0 {
			return
		}
		src := strings.TrimSpace(m.Text())
		if src == "" {
			return
		}
		if m.Is("code") && m.Parent().Is("pre") {
			m = m.Parent()
		}
		m.ReplaceWithHtml(`<pre><code class="language-mermaid">` + escapeText(src) + "</code></pre>")
	})

	// Code macro: keep the language so the fenced block is highlighted.
	doc.Find("pre").Each(func(_ int, pre *goquery.Selection) {
		if pre.Find("code").Length() > 0 {
			return
		}
		lang := ""
		if m := brushRe.FindStringSubmatch(pre.AttrOr("data-syntaxhighlighter-params", "")); m != nil {
			lang = strings.ToLower(m[1])
		}
		code := goquery.NewDocumentFromNode(pre.Nodes[0]).Text()
		cls := ""
		if lang != "" && lang != "none" {
			cls = ` class="language-` + lang + `"`
		}
		pre.ReplaceWithHtml("<pre><code" + cls + ">" + escapeText(code) + "</code></pre>")
	})

	// Info / note / warning / tip panels become block quotes with a label.
	doc.Find("div.confluence-information-macro").Each(func(_ int, panel *goquery.Selection) {
		label := "Note"
		for cls, l := range map[string]string{
			"confluence-information-macro-information": "Info",
			"confluence-information-macro-tip":         "Tip",
			"confluence-information-macro-warning":     "Warning",
			"confluence-information-macro-note":        "Note",
		} {
			if panel.HasClass(cls) {
				label = l
			}
		}
		title := strings.TrimSpace(panel.Find(".title").First().Text())
		body := panel.Find(".confluence-information-macro-body").First()
		inner, _ := body.Html()
		if body.Length() == 0 {
			inner, _ = panel.Html()
		}
		heading := "<strong>" + label + "</strong>"
		if title != "" {
			heading += " — " + escapeText(title)
		}
		panel.ReplaceWithHtml("<blockquote><p>" + heading + "</p>" + inner + "</blockquote>")
	})

	// Expand macros: keep the title as a bold line, show the content.
	doc.Find(".expand-control").Each(func(_ int, ctl *goquery.Selection) {
		ctl.ReplaceWithHtml("<p><strong>" + escapeText(strings.TrimSpace(ctl.Text())) + "</strong></p>")
	})

	// Decorative icons carry no text.
	doc.Find("span.aui-icon, span.icon").Each(func(_ int, s *goquery.Selection) {
		if strings.TrimSpace(s.Text()) == "" {
			s.Remove()
		}
	})
}

func escapeText(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}

// rewrite turns a Confluence URL into a relative path inside the export when
// the target was exported, and into an absolute URL otherwise.
func (pc pageContext) rewrite(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "#") || strings.HasPrefix(raw, "mailto:") || strings.HasPrefix(raw, "data:") {
		return raw
	}
	abs := pc.absolute(raw)
	u, err := url.Parse(abs)
	if err != nil {
		return abs
	}
	site, _ := url.Parse(pc.siteURL)
	if site != nil && u.Host != "" && !strings.EqualFold(u.Host, site.Host) {
		return abs // external link
	}

	if m := attachmentPathRe.FindStringSubmatch(u.Path); m != nil {
		name, _ := url.PathUnescape(m[2])
		if rel, ok := pc.attachmentLink(m[1], name); ok {
			return rel
		}
		return abs
	}

	var target *Node
	if m := pageIDPathRe.FindStringSubmatch(u.Path); m != nil {
		target = pc.tree.ByID[m[1]]
	} else if m := pageIDQueryRe.FindStringSubmatch(abs); m != nil {
		target = pc.tree.ByID[m[1]]
	} else if m := displayPathRe.FindStringSubmatch(u.Path); m != nil && strings.EqualFold(m[1], pc.tree.Space.Key) {
		title, _ := url.PathUnescape(strings.ReplaceAll(m[2], "+", " "))
		target = pc.tree.LookupTitle(title)
	}
	if target != nil && target.Selected && !target.IsFolder() {
		return pc.relative(target.MarkdownPath())
	}
	return abs
}

func (pc pageContext) attachmentLink(pageID, name string) (string, bool) {
	if pageID == pc.node.ID {
		if pc.files == nil {
			return "", false
		}
		saved, ok := pc.files[name]
		if !ok {
			return "", false
		}
		return pc.relative(path.Join(pc.node.Dir, AttachmentsDir, saved)), true
	}
	// An attachment of another exported page: it is saved under that page.
	if pc.files == nil {
		return "", false
	}
	other := pc.tree.ByID[pageID]
	if other == nil || !other.Selected {
		return "", false
	}
	return pc.relative(path.Join(other.Dir, AttachmentsDir, SanitizeFileName(name))), true
}

// relative returns target (relative to the export root) relative to the
// current page's folder, URL-escaped so it works in Markdown and browsers.
func (pc pageContext) relative(target string) string {
	from := strings.Split(pc.node.Dir, "/")
	to := strings.Split(target, "/")
	i := 0
	for i < len(from) && i < len(to)-1 && from[i] == to[i] {
		i++
	}
	var parts []string
	for range from[i:] {
		parts = append(parts, "..")
	}
	for _, p := range to[i:] {
		parts = append(parts, url.PathEscape(p))
	}
	return strings.Join(parts, "/")
}

func (pc pageContext) absolute(raw string) string {
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return raw
	}
	site, err := url.Parse(pc.siteURL)
	if err != nil {
		return raw
	}
	if strings.HasPrefix(raw, "//") {
		return site.Scheme + ":" + raw
	}
	if strings.HasPrefix(raw, "/") {
		// Cloud links already include /wiki; avoid doubling it.
		if site.Path != "" && strings.HasPrefix(raw, site.Path+"/") {
			return site.Scheme + "://" + site.Host + raw
		}
		return strings.TrimRight(pc.siteURL, "/") + raw
	}
	return raw
}
