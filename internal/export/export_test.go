package export

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/kaitoz11/confluence-exporter/internal/confluence"
	"github.com/kaitoz11/confluence-exporter/internal/fakeconfluence"
)

func TestExportCloud(t *testing.T) {
	srv := httptest.NewServer(fakeconfluence.New(0).Cloud())
	defer srv.Close()
	client := confluence.New(confluence.Site{Base: srv.URL, Cloud: true}, confluence.Credentials{Email: fakeconfluence.Email, Token: fakeconfluence.Token})
	exportAndCheck(t, client, "Test User", true)
}

func TestExportServer(t *testing.T) {
	srv := httptest.NewServer(fakeconfluence.New(0).Server())
	defer srv.Close()
	site, err := confluence.ParseSiteURL(srv.URL + "/confluence/display/DOC/Home")
	if err != nil || site.Cloud || site.SpaceKey != "DOC" {
		t.Fatalf("ParseSiteURL = %+v, %v", site, err)
	}
	client := confluence.New(site, confluence.Credentials{Token: fakeconfluence.Token})
	exportAndCheck(t, client, "Server User", false)
}

func TestBadCredentials(t *testing.T) {
	srv := httptest.NewServer(fakeconfluence.New(0).Cloud())
	defer srv.Close()
	client := confluence.New(confluence.Site{Base: srv.URL, Cloud: true}, confluence.Credentials{Email: "x", Token: "wrong"})
	_, err := client.CurrentUser(context.Background())
	if err == nil || !strings.Contains(err.Error(), "API token is not correct") {
		t.Fatalf("err = %v", err)
	}
}

