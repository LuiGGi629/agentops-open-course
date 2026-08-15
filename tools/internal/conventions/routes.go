package conventions

import (
	"encoding/json"
	"encoding/xml"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	toml "github.com/pelletier/go-toml"
)

const (
	pagePermalinkPattern    = "/:sectionslugs/:slug/"
	sectionPermalinkPattern = "/:sectionslugs/"
)

type pageRoute struct {
	Kind string
	Path string
}

type pageRoutes map[string]pageRoute

func frontMatterSlug(where, text string) (string, []Problem) {
	metadata, message := parseFrontMatter(text)
	if metadata == nil {
		return "", []Problem{problem(where, "%s", message)}
	}
	slug, ok := metadata["slug"].(string)
	if !ok || !permalinkSlug.MatchString(slug) {
		return "", []Problem{problem(where, "front matter slug must be explicit lowercase kebab-case, found %q", metadata["slug"])}
	}
	return slug, nil
}

func sectionRouteSegments(pages pageSet, where string) ([]string, []Problem) {
	relative := strings.TrimPrefix(where, "content/")
	directory := filepath.ToSlash(filepath.Dir(relative))
	if directory == "." {
		return nil, nil
	}
	parts := strings.Split(directory, "/")
	segments := make([]string, 0, len(parts))
	var problems []Problem
	for index := range parts {
		section := "content/" + strings.Join(parts[:index+1], "/") + "/_index.md"
		text, exists := pages[section]
		if !exists {
			problems = append(problems, problem(where, "parent section %s has no _index.md route authority", section))
			continue
		}
		slug, slugProblems := frontMatterSlug(section, text)
		problems = append(problems, slugProblems...)
		if slug != "" {
			segments = append(segments, slug)
		}
	}
	return segments, problems
}

func buildPageRoutes(pages pageSet) (pageRoutes, []Problem) {
	paths := make([]string, 0, len(pages))
	for where := range pages {
		paths = append(paths, where)
	}
	slices.Sort(paths)

	routes := make(pageRoutes, len(paths))
	owners := make(map[string]string, len(paths))
	slugOwners := make(map[string]string, len(paths))
	var problems []Problem
	for _, where := range paths {
		if where == landingPage {
			routes[where] = pageRoute{Kind: "home", Path: "/"}
			owners["/"] = where
			continue
		}
		segments, sectionProblems := sectionRouteSegments(pages, where)
		problems = append(problems, sectionProblems...)
		kind := "page"
		if filepath.Base(where) == "_index.md" {
			kind = "section"
		} else {
			slug, slugProblems := frontMatterSlug(where, pages[where])
			problems = append(problems, slugProblems...)
			if slug != "" {
				if owner, duplicate := slugOwners[slug]; duplicate {
					problems = append(problems, problem(where, "pages %s and %s use duplicate regular-page slug %q", owner, where, slug))
				} else {
					slugOwners[slug] = where
				}
				segments = append(segments, slug)
			}
		}
		route := "/" + strings.Join(segments, "/") + "/"
		if owner, duplicate := owners[route]; duplicate {
			problems = append(problems, problem(where, "pages %s and %s resolve to the same permalink %s", owner, where, route))
		} else {
			owners[route] = where
		}
		routes[where] = pageRoute{Kind: kind, Path: route}
	}
	return routes, problems
}

func checkPermalinkConfig(root string) []Problem {
	const where = "hugo.toml"
	document, err := toml.LoadFile(filepath.Join(root, where))
	if err != nil {
		return []Problem{problem(where, "could not read Hugo permalink configuration: %v", err)}
	}
	entries, ok := document.Get("permalinks").([]*toml.Tree)
	if !ok {
		return []Problem{problem(where, "permalinks must define page %q and section %q patterns", pagePermalinkPattern, sectionPermalinkPattern)}
	}
	found := make(map[string][]string)
	for _, entry := range entries {
		pattern, _ := entry.Get("pattern").(string)
		target, _ := entry.Get("target").(*toml.Tree)
		if target == nil {
			continue
		}
		kind, _ := target.Get("kind").(string)
		found[kind] = append(found[kind], pattern)
	}
	wanted := map[string]string{"page": pagePermalinkPattern, "section": sectionPermalinkPattern}
	var problems []Problem
	for kind, pattern := range wanted {
		patterns := found[kind]
		if len(patterns) != 1 || patterns[0] != pattern {
			problems = append(problems, problem(where, "%s permalinks must use exactly %q, found %q", kind, pattern, patterns))
		}
	}
	return problems
}

