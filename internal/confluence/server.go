package confluence

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
)

// serverClient uses the REST API v1 of Confluence Server and Data Center.
type serverClient struct {
	h    *httpClient
	base string // https://confluence.example.com[/context]
}

func (c *serverClient) SiteURL() string                { return c.base }
func (c *serverClient) IsCloud() bool                  { return false }
func (c *serverClient) SetRateLimit(perSecond float64) { c.h.setRateLimit(perSecond) }

const v1PageSize = 100

func (c *serverClient) CurrentUser(ctx context.Context) (string, error) {
	var u struct {
		Type        string `json:"type"`
		DisplayName string `json:"displayName"`
	}
	if err := c.h.getJSON(ctx, c.base+"/rest/api/user/current", &u); err != nil {
		return "", err
	}
	if u.Type == "anonymous" {
		return "", &APIError{Status: 401}
	}
	return u.DisplayName, nil
}

func (c *serverClient) ListSpaces(ctx context.Context) ([]Space, error) {
	var out []Space
	for start := 0; ; start += v1PageSize {
		var resp struct {
			Results []struct {
				ID       json.Number `json:"id"`
				Key      string      `json:"key"`
				Name     string      `json:"name"`
				Type     string      `json:"type"`
				Homepage *struct {
					ID string `json:"id"`
				} `json:"homepage"`
			} `json:"results"`
			Size int `json:"size"`
		}
		u := fmt.Sprintf("%s/rest/api/space?limit=%d&start=%d&expand=homepage", c.base, v1PageSize, start)
		if err := c.h.getJSON(ctx, u, &resp); err != nil {
			return nil, err
		}
		for _, s := range resp.Results {
			sp := Space{ID: s.ID.String(), Key: s.Key, Name: s.Name, Type: s.Type}
			if s.Homepage != nil {
				sp.HomepageID = s.Homepage.ID
			}
			out = append(out, sp)
		}
		if len(resp.Results) < v1PageSize {
			break
		}
	}
	sortSpaces(out)
	return out, nil
}

func (c *serverClient) ListPages(ctx context.Context, space Space, progress func(int)) ([]Page, error) {
	var pages []Page
	for start := 0; ; start += v1PageSize {
		var resp struct {
			Results []struct {
				ID         string `json:"id"`
				Title      string `json:"title"`
				Extensions struct {
					Position json.RawMessage `json:"position"`
				} `json:"extensions"`
				Ancestors []struct {
					ID string `json:"id"`
				} `json:"ancestors"`
			} `json:"results"`
		}
		u := fmt.Sprintf("%s/rest/api/content?spaceKey=%s&type=page&status=current&expand=ancestors&limit=%d&start=%d",
			c.base, url.QueryEscape(space.Key), v1PageSize, start)
		if err := c.h.getJSON(ctx, u, &resp); err != nil {
			return nil, err
		}
		for _, r := range resp.Results {
			p := Page{ID: r.ID, Title: r.Title, Kind: KindPage, Position: -1}
			if n := len(r.Ancestors); n > 0 {
				p.ParentID = r.Ancestors[n-1].ID
			}
			// position is a number, or the string "none" when unordered.
			if v, err := strconv.Atoi(strings.Trim(string(r.Extensions.Position), `"`)); err == nil {
				p.Position = v
			}
			pages = append(pages, p)
		}
		if progress != nil {
			progress(len(pages))
		}
		if len(resp.Results) < v1PageSize {
			break
		}
	}
	return pages, nil
}

func (c *serverClient) PageHTML(ctx context.Context, pageID string) (string, error) {
	var resp struct {
		Body struct {
			ExportView struct {
				Value string `json:"value"`
			} `json:"export_view"`
		} `json:"body"`
	}
	u := fmt.Sprintf("%s/rest/api/content/%s?expand=body.export_view", c.base, url.PathEscape(pageID))
	if err := c.h.getJSON(ctx, u, &resp); err != nil {
		return "", err
	}
	return resp.Body.ExportView.Value, nil
}

func (c *serverClient) ListAttachments(ctx context.Context, pageID string) ([]Attachment, error) {
	var out []Attachment
	for start := 0; ; start += v1PageSize {
		var resp struct {
			Results []struct {
				ID         string `json:"id"`
				Title      string `json:"title"`
				Extensions struct {
					MediaType string `json:"mediaType"`
					FileSize  int64  `json:"fileSize"`
				} `json:"extensions"`
				Links struct {
					Download string `json:"download"`
				} `json:"_links"`
			} `json:"results"`
		}
		u := fmt.Sprintf("%s/rest/api/content/%s/child/attachment?limit=%d&start=%d",
			c.base, url.PathEscape(pageID), v1PageSize, start)
		if err := c.h.getJSON(ctx, u, &resp); err != nil {
			return nil, err
		}
		for _, a := range resp.Results {
			dl := a.Links.Download
			if !strings.HasPrefix(dl, "http") {
				dl = c.base + dl
			}
			out = append(out, Attachment{ID: a.ID, Title: a.Title, MediaType: a.Extensions.MediaType, Size: a.Extensions.FileSize, DownloadURL: dl})
		}
		if len(resp.Results) < v1PageSize {
			break
		}
	}
	return out, nil
}

func (c *serverClient) Download(ctx context.Context, rawURL string, w io.Writer) error {
	return c.h.download(ctx, rawURL, w)
}
