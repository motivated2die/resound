package main

import (
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
)

var helpSrc = os.DirFS(filepath.Join("..", "..", "webapp", "help"))

// The real help center builds: every article is in SUMMARY.md, and every
// link, heading and visualization it uses exists.
func TestHelpCenter(t *testing.T) {
	mdLink := regexp.MustCompile(`<a [^>]*href="[^":]*\.md[#"]`) // not https://…/PRIVACY.md, nor the Markdown <link>
	files, err := buildHelp(helpSrc)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 10 {
		t.Fatalf("only %d files", len(files))
	}
	for name, data := range files {
		if !strings.HasSuffix(name, ".html") {
			continue
		}
		page := string(data)
		if strings.Count(page, "<h1") != 1 {
			t.Errorf("%s: wants exactly one <h1>", name)
		}
		if m := mdLink.FindString(page); m != "" {
			t.Errorf("%s: a link to a .md file was not rewritten: %s", name, m)
		}
		if strings.Contains(page, ":::") {
			t.Errorf("%s: an unclosed or unknown ::: block", name)
		}
	}
}

// The help articles the desktop panel opens (the help… constants in
// internal/app/pages.go, e.g. "voicemeeter#before-you-install-vb-cable")
// exist, with their headings.
func TestAppHelpLinks(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "internal", "app", "pages.go"))
	if err != nil {
		t.Fatal(err)
	}
	links := regexp.MustCompile(`(?m)^\s*help[A-Z]\w*\s*=\s*"([^"]+)"`).FindAllStringSubmatch(string(src), -1)
	if len(links) == 0 {
		t.Fatal("no help… constants found in internal/app/pages.go")
	}
	files, err := buildHelp(helpSrc)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range links {
		slug, frag, _ := strings.Cut(l[1], "#")
		page, ok := files["help/"+slug+".html"]
		switch {
		case !ok:
			t.Errorf("%q: no help article %s.md", l[1], slug)
		case frag != "" && !strings.Contains(string(page), `id="`+frag+`"`):
			t.Errorf("%q: %s.md has no such heading", l[1], slug)
		}
	}
}