func routeFile(siteRoot, route string) string {
	if route == "/" {
		return filepath.Join(siteRoot, "index.html")
	}
	return filepath.Join(siteRoot, filepath.FromSlash(strings.Trim(route, "/")), "index.html")
}

func loadBaseURL(root string) string {
	document, err := toml.LoadFile(filepath.Join(root, "hugo.toml"))
	if err != nil {
		return ""
	}
	baseURL, _ := document.Get("baseURL").(string)
	if baseURL == "" {
		return ""
	}
	return strings.TrimRight(baseURL, "/") + "/"
}

func readSearchRoutes(siteRoot string) map[string]bool {
	paths, _ := filepath.Glob(filepath.Join(siteRoot, "*.search-data.json"))
	if len(paths) != 1 {
		return nil
	}
	content, err := os.ReadFile(paths[0])
	if err != nil {
		return nil
	}
	// The Hextra search artifact is an object keyed by each page's relative permalink.
	var values map[string]any
	if err := json.Unmarshal(content, &values); err != nil {
		return nil
	}
	routes := make(map[string]bool, len(values))
	for route := range values {
		// Hextra always adds this synthetic termbase entry. It has no content page,
		// canonical URL, sitemap entry, or slug authority, so it is not a course route.
		if route == "/glossary" {
			continue
		}
		routes[route] = true
	}
	return routes
}

func readSitemapRoutes(siteRoot string) map[string]bool {
	content, err := os.ReadFile(filepath.Join(siteRoot, "sitemap.xml"))
	if err != nil {
		return nil
	}
	var sitemap struct {
		URLs []struct {
			Location string `xml:"loc"`
		} `xml:"url"`
	}
	if err := xml.Unmarshal(content, &sitemap); err != nil {
		return nil
	}
	routes := make(map[string]bool, len(sitemap.URLs))
	for _, entry := range sitemap.URLs {
		parsed, parseErr := url.Parse(entry.Location)
		if parseErr == nil {
			routes[parsed.EscapedPath()] = true
		}
	}
	return routes
}

func routeFromRenderedLink(href string) string {
	parsed, err := url.Parse(href)
	if err != nil || parsed.Scheme != "" || parsed.Host != "" || !strings.HasPrefix(parsed.Path, "/") {
		return ""
	}
	path, err := url.PathUnescape(parsed.Path)
	if err != nil {
		return ""
	}
	if path != "/" && !strings.HasSuffix(path, "/") {
		return ""
	}
	return path
}

