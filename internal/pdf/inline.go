package pdf

import (
	"errors"
	"regexp"
	"strings"
	"unicode"

	"github.com/yuin/goldmark/ast"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/util"
)

// span is a run of text in one style, a forced line break, or an image.
type span struct {
	text  string
	st    style
	br    bool
	image string // image destination; text holds the alt text
}

// piece is a measured part of a laid-out line.
type piece struct {
	text string
	st   style
	w    float64
}

type line struct {
	pieces []piece
	w, h   float64
}

var mdLinkRe = regexp.MustCompile(`^([^:#?]+)\.md(#.*)?$`)

// spans flattens the inline content of n.
func (r *renderer) spans(n ast.Node, st style) []span {
	var out []span
	var walk func(n ast.Node, st style)
	walk = func(n ast.Node, st style) {
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			switch c := c.(type) {
			case *ast.Text:
				v := c.Segment.Value(r.src)
				if !c.IsRaw() && !st.mono {
					v = util.ResolveEntityNames(util.ResolveNumericReferences(util.UnescapePunctuations(v)))
				}
				out = append(out, span{text: string(v), st: st})
				if c.HardLineBreak() {
					out = append(out, span{br: true, st: st})
				} else if c.SoftLineBreak() {
					out = append(out, span{text: " ", st: st})
				}
			case *ast.String:
				out = append(out, span{text: string(c.Value), st: st})
			case *ast.Emphasis:
				s := st
				if c.Level >= 2 {
					s.bold = true
				} else {
					s.italic = true
				}
				walk(c, s)
			case *east.Strikethrough:
				s := st
				s.strike = true
				walk(c, s)
			case *ast.CodeSpan:
				s := st
				s.mono, s.color = true, colCode
				s.size = st.size * 0.92
				walk(c, s)
			case *ast.Link:
				s := st
				s.color, s.link = colLink, linkTarget(string(c.Destination))
				walk(c, s)
			case *ast.AutoLink:
				s := st
				s.color, s.link = colLink, string(c.URL(r.src))
				out = append(out, span{text: string(c.Label(r.src)), st: s})
			case *ast.Image:
				out = append(out, span{image: string(c.Destination), text: plainText(r.spans(c, st)), st: st})
			case *ast.RawHTML:
				var b strings.Builder
				for i := 0; i < c.Segments.Len(); i++ {
					seg := c.Segments.At(i)
					b.Write(seg.Value(r.src))
				}
				if t := htmlToText(b.String()); t == "\n" {
					out = append(out, span{br: true, st: st})
				} else if t != "" {
					out = append(out, span{text: t, st: st})
				}
			case *east.TaskCheckBox:
				box := "☐ "
				if c.IsChecked {
					box = "☑ "
				}
				out = append(out, span{text: box, st: st})
			default:
				walk(c, st)
			}
		}
	}
	walk(n, st)
	return out
}

// linkTarget points links between exported pages at their PDFs.
func linkTarget(dest string) string {
	if m := mdLinkRe.FindStringSubmatch(dest); m != nil {
		return m[1] + ".pdf" + m[2]
	}
	return dest
}

