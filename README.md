# Confluence Space Exporter

A friendly terminal app that copies a whole Confluence space to your computer:

- every page becomes a **Markdown** file, in folders that mirror the space's page tree exactly
- **attachments** (images and files) are downloaded next to their page, and images show inside the Markdown
- a **website** (`index.html`) lets you browse the export like Confluence, offline: page tree, search, images, files
- each page can also be saved as a **PDF**, and any folder of Markdown files can be converted to PDF later
- links between pages keep working offline, and a `Table of Contents.md` links to everything

Works with **Confluence Cloud** (`*.atlassian.net`) and **Confluence Server / Data Center**.

## For users

1. Download the file for your computer from the [latest release](https://github.com/kaitoz11/confluence-exporter/releases/latest):
   - Mac with Apple chip (M1 and newer): `confluence-exporter-macos-apple-silicon`
   - Mac with Intel chip: `confluence-exporter-macos-intel`
   - Windows: `confluence-exporter-windows-x64.exe` (or `-arm64.exe` on ARM laptops)
   - Linux: `confluence-exporter-linux-x64` / `confluence-exporter-linux-arm64`
2. Start it:
   - **Windows:** double-click `confluence-exporter-windows-x64.exe`.
   - **Mac:** open the Terminal app, type `chmod +x ` (with a space), drag the downloaded file into the window, press Enter. Then drag the file into the window again and press Enter.
     If macOS says the app can't be opened, right-click the file in Finder → Open → Open.
3. Follow the screens. The app walks you through everything:

   | Step | What you do |
   |------|-------------|
   | 1 Connect | Enter your Confluence address (you can paste any Confluence link), your email and an API token. |
   | 2 Space | Pick the space. Press `/` to search. |
   | 3 Pages | Everything is ticked. Press **Space** to untick pages you don't want. |
   | 4 Options | Choose the folder, attachments, website and PDFs. |
   | 5 Export | Watch the progress. When done, press **w** to open the website or **o** to open the folder. |

   Keys are always shown at the bottom of the screen. **Esc** goes back, **Ctrl+C** quits.

### Getting an API token

- **Confluence Cloud:** go to <https://id.atlassian.com/manage-profile/security/api-tokens> → *Create API token* → copy it.
  Sign in with your work email and this token.
- **Server / Data Center:** in Confluence click your profile picture → *Settings* → *Personal Access Tokens* → *Create token*.
  Leave the username empty. If your server has no tokens, use your username and password instead.

The token is only sent to your Confluence site. It is saved on your computer only if you tick *Remember my token*
(in a file readable only by you).

### Browsing the export (website)

Open `index.html` in the export folder with any web browser. It works without internet and without installing
anything:

- **Page tree** on the left, in the same order as the Confluence sidebar, with the current page highlighted
- **Search** box (press `/`) that looks in titles and page text and ignores accents: `huong dan` finds *Hướng dẫn*
- **Breadcrumbs**, **Previous / Next** buttons and an **On this page** outline for long pages
- **Images** open larger when clicked; **attachments** are shown as file cards you can click to open
- **Open in Confluence** button on each page, light and dark mode, and a phone-friendly layout

For an earlier export, choose **Create a website from Markdown files** in the main menu, or press **w** on the
final screen of an export that skipped it.

### PDFs

PDFs are built directly by the app from each Markdown file and the images in its `_attachments` folder.
Nothing else needs to be installed. Fonts that cover Vietnamese and other Latin, Greek and Cyrillic text are built in.
Characters these fonts lack, such as emoji or Chinese, Japanese and Korean, show as a small box (□).

### Speed

The **Speed** setting on the options screen (and on the PDF screen) keeps the computer responsive:

| Speed | Confluence requests | Pages downloaded at once | PDFs at once | Use it when |
|-------|---------------------|--------------------------|--------------|-------------|
| Gentle | up to 4 per second | 2 | 1, with a short rest between files | the computer is old or busy, or Confluence is slow |
| Normal | up to 10 per second | 4 | 1 | most of the time |
| Fast | no limit | 6 | up to 4 | a powerful computer and a quick export are needed |

In tests, PDF creation used about 40 MB of memory on Gentle and about 80 MB on Fast.

## What the export looks like

```
Docs (DOC)/
├── index.html                    ← open this to browse the space
├── Table of Contents.md
├── _viewer/                      ← website files (style, search index)
└── Home/
    ├── Home.md
    ├── Home.html
    ├── Home.pdf
    ├── _attachments/
    │   ├── diagram.png
    │   └── report.xlsx
    ├── Getting Started/
    │   ├── Getting Started.md
    │   ├── Getting Started.html
    │   └── Getting Started.pdf
    └── Team Folder/              ← Confluence folders become plain folders
        └── Onboarding/
            └── Onboarding.md
```

- Pages appear in the same order as in the Confluence sidebar (see `Table of Contents.md`).
- Characters that are not allowed in file names (`/ \ : * ? " < > |`) are replaced, and sibling pages with the same
  title get ` (2)`, ` (3)`… added.
- Running the same export again updates the pages and skips attachments that were already downloaded, so an
  interrupted export can simply be restarted.
- Problems with single pages don't stop the export. They are listed at the end and saved to `export-warnings.txt`.

## For IT / power users

```
confluence-exporter              start the interactive app
confluence-exporter html <folder> build the offline website (index.html) for an export folder (no UI)
confluence-exporter [--speed gentle|normal|fast] pdf <folder>
                                 convert every .md file under <folder> to PDF (no UI, for scripts)
confluence-exporter --version
```

Environment variables (optional): `CONFLUENCE_URL`, `CONFLUENCE_EMAIL` and `CONFLUENCE_API_TOKEN` pre-fill the
sign-in form.

Settings are stored in the user config folder (`~/Library/Application Support/confluence-exporter` on macOS,
`%AppData%\confluence-exporter` on Windows, `~/.config/confluence-exporter` on Linux).

### Building

Requires Go 1.26 or later.

```
make build    # bin/confluence-exporter
make test     # vet + tests, including real PDF rendering
make dist     # single-file binaries for macOS, Windows and Linux (x64 + ARM64) in dist/
```

### Releases (GitHub Actions)

- **CI** (`.github/workflows/ci.yml`): every pull request and branch is tested on Linux, macOS and Windows,
  and all binaries are built and attached to the run as an artifact.
- **Release** (`.github/workflows/release.yml`): every push to `main` is tested, built for all six platforms and
  published automatically as a GitHub release named `v<VERSION>.<run number>` (for example `v1.0.12`), with a
  `SHA256SUMS.txt`. Pushing a tag such as `v2.0.0` publishes that exact version instead. Changes to Markdown
  files alone do not trigger a release. To start a new series, edit the `VERSION` file (for example `1.1`).

### How it works

| Package | Role |
|---------|------|
| `internal/confluence` | REST client. Cloud uses API v2 (pages, folders, attachments, cursor pagination). Server/DC uses API v1. Retries on rate limits (429) and server errors. |
| `internal/export` | Builds the page tree, picks safe file names, converts each page's `export_view` HTML to Markdown, rewrites links to attachments and other pages into relative paths, and downloads attachments with 4 parallel workers. |
| `internal/pdf` | Pure-Go Markdown → PDF: goldmark parses the Markdown and a small layout engine draws it with fpdf (A4, page numbers, PDF outline, tables, code, images). Embedded DejaVu fonts (`internal/pdf/fonts`, free license). Large images are shrunk to keep memory low. |
| `internal/site` | Offline website: scans the export folder (order from `Table of Contents.md`), renders each page with goldmark (raw HTML is shown as text, never run), and writes one `.html` per page plus a shared `_viewer/` with CSS, plain JavaScript, the page tree and a search index. Works from `file://` with no server. |
| `internal/tui` | The step-by-step interface (Bubble Tea). |
| `internal/fakeconfluence` | In-memory Cloud and Server APIs used by the tests. |
