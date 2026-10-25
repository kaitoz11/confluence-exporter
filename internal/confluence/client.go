// Package confluence talks to the Confluence REST API. It supports Confluence
// Cloud (REST v2, email + API token) and Confluence Server / Data Center
// (REST v1, personal access token or username + password).
package confluence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Space is a Confluence space.
type Space struct {
	ID         string
	Key        string
	Name       string
	Type       string // global, personal, ...
	HomepageID string
}

// Node kinds in the content tree.
const (
	KindPage   = "page"
	KindFolder = "folder" // Cloud folders, whiteboards, databases: containers without a body
)

// Page is one node of the space content tree.
type Page struct {
	ID       string
	Title    string
	ParentID string
	Position int
	Kind     string
}

// Attachment is a file attached to a page.
type Attachment struct {
	ID          string
	Title       string
	MediaType   string
	Size        int64
	DownloadURL string // absolute
}

// Client is implemented by the Cloud and Server/Data Center clients.
type Client interface {
	// CurrentUser verifies the credentials and returns the user's display name.
	CurrentUser(ctx context.Context) (string, error)
	ListSpaces(ctx context.Context) ([]Space, error)
	// ListPages returns every page (and container) of the space. progress is
	// called with the running total as pages are discovered.
	ListPages(ctx context.Context, space Space, progress func(found int)) ([]Page, error)
	// PageHTML returns the rendered HTML of a page body.
	PageHTML(ctx context.Context, pageID string) (string, error)
	ListAttachments(ctx context.Context, pageID string) ([]Attachment, error)
	Download(ctx context.Context, rawURL string, w io.Writer) error
	// SiteURL is the base URL of the Confluence web UI (with /wiki on Cloud).
	SiteURL() string
	IsCloud() bool
	// SetRateLimit caps requests per second (0 = no limit), to stay gentle
	// on both Confluence and the network.
	SetRateLimit(perSecond float64)
}

// Credentials are what the user types into the login form.
type Credentials struct {
	URL   string
	Email string // Cloud: Atlassian account email. Server/DC: optional username.
	Token string // Cloud: API token. Server/DC: personal access token or password.
}

// Site is a parsed, normalized Confluence URL.
type Site struct {
	Base     string // scheme://host[/context-path], no trailing slash, no /wiki
	Cloud    bool
	SpaceKey string // set when the pasted URL pointed inside a space
}

var spaceKeyRe = regexp.MustCompile(`/(?:spaces|display)/([^/?#]+)`)

// ParseSiteURL accepts anything from "mycompany.atlassian.net" to a full page
// URL copied from the browser and returns the normalized site.
func ParseSiteURL(raw string) (Site, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Site{}, errors.New("please enter your Confluence address")
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return Site{}, fmt.Errorf("%q does not look like a web address", raw)
	}
	var s Site
	if m := spaceKeyRe.FindStringSubmatch(u.Path); m != nil {
		s.SpaceKey, _ = url.PathUnescape(m[1])
	}
	s.Cloud = strings.HasSuffix(strings.ToLower(u.Hostname()), ".atlassian.net")
	path := u.Path
	if s.Cloud {
		path = ""
	} else {
		// Keep a context path such as /confluence, drop everything after it.
		for _, marker := range []string{"/wiki", "/display/", "/spaces/", "/pages/", "/rest/", "/plugins/", "/dashboard.action", "/index.action"} {
			if i := strings.Index(path, marker); i >= 0 {
				path = path[:i]
			}
		}
		path = strings.TrimRight(path, "/")
	}
	s.Base = u.Scheme + "://" + u.Host + path
	return s, nil
}

// New builds the right client for the site.
func New(site Site, creds Credentials) Client {
	h := &httpClient{
		hc:    &http.Client{Timeout: 5 * time.Minute},
		token: strings.TrimSpace(creds.Token),
		user:  strings.TrimSpace(creds.Email),
	}
	if site.Cloud {
		h.basic = true
		return &cloudClient{h: h, host: site.Base, wiki: site.Base + "/wiki"}
	}
	h.basic = h.user != ""
	return &serverClient{h: h, base: site.Base}
}

