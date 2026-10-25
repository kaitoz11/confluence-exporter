package pdf

import (
	"fmt"
	"html"
	"regexp"
	"strings"

	"github.com/go-pdf/fpdf"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

// Page geometry in millimetres (A4).
const (
	pageW, pageH = 210.0, 297.0
	marginL      = 18.0
	marginR      = 18.0
	marginT      = 16.0
	marginB      = 18.0
	ptToMM       = 0.352778
	bodySize     = 10.5
	lineFactor   = 1.45
)

type rgb [3]int

var (
	colText     = rgb{23, 43, 77}
	colMuted    = rgb{107, 119, 140}
	colLink     = rgb{0, 82, 204}
	colCode     = rgb{174, 46, 36}
	colCodeBg   = rgb{244, 245, 247}
	colBorder   = rgb{193, 199, 208}
	colHeaderBg = rgb{235, 236, 240}
	colQuoteBar = rgb{76, 154, 255}
	colRule     = rgb{223, 225, 230}
)

var markdown = goldmark.New(goldmark.WithExtensions(extension.GFM))

// renderer lays out one Markdown document.
type renderer struct {
	pdf    *fpdf.Fpdf
	src    []byte
	dir    string // folder of the Markdown file; images are relative to it
	left   float64
	right  float64
	images map[string]loadedImage
	// lastLevel keeps PDF outline levels consistent (no jumps of 2+).
	lastLevel int
}

func render(src []byte, dir, title, outPath string) error {
	doc := markdown.Parser().Parse(text.NewReader(src))

	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(marginL, marginT, marginR)
	pdf.SetAutoPageBreak(false, marginB) // page breaks are handled here
	pdf.SetCellMargin(0)
	pdf.SetTitle(title, true)
	pdf.SetCreator("Confluence Space Exporter", true)
	addFonts(pdf)
	pdf.AliasNbPages("{nb}")

	r := &renderer{pdf: pdf, src: src, dir: dir, left: marginL, right: pageW - marginR, images: map[string]loadedImage{}, lastLevel: -1}
	footerTitle := cleanText(title, familySans)
	pdf.SetFooterFunc(func() {
		r.setStyle(style{size: 7.5, color: colMuted})
		pdf.SetXY(marginL, pageH-11)
		w := pageW - marginL - marginR
		pdf.CellFormat(w*0.75, 4, truncateTo(pdf, footerTitle, w*0.7), "", 0, "L", false, 0, "")
		pdf.CellFormat(w*0.25, 4, fmt.Sprintf("%d / {nb}", pdf.PageNo()), "", 0, "R", false, 0, "")
	})
	pdf.AddPage()

	r.blocks(doc, blockCtx{color: colText})
	if pdf.Err() {
		return pdf.Error()
	}
	return pdf.OutputFileAndClose(outPath)
}

// style describes how a piece of text is drawn.
type style struct {
	bold, italic, mono bool
	strike             bool
	size               float64
	color              rgb
	link               string
}

func (r *renderer) setStyle(st style) {
	family, s := familySans, ""
	if st.mono {
		family = familyMono
		if st.bold {
			s = "B"
		}
	} else {
		if st.bold {
			s += "B"
		}
		if st.italic {
			s += "I"
		}
	}
	if st.strike {
		s += "S"
	}
	size := st.size
	if size == 0 {
		size = bodySize
	}
	r.pdf.SetFont(family, s, size)
	r.pdf.SetTextColor(st.color[0], st.color[1], st.color[2])
}

func family(st style) string {
	if st.mono {
		return familyMono
	}
	return familySans
}

func lineHeight(size float64) float64 { return size * ptToMM * lineFactor }

// ensure starts a new page when h millimetres do not fit on this one.
func (r *renderer) ensure(h float64) {
	if r.pdf.GetY()+h > pageH-marginB && r.pdf.GetY() > marginT+0.5 {
		r.pdf.AddPage()
		r.pdf.SetY(marginT)
	}
}

func (r *renderer) space(h float64) {
	y := r.pdf.GetY() + h
	if y > pageH-marginB {
		r.pdf.AddPage()
		y = marginT
	}
	r.pdf.SetY(y)
}

// blockCtx is inherited by nested blocks.
type blockCtx struct {
	color rgb
	tight bool // inside a tight list: no space between paragraphs
	depth int  // list nesting
}

func (r *renderer) blocks(parent ast.Node, ctx blockCtx) {
	for n := parent.FirstChild(); n != nil; n = n.NextSibling() {
		r.block(n, ctx)
	}
}

func (r *renderer) block(n ast.Node, ctx blockCtx) {
	base := style{size: bodySize, color: ctx.color}
	switch n := n.(type) {
	case *ast.Heading:
		r.heading(n, ctx)
	case *ast.Paragraph:
		r.paragraph(r.spans(n, base))
		if !ctx.tight {
			r.space(2.6)
		}
	case *ast.TextBlock:
		r.paragraph(r.spans(n, base))
		r.space(0.8)
	case *ast.List:
		r.list(n, ctx)
	case *ast.Blockquote:
		r.blockquote(n, ctx)
	case *ast.FencedCodeBlock, *ast.CodeBlock:
		r.codeBlock(n)
	case *ast.ThematicBreak:
		r.ensure(6)
		y := r.pdf.GetY() + 2.5
		r.rule(y, colRule, 0.3)
		r.pdf.SetY(y + 3.5)
	case *east.Table:
		r.table(n)
		r.space(3)
	case *ast.HTMLBlock:
		var b strings.Builder
		for i := 0; i < n.Lines().Len(); i++ {
			seg := n.Lines().At(i)
			b.Write(seg.Value(r.src))
		}
		if t := htmlToText(b.String()); strings.TrimSpace(t) != "" {
			r.paragraph([]span{{text: t, st: base}})
			r.space(2.6)
		}
	default:
		r.blocks(n, ctx)
	}
}

func (r *renderer) rule(y float64, c rgb, width float64) {
	r.pdf.SetDrawColor(c[0], c[1], c[2])
	r.pdf.SetLineWidth(width)
	r.pdf.Line(r.left, y, r.right, y)
}

var headingSizes = map[int]float64{1: 20, 2: 15.5, 3: 13, 4: 11.5, 5: 10.5, 6: 10.5}

func (r *renderer) heading(n *ast.Heading, ctx blockCtx) {
	size := headingSizes[n.Level]
	lh := lineHeight(size)
	if r.pdf.GetY() > marginT+1 {
		r.space(lh * 0.45)
	}
	r.ensure(lh*2 + lineHeight(bodySize)*2) // keep the heading with what follows
	spans := r.spans(n, style{bold: true, size: size, color: ctx.color})

	level := min(n.Level-1, r.lastLevel+1)
	r.lastLevel = level
	r.pdf.Bookmark(plainText(spans), level, -1)

	r.paragraph(spans)
	if n.Level <= 2 {
		y := r.pdf.GetY() + 0.8
		r.rule(y, colRule, 0.35)
		r.pdf.SetY(y + 0.4)
	}
	r.space(lh * 0.3)
}

func (r *renderer) list(n *ast.List, ctx blockCtx) {
	num := n.Start
	if num == 0 {
		num = 1
	}
	bullets := []string{"•", "◦", "▪"}
	child := ctx
	child.tight = n.IsTight
	child.depth++
	base := style{size: bodySize, color: ctx.color}
	for item := n.FirstChild(); item != nil; item = item.NextSibling() {
		marker := bullets[ctx.depth%len(bullets)]
		if n.IsOrdered() {
			marker = fmt.Sprintf("%d.", num)
			num++
		}
		indent := 6.0
		if n.IsOrdered() {
			indent = 4 + r.measure(marker, base)
		}
		lh := lineHeight(bodySize)
		r.ensure(lh)
		y := r.pdf.GetY()
		r.setStyle(base)
		r.pdf.SetXY(r.left, y)
		r.pdf.CellFormat(indent-1.5, lh, marker, "", 0, "R", false, 0, "")
		r.pdf.SetY(y)
		old := r.left
		r.left += indent
		r.blocks(item, child)
		r.left = old
	}
	if ctx.depth == 0 {
		r.space(1.8)
	}
}

func (r *renderer) blockquote(n *ast.Blockquote, ctx blockCtx) {
	r.ensure(lineHeight(bodySize))
	y0, page0 := r.pdf.GetY(), r.pdf.PageNo()
	old := r.left
	r.left += 5
	child := ctx
	child.color = rgb{37, 56, 88}
	r.blocks(n, child)
	r.left = old
	y1 := r.pdf.GetY() - 2
	if r.pdf.PageNo() != page0 {
		y0 = marginT
	}
	r.pdf.SetDrawColor(colQuoteBar[0], colQuoteBar[1], colQuoteBar[2])
	r.pdf.SetLineWidth(0.9)
	r.pdf.Line(r.left+1.2, y0, r.left+1.2, max(y1, y0+lineHeight(bodySize)))
	r.space(1)
}

var (
	tagRe       = regexp.MustCompile(`(?s)<[^>]*>`)
	lineBreakRe = regexp.MustCompile(`(?i)<br\s*/?>|</p>|</div>|</li>|</tr>`)
)

// htmlToText keeps the text of raw HTML left in the Markdown.
func htmlToText(s string) string {
	s = lineBreakRe.ReplaceAllString(s, "\n")
	return html.UnescapeString(tagRe.ReplaceAllString(s, ""))
}

func truncateTo(pdf *fpdf.Fpdf, s string, w float64) string {
	if pdf.GetStringWidth(s) <= w {
		return s
	}
	rs := []rune(s)
	for len(rs) > 1 && pdf.GetStringWidth(string(rs)+"…") > w {
		rs = rs[:len(rs)-1]
	}
	return string(rs) + "…"
}