func checkRenderedRoutes(root, siteRoot string, pages pageSet) []Problem {
	routes, problems := buildPageRoutes(pages)
	baseURL := loadBaseURL(root)
	if baseURL == "" {
		problems = append(problems, problem("hugo.toml", "baseURL must be set to verify canonical page metadata"))
	}
	searchRoutes := readSearchRoutes(siteRoot)
	if searchRoutes == nil {
		problems = append(problems, problem("site/*.search-data.json", "rendered search route index is missing or invalid"))
	}
	sitemapRoutes := readSitemapRoutes(siteRoot)
	if sitemapRoutes == nil {
		problems = append(problems, problem("site/sitemap.xml", "rendered sitemap is missing or invalid"))
	}

	expectedFiles := make(map[string]string, len(routes))
	navigationRoutes := make(map[string]bool, len(routes))
	for source, route := range routes {
		path := routeFile(siteRoot, route.Path)
		absolute, _ := filepath.Abs(path)
		expectedFiles[absolute] = source
		parsed, err := parseRendered(path)
		if err != nil {
			problems = append(problems, problem(source, "expected permalink %s did not render to %s", route.Path, relative(root, path)))
			continue
		}
		expectedCanonical := strings.TrimRight(baseURL, "/") + route.Path
		if parsed.canonical != expectedCanonical {
			problems = append(problems, problem(source, "rendered canonical URL must be %q, found %q", expectedCanonical, parsed.canonical))
		}
		if parsed.meta["og:url"] != expectedCanonical {
			problems = append(problems, problem(source, "rendered Open Graph URL must be %q, found %q", expectedCanonical, parsed.meta["og:url"]))
		}
		if searchRoutes != nil && !searchRoutes[route.Path] {
			problems = append(problems, problem(source, "permalink %s is absent from the rendered search index", route.Path))
		}
		if sitemapRoutes != nil && !sitemapRoutes[route.Path] {
			problems = append(problems, problem(source, "permalink %s is absent from the rendered sitemap", route.Path))
		}
		for _, link := range parsed.links {
			if target := routeFromRenderedLink(link.href); target != "" {
				navigationRoutes[target] = true
			}
		}
	}
	for source, route := range routes {
		if !navigationRoutes[route.Path] {
			problems = append(problems, problem(source, "permalink %s is absent from rendered navigation and page links", route.Path))
		}
	}

	_ = filepath.WalkDir(siteRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || filepath.Ext(path) != ".html" {
			return nil
		}
		absolute, _ := filepath.Abs(path)
		if _, expected := expectedFiles[absolute]; expected || filepath.Base(path) == "404.html" {
			return nil
		}
		problems = append(problems, problem(relative(siteRoot, path), "rendered HTML page has no source slug authority; aliases and legacy routes are not allowed"))
		return nil
	})
	if searchRoutes != nil && len(searchRoutes) != len(routes) {
		problems = append(problems, problem("site/*.search-data.json", "search route count is %d, want %d source permalinks", len(searchRoutes), len(routes)))
	}
	if sitemapRoutes != nil && len(sitemapRoutes) != len(routes) {
		problems = append(problems, problem("site/sitemap.xml", "sitemap route count is %d, want %d source permalinks", len(sitemapRoutes), len(routes)))
	}
	return problems
}

func renderedRouteSource(root, renderedRelative string) string {
	if !strings.HasSuffix(renderedRelative, "index.html") {
		return ""
	}
	pages, err := loadPages(root)
	if err != nil {
		return ""
	}
	routes, _ := buildPageRoutes(pages)
	renderedPath := filepath.ToSlash(renderedRelative)
	for source, route := range routes {
		if filepath.ToSlash(strings.TrimPrefix(routeFile("", route.Path), string(filepath.Separator))) == renderedPath {
			return strings.TrimPrefix(source, "content/")
		}
	}
	return ""
}

func checkSourceFragments(siteRoot string, pages pageSet) []Problem {
	routes, _ := buildPageRoutes(pages)
	pattern := regexp.MustCompile(`\{\{<\s+relref\s+"([^"#]+)#([^"#]+)"[^}]*>\}\}`)
	var problems []Problem
	for where, text := range pages {
		for _, line := range linesOutsideFences(text) {
			for _, match := range pattern.FindAllStringSubmatch(line.text, -1) {
				target := "content/" + strings.TrimPrefix(match[1], "/")
				route, exists := routes[target]
				if !exists {
					problems = append(problems, problem(where, "line %d: relref fragment target does not exist: %s", line.number, match[1]))
					continue
				}
				page, err := parseRendered(routeFile(siteRoot, route.Path))
				if err != nil {
					problems = append(problems, problem(where, "line %d: rendered relref target is missing: %s", line.number, route.Path))
					continue
				}
				if !page.ids[match[2]] {
					problems = append(problems, problem(where, "line %d: rendered relref target %s has no fragment %q", line.number, route.Path, match[2]))
				}
			}
		}
	}
	return problems
}
