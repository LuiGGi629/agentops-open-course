package conventions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderedLinksCannotEscapeOrTargetMissingFile(t *testing.T) {
	root := t.TempDir()
	site := filepath.Join(root, "site")
	page := filepath.Join(site, "chapter", "page.html")
	if err := os.MkdirAll(filepath.Dir(page), 0o750); err != nil {
		t.Fatal(err)
	}
	if message := checkRenderedLink(site, page, "../../docs/assets/font.txt"); !strings.Contains(message, "escapes") {
		t.Fatalf("escape message = %q", message)
	}
	if message := checkRenderedLink(site, page, "../assets/missing.txt"); !strings.Contains(message, "missing") {
		t.Fatalf("missing message = %q", message)
	}
}

func TestRenderedLinksRequireExistingFragments(t *testing.T) {
	root := t.TempDir()
	site := filepath.Join(root, "site")
	page := filepath.Join(site, "chapter", "index.html")
	target := filepath.Join(site, "target", "index.html")
	if err := os.MkdirAll(filepath.Dir(page), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(page, []byte(`<html><body><h2 id="local-fragment">Local</h2></body></html>`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(`<html><body><h2 id=target-fragment>Target</h2></body></html>`), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, href := range []string{"#local-fragment", "/target/#target-fragment"} {
		if message := checkRenderedLink(site, page, href); message != "" {
			t.Errorf("checkRenderedLink(%q) = %q, want success", href, message)
		}
	}
	if message := checkRenderedLink(site, page, "/target/#missing"); !strings.Contains(message, "fragment") {
		t.Fatalf("missing fragment message = %q", message)
	}
}

func TestCheckRenderedFindsHomepageMetadataAnd404Recovery(t *testing.T) {
	root := t.TempDir()
	site := filepath.Join(root, "site")
	client := filepath.Join(root, "clients", "web")
	if err := os.MkdirAll(filepath.Join(site, "assets"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(client, 0o750); err != nil {
		t.Fatal(err)
	}
	document := `<!doctype html><html lang="en"><head><meta name="description" content="x"></head><body><main><h1>Page</h1></main></body></html>`
	for _, name := range []string{"index.html", "404.html"} {
		if err := os.WriteFile(filepath.Join(site, name), []byte(document), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(client, "index.html"), []byte(`<html lang="en"><main><h1>x</h1><label>x</label><div aria-live="polite"></div><style>:focus-visible{} @media (max-width:1px){} @media (forced-colors: active){} @media (prefers-reduced-motion: reduce){}</style></main></html>`), 0o600); err != nil {
		t.Fatal(err)
	}
	problems := CheckRendered(root, site)
	messages := problemMessages(problems)
	if !strings.Contains(messages, "og:title") || !strings.Contains(messages, "route back home") {
		t.Fatalf("problems = %#v", problems)
	}
}

func TestParseRenderedUsesAccessibleImageAlternative(t *testing.T) {
	path := filepath.Join(t.TempDir(), "page.html")
	content := `<html lang="en"><main><h1>x</h1><a href="/"><img alt="Home"></a></main></html>`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	parsed, err := parseRendered(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.links) != 1 || parsed.links[0].name != "Home" {
		t.Fatalf("links = %#v", parsed.links)
	}
}

func TestCheckRenderedRoutesBindsSlugsToEveryPublishedSurface(t *testing.T) {
	root := t.TempDir()
	site := filepath.Join(root, "site")
	files := map[string]string{
		"hugo.toml": `baseURL = "https://example.test/"
[[permalinks]]
pattern = "/:sectionslugs/:slug/"
[permalinks.target]
kind = "page"
[[permalinks]]
pattern = "/:sectionslugs/"
[permalinks.target]
kind = "section"
`,
		"content/_index.md":                        "---\ntitle: Home\ndescription: x\n---\n",
		"content/2. Agents/_index.md":              "---\ntitle: Agents\ndescription: x\nslug: 2-agents\n---\n",
		"content/2. Agents/2.1. First Agent.md":    "---\ntitle: First\ndescription: x\nslug: 2-1-first-agent\n---\n",
		"site/index.html":                          renderedRouteFixture("https://example.test/", "/2-agents/", "/2-agents/2-1-first-agent/"),
		"site/2-agents/index.html":                 renderedRouteFixture("https://example.test/2-agents/", "/", "/2-agents/2-1-first-agent/"),
		"site/2-agents/2-1-first-agent/index.html": renderedRouteFixture("https://example.test/2-agents/2-1-first-agent/", "/", "/2-agents/"),
		"site/en.search-data.json": `{
  "/": {},
  "/2-agents/": {},
  "/2-agents/2-1-first-agent/": {},
  "/glossary": {}
}`,
		"site/sitemap.xml": `<?xml version="1.0"?><urlset><url><loc>https://example.test/</loc></url><url><loc>https://example.test/2-agents/</loc></url><url><loc>https://example.test/2-agents/2-1-first-agent/</loc></url></urlset>`,
	}
	for path, content := range files {
		fullPath := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	pages, err := loadPages(root)
	if err != nil {
		t.Fatal(err)
	}
	if problems := checkRenderedRoutes(root, site, pages); len(problems) != 0 {
		t.Fatalf("valid rendered routes problems = %#v", problems)
	}
	// An undeclared rendered page is still unaccounted for: nothing in content/ claims
	// it, so nothing keeps it correct.
	legacy := filepath.Join(site, "legacy.html")
	if err := os.WriteFile(legacy, []byte(renderedRouteFixture("https://example.test/legacy.html")), 0o600); err != nil {
		t.Fatal(err)
	}
	if messages := problemMessages(checkRenderedRoutes(root, site, pages)); !strings.Contains(messages, "neither a source permalink nor a declared alias") {
		t.Fatalf("undeclared rendered page was accepted: %s", messages)
	}

	// The same file becomes legitimate the moment a page declares it — that declaration
	// is what binds the historical route to the page responsible for still serving it.
	declared := make(pageSet, len(pages))
	for where, text := range pages {
		declared[where] = text
	}
	for where, text := range declared {
		if strings.HasSuffix(where, "2.1. First Agent.md") {
			// The opening delimiter has no newline before it, so this lands on the
			// closing one and leaves the front matter well formed.
			declared[where] = strings.Replace(text, "\n---\n", "\naliases:\n  - \"/legacy.html\"\n---\n", 1)
			break
		}
	}
	if problems := checkRenderedRoutes(root, site, declared); len(problems) != 0 {
		t.Fatalf("declared alias was rejected: %#v", problems)
	}
	if err := os.Remove(legacy); err != nil {
		t.Fatal(err)
	}
	broken := strings.Replace(files["site/2-agents/2-1-first-agent/index.html"], "https://example.test/2-agents/2-1-first-agent/", "https://example.test/wrong/", 1)
	if err := os.WriteFile(filepath.Join(site, "2-agents", "2-1-first-agent", "index.html"), []byte(broken), 0o600); err != nil {
		t.Fatal(err)
	}
	if messages := problemMessages(checkRenderedRoutes(root, site, pages)); !strings.Contains(messages, "canonical") {
		t.Fatalf("canonical drift was accepted: %s", messages)
	}
}

func renderedRouteFixture(canonical string, links ...string) string {
	var anchors strings.Builder
	for _, link := range links {
		anchors.WriteString(`<a href="` + link + `">Page</a>`)
	}
	return `<html lang="en"><head><link rel="canonical" href="` + canonical + `"><meta property="og:url" content="` + canonical + `"></head><body><main><h1>Page</h1>` + anchors.String() + `</main></body></html>`
}