// APIError is returned for non-2xx responses, with a message a non-technical
// user can act on.
type APIError struct {
	Status int
	URL    string
	Body   string
}

func (e *APIError) Error() string {
	switch e.Status {
	case http.StatusUnauthorized:
		return "sign-in failed: the email/username or API token is not correct"
	case http.StatusForbidden:
		return "access denied: your account does not have permission for this content"
	case http.StatusNotFound:
		return "not found (404): check the Confluence address, or the content was deleted"
	case http.StatusTooManyRequests:
		return "Confluence is rate limiting requests; please try again in a few minutes"
	}
	msg := strings.TrimSpace(e.Body)
	if len(msg) > 200 {
		msg = msg[:200] + "…"
	}
	return fmt.Sprintf("Confluence returned HTTP %d: %s", e.Status, msg)
}

type httpClient struct {
	hc    *http.Client
	basic bool
	user  string
	token string

	limitMu  sync.Mutex
	interval time.Duration // minimum time between requests
	next     time.Time     // earliest start of the next request
}

func (h *httpClient) setRateLimit(perSecond float64) {
	h.limitMu.Lock()
	defer h.limitMu.Unlock()
	h.interval = 0
	if perSecond > 0 {
		h.interval = time.Duration(float64(time.Second) / perSecond)
	}
}

// wait blocks until the rate limit allows another request.
func (h *httpClient) wait(ctx context.Context) error {
	h.limitMu.Lock()
	if h.interval == 0 {
		h.limitMu.Unlock()
		return nil
	}
	now := time.Now()
	start := h.next
	if start.Before(now) {
		start = now
	}
	h.next = start.Add(h.interval)
	h.limitMu.Unlock()
	return sleep(ctx, start.Sub(now))
}

const maxAttempts = 6

// get performs a GET with retries for rate limiting and transient errors.
// The caller must close the response body.
func (h *httpClient) get(ctx context.Context, rawURL string, accept string) (*http.Response, error) {
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			if err := sleep(ctx, backoff(attempt, lastErr)); err != nil {
				return nil, err
			}
		}
		if err := h.wait(ctx); err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return nil, err
		}
		if h.basic {
			req.SetBasicAuth(h.user, h.token)
		} else {
			req.Header.Set("Authorization", "Bearer "+h.token)
		}
		if accept != "" {
			req.Header.Set("Accept", accept)
		}
		req.Header.Set("User-Agent", "confluence-space-exporter")
		resp, err := h.hc.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			lastErr = err
			continue
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return resp, nil
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		apiErr := &APIError{Status: resp.StatusCode, URL: rawURL, Body: string(body)}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			lastErr = &retryAfterError{APIError: apiErr, after: retryAfter(resp)}
			continue
		}
		return nil, apiErr
	}
	var ra *retryAfterError
	if errors.As(lastErr, &ra) {
		return nil, ra.APIError
	}
	return nil, fmt.Errorf("could not reach Confluence: %w", lastErr)
}

func (h *httpClient) getJSON(ctx context.Context, rawURL string, out any) error {
	resp, err := h.get(ctx, rawURL, "application/json")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("unexpected response from Confluence (is the address correct?): %w", err)
	}
	return nil
}

func (h *httpClient) download(ctx context.Context, rawURL string, w io.Writer) error {
	resp, err := h.get(ctx, rawURL, "")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, err = io.Copy(w, resp.Body)
	return err
}

type retryAfterError struct {
	*APIError
	after time.Duration
}

func retryAfter(resp *http.Response) time.Duration {
	if s := resp.Header.Get("Retry-After"); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			return time.Duration(n) * time.Second
		}
	}
	return 0
}

func backoff(attempt int, lastErr error) time.Duration {
	var ra *retryAfterError
	if errors.As(lastErr, &ra) && ra.after > 0 {
		return ra.after
	}
	d := time.Duration(1<<attempt) * 500 * time.Millisecond
	return d + time.Duration(rand.Int63n(int64(d/2)+1))
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