// Like the app, the help center loads nothing from other hosts (the CSP
// would refuse it): no URLs in its styles, scripts, template or
// visualizations, other than links the reader follows.
func TestHelpSelfContained(t *testing.T) {
	url := regexp.MustCompile(`(https?:)?//[a-z0-9-]+(\.[a-z0-9-]+)+[/"'\s)]`)
	link := regexp.MustCompile(`<a [^>]*href="https://[^"]*"`)
	err := fs.WalkDir(helpSrc, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasSuffix(path, ".md") {
			return err
		}
		data, err := fs.ReadFile(helpSrc, path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(data), "\n") {
			line = link.ReplaceAllString(line, "<a")
			if m := url.FindString(line); m != "" && !strings.Contains(line, "www.w3.org/2000/svg") {
				t.Errorf("%s:%d references %q", path, i+1, m)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// The building blocks: links between articles, callouts, visualizations,
// checklists, and the checks that keep the help center consistent.
func TestHelpMarkdown(t *testing.T) {
	base := fstest.MapFS{
		"page.html":     {Data: []byte(`<h1>{{.Title}}</h1>{{.Body}}{{with .Next}}<a class="next" href="/help/{{.Slug}}">{{.Label}}</a>{{end}}`)},
		"help.css":      {Data: []byte("")},
		"help.js":       {Data: []byte("")},
		"ai-prompt.txt": {Data: []byte("Help me with Resound.\n\n{{documentation}}\n\n## My question\n")},
		"viz/demo.html": {Data: []byte(`<div class="demo"></div>`)},
		"SUMMARY.md":    {Data: []byte("# Help\n\n## Start\n- [First steps](one.md)\n- [More](two.md)\n")},
		"index.md":      {Data: []byte("# Help\n\nWelcome. See [one](one.md).\n")},
		"one.md": {Data: []byte("# One\n\nIntro text.\n\n## A heading\n\nSee [two](two.md#later) and [here](#a-heading).\n\n" +
			":::tip Look\nA **tip**.\n:::\n\n:::viz demo\nThe caption.\n:::\n\n- [ ] First\n- [ ] Second\n")},
		"two.md": {Data: []byte("# Two\n\n## Later\n\nText.\n")},
	}
	files, err := buildHelp(base)
	if err != nil {
		t.Fatal(err)
	}
	one := string(files["help/one.html"])
	for _, want := range []string{
		`<h1>One</h1>`,
		`<h2 id="a-heading">A heading</h2>`,
		`href="/help/two#later"`,
		`href="#a-heading"`,
		`<aside class="callout callout-tip"><p class="callout-title">Look</p>`,
		`A <strong>tip</strong>.`,
		`<figure class="viz" data-viz="demo">`,
		`<div class="demo"></div>`,
		`<figcaption><p>The caption.</p>`,
		`<ul class="checklist">`,
		`type="checkbox"`,
		`<a class="next" href="/help/two">More</a>`,
	} {
		if !strings.Contains(one, want) {
			t.Errorf("one.html lacks %q:\n%s", want, one)
		}
	}
	if !strings.Contains(string(files["help/index.html"]), `<a class="next" href="/help/one">First steps</a>`) {
		t.Errorf("the overview doesn't lead to the first article")
	}
	if s := string(files["help/search.json"]); !strings.Contains(s, `"Slug":"two"`) || strings.Contains(s, `"Slug":"index"`) {
		t.Errorf("search index: %s", s)
	}

	broken := map[string]string{
		"link to a missing article": "See [x](nope.md).",
		"link to a missing heading": "See [x](two.md#nope).",
		"link to a missing anchor":  "See [x](#nope).",
		"link to /help by address":  "See [x](/help/two).",
		"an unknown visualization":  ":::viz nope\n:::",
		"a second # heading":        "# Again",
	}
	for name, md := range broken {
		fsys := fstest.MapFS{}
		for k, v := range base {
			fsys[k] = v
		}
		fsys["one.md"] = &fstest.MapFile{Data: []byte("# One\n\n" + md + "\n")}
		if _, err := buildHelp(fsys); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	unlisted := fstest.MapFS{}
	for k, v := range base {
		unlisted[k] = v
	}
	unlisted["three.md"] = &fstest.MapFile{Data: []byte("# Three\n")}
	if _, err := buildHelp(unlisted); err == nil || !strings.Contains(err.Error(), "SUMMARY.md") {
		t.Errorf("an article missing from SUMMARY.md: %v", err)
	}
}

// The help center as Markdown (llms.go): every article, readable outside
// the site, and the assistants' links with the prompt in their address.
func TestHelpLLMs(t *testing.T) {
	files, err := buildHelp(helpSrc)
	if err != nil {
		t.Fatal(err)
	}
	full := string(files["llms-full.txt"])
	index := string(files["llms.txt"])
	relLink := regexp.MustCompile(`\]\((?:[^h)][^)]*)\)`) // anything but https://…
	for name, data := range files {
		slug, ok := strings.CutPrefix(name, "help/")
		if !ok || !strings.HasSuffix(slug, ".html") {
			continue
		}
		slug = strings.TrimSuffix(slug, ".html")
		md := string(files["help/"+slug+".md"])
		switch {
		case md == "":
			t.Errorf("%s has no Markdown version", name)
		case !strings.Contains(full, md):
			t.Errorf("llms-full.txt lacks %s.md", slug)
		case slug != "index" && !strings.Contains(index, site+"/help/"+slug+".md"):
			t.Errorf("llms.txt doesn't list %s.md", slug)
		}
		if !strings.Contains(string(data), `href="/help/`+slug+`.md"`) {
			t.Errorf("%s doesn't name its Markdown version", name)
		}
	}
	if strings.Contains(full, ":::") {
		t.Error("llms-full.txt has a ::: block left")
	}
	if m := relLink.FindString(full); m != "" {
		t.Errorf("llms-full.txt has a relative link: %s", m)
	}
	ctx := string(files["help/ai-context.txt"])
	if !strings.Contains(ctx, "<documentation>\n"+strings.TrimRight(full, "\n")+"\n</documentation>") || strings.Contains(ctx, aiSlot) {
		t.Error("ai-context.txt doesn't have llms-full.txt in the prompt's slot")
	}
	if !strings.HasSuffix(ctx, "## My question\n\n") {
		t.Errorf("ai-context.txt doesn't end with the reader's question: %q", ctx[len(ctx)-40:])
	}

	home := string(files["help/index.html"])
	for _, a := range aiAssistants {
		re := regexp.MustCompile(`<a href="([^"]*)"[^>]*data-ai-open="` + a.Name + `"`)
		m := re.FindStringSubmatch(home)
		if m == nil {
			t.Errorf("the overview has no link to %s", a.Name)
			continue
		}
		href := strings.ReplaceAll(m[1], "&amp;", "&")
		if a.Param == "" {
			continue
		}
		// Far below what assistants and hosts accept (Claude: ~14,000 characters).
		if len(href) > 4000 {
			t.Errorf("%s: the link is %d characters long", a.Name, len(href))
		}
		if !strings.Contains(href, a.Param+"=") || !strings.Contains(href, url.QueryEscape(site+llmsFullPath)) ||
			!strings.HasSuffix(href, "%0A%0A") || strings.Contains(href, url.QueryEscape(aiSlot)) {
			t.Errorf("%s: the link doesn't carry the prompt with llms-full.txt: %s", a.Name, href)
		}
	}
}

func TestLLMsMarkdown(t *testing.T) {
	base := fstest.MapFS{
		"page.html":     {Data: []byte(`{{.Body}}`)},
		"help.css":      {Data: []byte("")},
		"help.js":       {Data: []byte("")},
		"ai-prompt.txt": {Data: []byte("Help me with Resound.\n\n{{documentation}}\n\n## My question\n")},
		"SUMMARY.md":    {Data: []byte("# Help\n\n## Start\n- [First steps](one.md)\n- [More](two.md)\n")},
		"index.md":      {Data: []byte("# Help\n\nWelcome.\n")},
		"one.md": {Data: []byte("# One\n\nSee [two](two.md#later), [here](#a-heading) and [the app](/connect).\n\n## A heading\n\n" +
			":::tip Look\nA **tip**.\n\nSecond.\n:::\n\n```\n:::tip not a callout\n```\n")},
		"two.md": {Data: []byte("# Two\n\n## Later\n\nText.\n")},
	}
	files, err := buildHelp(base)
	if err != nil {
		t.Fatal(err)
	}
	one := string(files["help/one.md"])
	for _, want := range []string{
		"# One\n\nSource: " + site + "/help/one\n",
		"[two](" + site + "/help/two#later)",
		"[here](" + site + "/help/one#a-heading)",
		"[the app](" + site + "/connect)",
		"> **Tip: Look**\n>\n> A **tip**.\n>\n> Second.\n\n",
		"```\n:::tip not a callout\n```",
	} {
		if !strings.Contains(one, want) {
			t.Errorf("one.md lacks %q:\n%s", want, one)
		}
	}
	if ctx := string(files["help/ai-context.txt"]); !strings.HasPrefix(ctx, "Help me with Resound.") {
		t.Errorf("ai-context.txt doesn't start with the prompt: %.80q", ctx)
	}
}