func exportAndCheck(t *testing.T, client confluence.Client, wantUser string, cloud bool) {
	ctx := context.Background()
	user, err := client.CurrentUser(ctx)
	if err != nil || user != wantUser {
		t.Fatalf("CurrentUser = %q, %v", user, err)
	}
	spaces, err := client.ListSpaces(ctx)
	if err != nil || len(spaces) == 0 || spaces[0].Key != fakeconfluence.SpaceKey {
		t.Fatalf("ListSpaces = %v, %v", spaces, err)
	}
	pages, err := client.ListPages(ctx, spaces[0], nil)
	if err != nil {
		t.Fatal(err)
	}
	tree := BuildTree(spaces[0], pages)

	out := t.TempDir()
	if dir := os.Getenv("EXPORT_TEST_OUT"); dir != "" {
		out = filepath.Join(dir, t.Name()) // keep the result for manual inspection
	}
	var mu sync.Mutex
	var events []Event
	res := Run(ctx, client, tree, Options{OutputDir: out, Attachments: true, PDF: true, Speed: SpeedFast}, func(e Event) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
	})
	if len(res.Warnings) > 0 {
		t.Fatalf("warnings: %v", res.Warnings)
	}
	if res.Pages != 5 || res.Attachments != 2 {
		t.Fatalf("result = %+v", res)
	}

	var files []string
	_ = filepath.Walk(res.Root, func(p string, fi os.FileInfo, err error) error {
		if !fi.IsDir() {
			rel, _ := filepath.Rel(res.Root, p)
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Strings(files)
	t.Logf("files:\n  %s", strings.Join(files, "\n  "))

	inside := "Home/Team Folder/Inside folder/Inside folder.md"
	if !cloud {
		inside = "Home/Inside folder/Inside folder.md" // Server has no folders
	}
	want := []string{
		"Table of Contents.md",
		"Home/Home.md",
		"Home/_attachments/diagram v2.png",
		"Home/_attachments/report.pdf",
		"Home/FAQ - Q-A/FAQ - Q-A.md",
		"Home/Getting Started/Getting Started.md",
		"Home/Getting Started/Getting Started/Getting Started.md",
		inside,
	}
	want = append(want, "Home/Home.pdf", strings.TrimSuffix(inside, ".md")+".pdf")
	have := map[string]bool{}
	for _, f := range files {
		have[f] = true
	}
	for _, w := range want {
		if !have[w] {
			t.Errorf("missing %s", w)
		}
	}

	home, _ := os.ReadFile(filepath.Join(res.Root, "Home", "Home.md"))
	md := string(home)
	t.Logf("Home.md:\n%s", md)
	for _, s := range []string{
		"# Home",
		"Tiếng Việt",
		"](_attachments/diagram%20v2.png)",
		"](Getting%20Started/Getting%20Started.md)",
		"](https://example.com/x)",
		"**Warning**",
		"```java",
		"int x = 1 < 2;",
		"| Name",
		"| a<br />b |",
		"## Attachments",
	} {
		if !strings.Contains(md, s) {
			t.Errorf("Home.md lacks %q", s)
		}
	}
	gs, _ := os.ReadFile(filepath.Join(res.Root, "Home", "Getting Started", "Getting Started.md"))
	for _, s := range []string{"](../Home.md)", "](../_attachments/report.pdf)"} {
		if !strings.Contains(string(gs), s) {
			t.Errorf("Getting Started.md lacks %q:\n%s", s, gs)
		}
	}
	if cloud {
		toc, _ := os.ReadFile(filepath.Join(res.Root, TOCFile))
		if !strings.Contains(string(toc), "**Team Folder**") {
			t.Errorf("TOC lacks the folder:\n%s", toc)
		}
	}

	// Sidebar order: FAQ (position 1), Getting Started (2), then position 5.
	var order []string
	for _, c := range tree.ByID["1"].Children {
		order = append(order, c.Title)
	}
	last := "Team Folder"
	if !cloud {
		last = "Inside folder"
	}
	if fmt.Sprint(order) != fmt.Sprintf("[FAQ: Q/A? Getting Started %s]", last) && cloud {
		t.Errorf("child order = %v", order)
	}

	// Running again must reuse the downloaded attachments and succeed.
	res2 := Run(ctx, client, tree, Options{OutputDir: out, Attachments: true, Speed: SpeedGentle}, func(Event) {})
	if len(res2.Warnings) > 0 || res2.Pages != 5 {
		t.Fatalf("second run = %+v", res2)
	}
}

func TestSanitizeName(t *testing.T) {
	cases := map[string]string{
		"FAQ: Q/A?":           "FAQ - Q-A",
		"  trailing dots... ": "trailing dots",
		"CON":                 "_CON",
		"":                    "Untitled",
		`a<b>c*d|e"f`:         "abcd-e'f",
		"Tiếng Việt 🚀":        "Tiếng Việt 🚀",
	}
	for in, want := range cases {
		if got := SanitizeName(in); got != want {
			t.Errorf("SanitizeName(%q) = %q, want %q", in, got, want)
		}
	}
	used := map[string]bool{}
	if a, b := UniqueName("Page", used), UniqueName("page", used); a != "Page" || b != "page (2)" {
		t.Errorf("UniqueName = %q, %q", a, b)
	}
}

func TestParseSiteURL(t *testing.T) {
	cases := []struct {
		in, base, key string
		cloud         bool
	}{
		{"acme.atlassian.net", "https://acme.atlassian.net", "", true},
		{"https://acme.atlassian.net/wiki/spaces/ENG/pages/123/Title", "https://acme.atlassian.net", "ENG", true},
		{"https://wiki.corp.com/confluence/display/OPS/Runbook", "https://wiki.corp.com/confluence", "OPS", false},
		{"https://wiki.corp.com/pages/viewpage.action?pageId=1", "https://wiki.corp.com", "", false},
		{"http://localhost:8090/", "http://localhost:8090", "", false},
	}
	for _, c := range cases {
		s, err := confluence.ParseSiteURL(c.in)
		if err != nil || s.Base != c.base || s.SpaceKey != c.key || s.Cloud != c.cloud {
			t.Errorf("ParseSiteURL(%q) = %+v, %v", c.in, s, err)
		}
	}
}
