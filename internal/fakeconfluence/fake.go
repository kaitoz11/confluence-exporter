// Package fakeconfluence is an in-memory Confluence used by the tests. It
// serves the Cloud REST v2 and the Server/Data Center REST v1 endpoints that
// the exporter calls, over the same small space.
package fakeconfluence

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
)

const (
	Email    = "me@example.com"
	Token    = "tok"
	SpaceKey = "DOC"
)

type page struct {
	ID, Title, ParentID, ParentType string
	Position                        int
	Body                            string
}

type attachment struct {
	ID, PageID, Title, MediaType string
	Data                         []byte
}

// Fake holds the data set. Extra pages can be generated to try the UI on a
// bigger tree.
type Fake struct {
	pages       []page
	folders     []page
	attachments []attachment
	// RateLimitOnce makes the first attachment listing answer 429.
	RateLimitOnce bool
	limited       atomic.Bool
}

// New returns the standard data set plus extra generated pages.
func New(extra int) *Fake {
	f := &Fake{RateLimitOnce: true}
	f.pages = []page{
		{ID: "1", Title: "Home", Position: 0, Body: `<p>Welcome to <strong>Tiếng Việt</strong> docs.</p>
<p><img class="confluence-embedded-image" src="/wiki/download/attachments/1/diagram%20v2.png?version=1&amp;api=v2" data-image-src="/wiki/download/attachments/1/diagram%20v2.png?version=1&amp;api=v2"></p>
<p>See <a href="/wiki/spaces/DOC/pages/2/Getting+Started">getting started</a> and <a href="https://example.com/x">external</a>.</p>
<div class="confluence-information-macro confluence-information-macro-warning"><div class="confluence-information-macro-body"><p>Be careful</p></div></div>
<pre class="syntaxhighlighter-pre" data-syntaxhighlighter-params="brush: java; gutter: false">class A { int x = 1 &lt; 2; }</pre>
<table><tbody><tr><th>Name</th><th>Value</th></tr><tr><td><p>a</p><p>b</p></td><td>2</td></tr></tbody></table>`},
		{ID: "2", Title: "Getting Started", ParentID: "1", ParentType: "page", Position: 2,
			Body: `<p>Back to <a href="/wiki/spaces/DOC/pages/1/Home">home</a>, file <a href="/wiki/download/attachments/1/report.pdf">report</a>.</p>`},
		{ID: "3", Title: "FAQ: Q/A?", ParentID: "1", ParentType: "page", Position: 1, Body: `<p>Questions</p>`},
		{ID: "4", Title: "Inside folder", ParentID: "900", ParentType: "folder", Position: 0, Body: `<p>In a folder</p>`},
		{ID: "5", Title: "Getting Started", ParentID: "2", ParentType: "page", Position: 0, Body: `<p>Nested duplicate title</p>`},
	}
	f.folders = []page{{ID: "900", Title: "Team Folder", ParentID: "1", ParentType: "page", Position: 5}}
	f.attachments = []attachment{
		{ID: "a1", PageID: "1", Title: "diagram v2.png", MediaType: "image/png", Data: samplePNG()},
		{ID: "a2", PageID: "1", Title: "report.pdf", MediaType: "application/pdf", Data: []byte("%PDF-1.4 fake")},
	}
	for i := 0; i < extra; i++ {
		id := strconv.Itoa(1000 + i)
		parent := "2"
		if i >= 5 {
			parent = strconv.Itoa(1000 + (i-5)/4)
		}
		f.pages = append(f.pages, page{
			ID: id, Title: fmt.Sprintf("Section %d — notes", i+1), ParentID: parent, ParentType: "page", Position: i,
			Body: fmt.Sprintf(`<h2>Overview</h2><p>Generated page %d.</p><ul><li>one</li><li>two</li></ul>`, i+1),
		})
	}
	return f
}

func samplePNG() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 240, 80))
	for y := 0; y < 80; y++ {
		for x := 0; x < 240; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: 82, B: 204, A: 255})
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func (f *Fake) page(id string) *page {
	for i := range f.pages {
		if f.pages[i].ID == id {
			return &f.pages[i]
		}
	}
	return nil
}

