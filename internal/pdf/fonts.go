package pdf

import (
	_ "embed"
	"strings"
	"sync"
	"unicode"

	"github.com/go-pdf/fpdf"
	"golang.org/x/image/font/sfnt"
)

// DejaVu fonts (see fonts/LICENSE) cover Latin including Vietnamese, Greek
// and Cyrillic, plus common symbols.
var (
	//go:embed fonts/DejaVuSansCondensed.ttf
	sansRegular []byte
	//go:embed fonts/DejaVuSansCondensed-Bold.ttf
	sansBold []byte
	//go:embed fonts/DejaVuSansCondensed-Oblique.ttf
	sansItalic []byte
	//go:embed fonts/DejaVuSansCondensed-BoldOblique.ttf
	sansBoldItalic []byte
	//go:embed fonts/DejaVuSansMono.ttf
	monoRegular []byte
	//go:embed fonts/DejaVuSansMono-Bold.ttf
	monoBold []byte
)

const (
	familySans = "sans"
	familyMono = "mono"
)

func addFonts(pdf *fpdf.Fpdf) {
	pdf.AddUTF8FontFromBytes(familySans, "", sansRegular)
	pdf.AddUTF8FontFromBytes(familySans, "B", sansBold)
	pdf.AddUTF8FontFromBytes(familySans, "I", sansItalic)
	pdf.AddUTF8FontFromBytes(familySans, "BI", sansBoldItalic)
	pdf.AddUTF8FontFromBytes(familyMono, "", monoRegular)
	pdf.AddUTF8FontFromBytes(familyMono, "B", monoBold)
}

// coverage remembers which characters a font can draw. Characters it cannot
// draw are replaced before they reach fpdf, which would otherwise print
// nothing or fail.
type coverage struct {
	font  *sfnt.Font
	mu    sync.Mutex
	buf   sfnt.Buffer
	known map[rune]bool
}

var (
	coverOnce sync.Once
	covers    map[string]*coverage
)

func coverageFor(family string) *coverage {
	coverOnce.Do(func() {
		covers = map[string]*coverage{}
		for name, data := range map[string][]byte{familySans: sansRegular, familyMono: monoRegular} {
			f, err := sfnt.Parse(data)
			if err != nil {
				panic("embedded font: " + err.Error())
			}
			covers[name] = &coverage{font: f, known: map[rune]bool{}}
		}
	})
	return covers[family]
}

func (c *coverage) has(r rune) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if v, ok := c.known[r]; ok {
		return v
	}
	i, err := c.font.GlyphIndex(&c.buf, r)
	v := err == nil && i != 0
	c.known[r] = v
	return v
}

// cleanText makes text safe to draw with the family's font.
func cleanText(s, family string) string {
	cov := coverageFor(family)
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\t':
			b.WriteString("    ")
		case r == 0x00A0 || r == 0x2028 || r == 0x2029: // no-break space, line/paragraph separators
			b.WriteRune(' ')
		case r == 0x200B || r == 0x200C || r == 0x200D || r == 0xFEFF || r == 0xFE0F:
			// zero-width characters and the emoji variation selector
		case unicode.IsControl(r), unicode.Is(unicode.Mn, r) && !cov.has(r):
			// control characters and combining marks the font cannot draw
		case cov.has(r):
			b.WriteRune(r)
		default:
			// Emoji, CJK and other scripts the font lacks.
			b.WriteRune(0x25A1) // white square
		}
	}
	return b.String()
}
