package pdf

import (
	"strings"

	"github.com/go-pdf/fpdf"
	"github.com/yuin/goldmark/ast"
	east "github.com/yuin/goldmark/extension/ast"
)

func imageOpts(kind string) fpdf.ImageOptions { return fpdf.ImageOptions{ImageType: kind} }

// codeBlock draws preformatted text on a grey background, wrapping long
// lines.
func (r *renderer) codeBlock(n ast.Node) {
	const pad = 2.5
	st := style{mono: true, size: 8.8, color: colText}
	lh := lineHeight(st.size) * 0.92

	var b strings.Builder
	for i := 0; i < n.Lines().Len(); i++ {
		seg := n.Lines().At(i)
		b.Write(seg.Value(r.src))
	}
	src := strings.TrimRight(b.String(), "\n")

	width := r.right - r.left - 2*pad
	var lines []line
	for _, raw := range strings.Split(src, "\n") {
		// Keep indentation: tokenize would collapse it, so wrap by characters.
		text := cleanText(raw, familyMono)
		if text == "" {
			lines = append(lines, line{h: lh})
			continue
		}
		var part []rune
		pw := 0.0
		for _, c := range text {
			cw := r.measure(string(c), st)
			if pw+cw > width && len(part) > 0 {
				lines = append(lines, line{pieces: []piece{{text: string(part), st: st, w: pw}}, w: pw, h: lh})
				part, pw = nil, 0
			}
			part = append(part, c)
			pw += cw
		}
		lines = append(lines, line{pieces: []piece{{text: string(part), st: st, w: pw}}, w: pw, h: lh})
	}

	bg := func(y, h float64) {
		r.pdf.SetFillColor(colCodeBg[0], colCodeBg[1], colCodeBg[2])
		r.pdf.Rect(r.left, y, r.right-r.left, h, "F")
	}
	r.ensure(lh*2 + 2*pad)
	bg(r.pdf.GetY(), pad)
	r.pdf.SetY(r.pdf.GetY() + pad)
	for _, ln := range lines {
		if r.pdf.GetY()+lh > pageH-marginB {
			r.pdf.AddPage()
		}
		y := r.pdf.GetY()
		bg(y, lh)
		for _, p := range ln.pieces {
			r.setStyle(p.st)
			r.pdf.SetXY(r.left+pad, y)
			r.pdf.CellFormat(p.w, lh, p.text, "", 0, "L", false, 0, "")
		}
		r.pdf.SetY(y + lh)
	}
	bg(r.pdf.GetY(), pad)
	r.pdf.SetY(r.pdf.GetY() + pad)
	r.space(2.6)
}

type tableCell struct {
	spans []span
	align string
}

// table draws a GFM table with wrapped cells. Column widths follow the
// amount of text in each column; the header repeats after a page break.
func (r *renderer) table(t *east.Table) {
	const pad = 1.6
	size := 9.0
	var header []tableCell
	var rows [][]tableCell
	for row := t.FirstChild(); row != nil; row = row.NextSibling() {
		_, isHeader := row.(*east.TableHeader)
		var cells []tableCell
		for c := row.FirstChild(); c != nil; c = c.NextSibling() {
			cell, ok := c.(*east.TableCell)
			if !ok {
				continue
			}
			st := style{size: size, color: colText, bold: isHeader}
			spans := r.spans(cell, st)
			// Images do not fit in cells; keep their names.
			for i, s := range spans {
				if s.image != "" {
					spans[i] = span{text: "[" + firstNonEmpty(s.text, "image") + "]", st: style{size: size, italic: true, color: colMuted}}
				}
			}
			align := "L"
			switch cell.Alignment {
			case east.AlignCenter:
				align = "C"
			case east.AlignRight:
				align = "R"
			}
			cells = append(cells, tableCell{spans: spans, align: align})
		}
		if isHeader {
			header = cells
		} else {
			rows = append(rows, cells)
		}
	}
	cols := len(header)
	for _, row := range rows {
		cols = max(cols, len(row))
	}
	if cols == 0 {
		return
	}

	// Natural width of each column, then share the page width.
	avail := r.right - r.left
	natural := make([]float64, cols)
	for _, row := range append([][]tableCell{header}, rows...) {
		for i, c := range row {
			w := 0.0
			for _, ln := range r.breakLines(c.spans, 1e6) {
				w = max(w, ln.w)
			}
			natural[i] = max(natural[i], w+2*pad)
		}
	}
	total := 0.0
	for i := range natural {
		natural[i] = min(max(natural[i], 10), avail*0.6)
		total += natural[i]
	}
	widths := make([]float64, cols)
	for i := range widths {
		widths[i] = natural[i] / total * avail
	}

	layout := func(row []tableCell) ([][]line, float64) {
		cellLines := make([][]line, cols)
		h := lineHeight(size) + 2*pad
		for i := 0; i < cols && i < len(row); i++ {
			cellLines[i] = r.breakLines(row[i].spans, widths[i]-2*pad)
			ch := 2 * pad
			for _, ln := range cellLines[i] {
				ch += ln.h
			}
			h = max(h, ch)
		}
		return cellLines, h
	}
	drawRow := func(row []tableCell, isHeader bool) {
		cellLines, h := layout(row)
		maxH := pageH - marginT - marginB
		if r.pdf.GetY()+h > pageH-marginB && r.pdf.GetY() > marginT+0.5 {
			r.pdf.AddPage()
			if !isHeader && header != nil {
				hl, hh := layout(header)
				r.drawCells(header, hl, widths, hh, true, pad)
			}
		}
		if h > maxH {
			h = maxH // a giant cell is cut at the page end rather than overflowing
		}
		r.drawCells(row, cellLines, widths, h, isHeader, pad)
	}
	if header != nil {
		drawRow(header, true)
	}
	for _, row := range rows {
		drawRow(row, false)
	}
}

func (r *renderer) drawCells(row []tableCell, cellLines [][]line, widths []float64, h float64, isHeader bool, pad float64) {
	y := r.pdf.GetY()
	x := r.left
	r.pdf.SetDrawColor(colBorder[0], colBorder[1], colBorder[2])
	r.pdf.SetLineWidth(0.2)
	for i, w := range widths {
		if isHeader {
			r.pdf.SetFillColor(colHeaderBg[0], colHeaderBg[1], colHeaderBg[2])
			r.pdf.Rect(x, y, w, h, "FD")
		} else {
			r.pdf.Rect(x, y, w, h, "D")
		}
		if i < len(row) {
			ly := y + pad
			for _, ln := range cellLines[i] {
				if ly+ln.h > y+h-pad+0.01 {
					break
				}
				px := x + pad
				switch row[i].align {
				case "C":
					px += (w - 2*pad - ln.w) / 2
				case "R":
					px += w - 2*pad - ln.w
				}
				for _, p := range ln.pieces {
					r.setStyle(p.st)
					r.pdf.SetXY(px, ly)
					r.pdf.CellFormat(p.w, ln.h, p.text, "", 0, "L", false, 0, p.st.link)
					px += p.w
				}
				ly += ln.h
			}
		}
		x += w
	}
	r.pdf.SetY(y + h)
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