func (f *Fake) pageAttachments(id string) []attachment {
	var out []attachment
	for _, a := range f.attachments {
		if a.PageID == id {
			out = append(out, a)
		}
	}
	return out
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (f *Fake) rateLimited(w http.ResponseWriter) bool {
	if f.RateLimitOnce && f.limited.CompareAndSwap(false, true) {
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
		return true
	}
	return false
}

func (f *Fake) serveDownload(w http.ResponseWriter, r *http.Request) {
	// .../download/attachments/<pageId>/<name>
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 2 {
		http.NotFound(w, r)
		return
	}
	pageID, name := parts[len(parts)-2], parts[len(parts)-1]
	for _, a := range f.pageAttachments(pageID) {
		if a.Title == name {
			w.Header().Set("Content-Type", a.MediaType)
			_, _ = w.Write(a.Data)
			return
		}
	}
	http.NotFound(w, r)
}

// Cloud serves the Confluence Cloud API (basic auth with email + token).
func (f *Fake) Cloud() http.Handler {
	mux := http.NewServeMux()
	auth := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if u, p, _ := r.BasicAuth(); u != Email || p != Token {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			h(w, r)
		}
	}
	node := func(p page) map[string]any {
		m := map[string]any{"id": p.ID, "title": p.Title, "position": p.Position, "parentId": nil, "parentType": nil}
		if p.ParentID != "" {
			m["parentId"], m["parentType"] = p.ParentID, p.ParentType
		}
		return m
	}
	mux.HandleFunc("/wiki/rest/api/user/current", auth(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"type": "known", "displayName": "Test User"})
	}))
	mux.HandleFunc("/wiki/api/v2/spaces", auth(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"results": []map[string]any{
			{"id": "77", "key": SpaceKey, "name": "Docs", "type": "global", "homepageId": "1"},
			{"id": "78", "key": "~me", "name": "Personal notes", "type": "personal", "homepageId": "1"},
		}})
	}))
	mux.HandleFunc("/wiki/api/v2/spaces/77/pages", auth(func(w http.ResponseWriter, r *http.Request) {
		// Pages of 3 results, to exercise cursor pagination.
		start, _ := strconv.Atoi(r.URL.Query().Get("cursor"))
		end := min(start+3, len(f.pages))
		var res []map[string]any
		for _, p := range f.pages[start:end] {
			res = append(res, node(p))
		}
		links := map[string]any{}
		if end < len(f.pages) {
			links["next"] = "/wiki/api/v2/spaces/77/pages?cursor=" + strconv.Itoa(end)
		}
		writeJSON(w, map[string]any{"results": res, "_links": links})
	}))
	mux.HandleFunc("/wiki/api/v2/folders/", auth(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/wiki/api/v2/folders/")
		for _, fo := range f.folders {
			if fo.ID == id {
				writeJSON(w, node(fo))
				return
			}
		}
		http.NotFound(w, r)
	}))
	mux.HandleFunc("/wiki/api/v2/pages/", auth(func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/wiki/api/v2/pages/")
		if id, ok := strings.CutSuffix(rest, "/attachments"); ok {
			if f.rateLimited(w) {
				return
			}
			var res []map[string]any
			for _, a := range f.pageAttachments(id) {
				res = append(res, map[string]any{
					"id": a.ID, "title": a.Title, "mediaType": a.MediaType, "fileSize": len(a.Data),
					"downloadLink": "/download/attachments/" + id + "/" + strings.ReplaceAll(a.Title, " ", "%20") + "?version=1&api=v2",
				})
			}
			writeJSON(w, map[string]any{"results": res})
			return
		}
		p := f.page(rest)
		if p == nil {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("body-format") != "export_view" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]any{"id": p.ID, "body": map[string]any{"export_view": map[string]any{"value": p.Body}}})
	}))
	mux.HandleFunc("/wiki/download/attachments/", auth(f.serveDownload))
	return mux
}

// Server serves the Confluence Server / Data Center API (bearer token),
// under the context path /confluence.
func (f *Fake) Server() http.Handler {
	const ctx = "/confluence"
	mux := http.NewServeMux()
	auth := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer "+Token {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			h(w, r)
		}
	}
	// Server bodies link with /display/ and /pages/viewpage.action URLs.
	serverBody := func(s string) string {
		s = strings.ReplaceAll(s, "/wiki/spaces/DOC/pages/2/Getting+Started", ctx+"/display/DOC/Getting+Started")
		s = strings.ReplaceAll(s, "/wiki/spaces/DOC/pages/1/Home", ctx+"/pages/viewpage.action?pageId=1")
		return strings.ReplaceAll(s, "/wiki/download/", ctx+"/download/")
	}
	ancestors := func(p page) []map[string]any {
		var chain []map[string]any
		for id := p.ParentID; id != ""; {
			pp := f.page(id)
			if pp == nil {
				// Server has no folders: attach to the folder's parent.
				for _, fo := range f.folders {
					if fo.ID == id {
						pp = f.page(fo.ParentID)
					}
				}
				if pp == nil {
					break
				}
			}
			chain = append([]map[string]any{{"id": pp.ID}}, chain...)
			id = pp.ParentID
		}
		return chain
	}
	page := func(r *http.Request) (int, int) {
		start, _ := strconv.Atoi(r.URL.Query().Get("start"))
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if limit == 0 {
			limit = 25
		}
		return start, limit
	}
	mux.HandleFunc(ctx+"/rest/api/user/current", auth(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"type": "known", "displayName": "Server User"})
	}))
	mux.HandleFunc(ctx+"/rest/api/space", auth(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"results": []map[string]any{
			{"id": 77, "key": SpaceKey, "name": "Docs", "type": "global", "homepage": map[string]any{"id": "1"}},
		}, "size": 1})
	}))
	mux.HandleFunc(ctx+"/rest/api/content", auth(func(w http.ResponseWriter, r *http.Request) {
		start, limit := page(r)
		end := min(start+limit, len(f.pages))
		start = min(start, end)
		var res []map[string]any
		for _, p := range f.pages[start:end] {
			res = append(res, map[string]any{
				"id": p.ID, "title": p.Title, "ancestors": ancestors(p),
				"extensions": map[string]any{"position": p.Position},
			})
		}
		writeJSON(w, map[string]any{"results": res, "size": len(res)})
	}))
	mux.HandleFunc(ctx+"/rest/api/content/", auth(func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, ctx+"/rest/api/content/")
		if id, ok := strings.CutSuffix(rest, "/child/attachment"); ok {
			if f.rateLimited(w) {
				return
			}
			var res []map[string]any
			for _, a := range f.pageAttachments(id) {
				res = append(res, map[string]any{
					"id": a.ID, "title": a.Title,
					"extensions": map[string]any{"mediaType": a.MediaType, "fileSize": len(a.Data)},
					"_links":     map[string]any{"download": "/download/attachments/" + id + "/" + strings.ReplaceAll(a.Title, " ", "%20") + "?version=1"},
				})
			}
			writeJSON(w, map[string]any{"results": res, "size": len(res)})
			return
		}
		p := f.page(rest)
		if p == nil {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, map[string]any{"id": p.ID, "body": map[string]any{"export_view": map[string]any{"value": serverBody(p.Body)}}})
	}))
	mux.HandleFunc(ctx+"/download/attachments/", auth(f.serveDownload))
	return mux
}