func plainText(spans []span) string {
	var b strings.Builder
	for _, s := range spans {
		if s.br {
			b.WriteByte(' ')
		}
		b.WriteString(s.text)
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func (r *renderer) measure(s string, st style) float64 {
	r.setStyle(st)
	return r.pdf.GetStringWidth(s)
}

// paragraph lays out spans from the current position, placing images on
// their own lines.
func (r *renderer) paragraph(spans []span) {
	var run []span
	flush := func() {
		if len(run) > 0 {
			r.drawLines(r.breakLines(run, r.right-r.left), r.left, r.right-r.left, "L")
			run = nil
		}
	}
	for _, s := range spans {
		if s.image != "" {
			flush()
			r.image(s)
			continue
		}
		run = append(run, s)
	}
	flush()
}

// tokenize splits text into words and single spaces.
func tokenize(s string) []string {
	var out []string
	start := -1
	for i, c := range s {
		if c == '\n' {
			if start >= 0 {
				out = append(out, s[start:i])
				start = -1
			}
			out = append(out, "\n")
			continue
		}
		if unicode.IsSpace(c) {
			if start >= 0 {
				out = append(out, s[start:i])
				start = -1
			}
			if len(out) == 0 || out[len(out)-1] != " " {
				out = append(out, " ")
			}
			continue
		}
		if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		out = append(out, s[start:])
	}
	return out
}

// breakLines wraps spans into lines no wider than width.
func (r *renderer) breakLines(spans []span, width float64) []line {
	var lines []line
	var cur line
	minH := 0.0
	push := func() {
		// Drop trailing spaces.
		for len(cur.pieces) > 0 && strings.TrimSpace(cur.pieces[len(cur.pieces)-1].text) == "" {
			cur.w -= cur.pieces[len(cur.pieces)-1].w
			cur.pieces = cur.pieces[:len(cur.pieces)-1]
		}
		if cur.h == 0 {
			cur.h = minH
		}
		lines = append(lines, cur)
		cur = line{}
	}
	add := func(text string, st style, w float64) {
		if n := len(cur.pieces); n > 0 && cur.pieces[n-1].st == st {
			cur.pieces[n-1].text += text
			cur.pieces[n-1].w += w
		} else {
			cur.pieces = append(cur.pieces, piece{text: text, st: st, w: w})
		}
		cur.w += w
		cur.h = max(cur.h, lineHeight(st.size))
	}
	for _, s := range spans {
		minH = max(minH, lineHeight(s.st.size))
		if s.br {
			push()
			continue
		}
		fam := family(s.st)
		for _, tok := range tokenize(s.text) {
			if tok == "\n" {
				push()
				continue
			}
			if tok == " " && len(cur.pieces) == 0 {
				continue
			}
			tok = cleanText(tok, fam)
			w := r.measure(tok, s.st)
			if cur.w+w <= width {
				add(tok, s.st, w)
				continue
			}
			if tok == " " {
				push()
				continue
			}
			if len(cur.pieces) > 0 && w <= width {
				push()
				add(tok, s.st, w)
				continue
			}
			// A word longer than the line (URLs, IDs): break it anywhere.
			var part []rune
			pw := 0.0
			for _, c := range tok {
				cw := r.measure(string(c), s.st)
				if cur.w+pw+cw > width && (len(part) > 0 || len(cur.pieces) > 0) {
					if len(part) > 0 {
						add(string(part), s.st, pw)
					}
					push()
					part, pw = nil, 0
				}
				part = append(part, c)
				pw += cw
			}
			if len(part) > 0 {
				add(string(part), s.st, pw)
			}
		}
	}
	if len(cur.pieces) > 0 {
		push()
	}
	return lines
}

// drawLines draws lines at x, breaking pages as needed.
func (r *renderer) drawLines(lines []line, x, width float64, align string) {
	for _, ln := range lines {
		r.ensure(ln.h)
		y := r.pdf.GetY()
		px := x
		switch align {
		case "C":
			px += (width - ln.w) / 2
		case "R":
			px += width - ln.w
		}
		for _, p := range ln.pieces {
			r.setStyle(p.st)
			r.pdf.SetXY(px, y)
			r.pdf.CellFormat(p.w, ln.h, p.text, "", 0, "L", false, 0, p.st.link)
			px += p.w
		}
		r.pdf.SetY(y + ln.h)
	}
}

// image places a picture on its own line, scaled to fit the page.
func (r *renderer) image(s span) {
	img, err := r.loadImage(s.image)
	if err != nil {
		label := strings.TrimSpace(s.text)
		if label == "" {
			label = s.image
		}
		reason := err.Error()
		if errors.Is(err, errSkipImage) {
			reason = strings.TrimPrefix(reason, errSkipImage.Error()+": ")
		}
		st := style{italic: true, size: bodySize * 0.9, color: colMuted}
		r.drawLines(r.breakLines([]span{{text: "[Image: " + label + " — " + reason + "]", st: st}}, r.right-r.left), r.left, r.right-r.left, "L")
		return
	}
	// Pixels at 96 dpi, never wider than the text or taller than a page.
	w := float64(img.width) * 25.4 / 96
	h := float64(img.height) * 25.4 / 96
	if maxW := r.right - r.left; w > maxW {
		h, w = h*maxW/w, maxW
	}
	if maxH := pageH - marginT - marginB - 4; h > maxH {
		w, h = w*maxH/h, maxH
	}
	r.ensure(h + 1)
	y := r.pdf.GetY() + 0.5
	r.pdf.ImageOptions(img.name, r.left, y, w, h, false, imageOpts(img.kind), 0, "")
	r.pdf.SetY(y + h + 1.5)
}
