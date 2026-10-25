package confluence

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strings"
)

// cloudClient uses the Confluence Cloud REST API v2.
type cloudClient struct {
	h    *httpClient
	host string // https://x.atlassian.net
	wiki string // https://x.atlassian.net/wiki
}

func (c *cloudClient) SiteURL() string                { return c.wiki }
func (c *cloudClient) IsCloud() bool                  { return true }
func (c *cloudClient) SetRateLimit(perSecond float64) { c.h.setRateLimit(perSecond) }

type v2Links struct {
	Next string `json:"next"`
}

// nextURL resolves a v2 "_links.next" value, which is relative to the host
// and usually already starts with /wiki.
func (c *cloudClient) nextURL(next string) string {
	if next == "" {
		return ""
	}
	if strings.HasPrefix(next, "http") {
		return next
	}
	if strings.HasPrefix(next, "/wiki/") {
		return c.host + next
	}
	return c.wiki + next
}

func (c *cloudClient) CurrentUser(ctx context.Context) (string, error) {
	var u struct {
		Type        string `json:"type"`
		DisplayName string `json:"displayName"`
	}
	if err := c.h.getJSON(ctx, c.wiki+"/rest/api/user/current", &u); err != nil {
		return "", err
	}
	if u.Type == "anonymous" {
		return "", &APIError{Status: 401}
	}
	return u.DisplayName, nil
}

func (c *cloudClient) ListSpaces(ctx context.Context) ([]Space, error) {
	var out []Space
	next := c.wiki + "/api/v2/spaces?limit=250&status=current"
	for next != "" {
		var resp struct {
			Results []struct {
				ID         string `json:"id"`
				Key        string `json:"key"`
				Name       string `json:"name"`
				Type       string `json:"type"`
				HomepageID string `json:"homepageId"`
			} `json:"results"`
			Links v2Links `json:"_links"`
		}
		if err := c.h.getJSON(ctx, next, &resp); err != nil {
			return nil, err
		}
		for _, s := range resp.Results {
			out = append(out, Space{ID: s.ID, Key: s.Key, Name: s.Name, Type: s.Type, HomepageID: s.HomepageID})
		}
		next = c.nextURL(resp.Links.Next)
	}
	sortSpaces(out)
	return out, nil
}

type v2Node struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	ParentID   string `json:"parentId"`
	ParentType string `json:"parentType"`
	Position   *int   `json:"position"`
}

func (c *cloudClient) ListPages(ctx context.Context, space Space, progress func(int)) ([]Page, error) {
	var pages []Page
	known := map[string]bool{}
	var nodes []v2Node

	next := fmt.Sprintf("%s/api/v2/spaces/%s/pages?limit=250&status=current", c.wiki, url.PathEscape(space.ID))
	for next != "" {
		var resp struct {
			Results []v2Node `json:"results"`
			Links   v2Links  `json:"_links"`
		}
		if err := c.h.getJSON(ctx, next, &resp); err != nil {
			return nil, err
		}
		for _, n := range resp.Results {
			pages = append(pages, toPage(n, KindPage))
			known[n.ID] = true
		}
		nodes = append(nodes, resp.Results...)
		if progress != nil {
			progress(len(pages))
		}
		next = c.nextURL(resp.Links.Next)
	}

	// Pages can live inside folders (and whiteboards, databases, ...), which
	// the pages endpoint does not return. Fetch those containers so the tree
	// keeps its exact shape.
	pending := map[string]string{} // container id -> container type
	queue := func(n v2Node) {
		if n.ParentID != "" && !known[n.ParentID] && n.ParentType != "" && n.ParentType != "page" {
			pending[n.ParentID] = n.ParentType
		}
	}
	for _, n := range nodes {
		queue(n)
	}
	for len(pending) > 0 {
		for id, typ := range pending {
			delete(pending, id)
			if known[id] {
				continue
			}
			known[id] = true
			n, err := c.getContainer(ctx, id, typ)
			if err != nil {
				// Unknown container: its pages attach to the space root instead.
				continue
			}
			pages = append(pages, toPage(n, KindFolder))
			queue(n)
		}
	}
	return pages, nil
}

func toPage(n v2Node, kind string) Page {
	p := Page{ID: n.ID, Title: n.Title, ParentID: n.ParentID, Kind: kind}
	if n.Position != nil {
		p.Position = *n.Position
	}
	return p
}

func (c *cloudClient) getContainer(ctx context.Context, id, typ string) (v2Node, error) {
	var endpoint string
	switch typ {
	case "folder":
		endpoint = "folders"
	case "whiteboard":
		endpoint = "whiteboards"
	case "database":
		endpoint = "databases"
	case "embed":
		endpoint = "embeds"
	default:
		return v2Node{}, errors.New("unsupported container type " + typ)
	}
	var n v2Node
	err := c.h.getJSON(ctx, fmt.Sprintf("%s/api/v2/%s/%s", c.wiki, endpoint, url.PathEscape(id)), &n)
	return n, err
}

func (c *cloudClient) PageHTML(ctx context.Context, pageID string) (string, error) {
	var lastErr error
	// export_view renders macros into plain HTML, which converts best.
	for _, format := range []string{"export_view", "view", "storage"} {
		var resp struct {
			Body map[string]struct {
				Value string `json:"value"`
			} `json:"body"`
		}
		u := fmt.Sprintf("%s/api/v2/pages/%s?body-format=%s", c.wiki, url.PathEscape(pageID), format)
		if err := c.h.getJSON(ctx, u, &resp); err != nil {
			var apiErr *APIError
			if errors.As(err, &apiErr) && apiErr.Status == 400 {
				lastErr = err
				continue
			}
			return "", err
		}
		if b, ok := resp.Body[format]; ok {
			return b.Value, nil
		}
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", nil
}

func (c *cloudClient) ListAttachments(ctx context.Context, pageID string) ([]Attachment, error) {
	var out []Attachment
	next := fmt.Sprintf("%s/api/v2/pages/%s/attachments?limit=250&status=current", c.wiki, url.PathEscape(pageID))
	for next != "" {
		var resp struct {
			Results []struct {
				ID           string `json:"id"`
				Title        string `json:"title"`
				MediaType    string `json:"mediaType"`
				FileSize     int64  `json:"fileSize"`
				DownloadLink string `json:"downloadLink"`
			} `json:"results"`
			Links v2Links `json:"_links"`
		}
		if err := c.h.getJSON(ctx, next, &resp); err != nil {
			return nil, err
		}
		for _, a := range resp.Results {
			dl := a.DownloadLink
			if !strings.HasPrefix(dl, "http") {
				dl = c.wiki + dl
			}
			out = append(out, Attachment{ID: a.ID, Title: a.Title, MediaType: a.MediaType, Size: a.FileSize, DownloadURL: dl})
		}
		next = c.nextURL(resp.Links.Next)
	}
	return out, nil
}

func (c *cloudClient) Download(ctx context.Context, rawURL string, w io.Writer) error {
	return c.h.download(ctx, rawURL, w)
}

func sortSpaces(s []Space) {
	sort.SliceStable(s, func(i, j int) bool {
		// Team spaces before personal spaces, then alphabetical.
		pi, pj := s[i].Type == "personal", s[j].Type == "personal"
		if pi != pj {
			return !pi
		}
		return strings.ToLower(s[i].Name) < strings.ToLower(s[j].Name)
	})
}
