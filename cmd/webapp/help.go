package main

// The help center at /help/: articles written in Markdown (webapp/help/*.md)
// and rendered here into static pages, one per article, with the sidebar
// from SUMMARY.md. It is part of the website but not of the installed app:
// its files are added after the service worker's file list is made, and the
// service worker leaves /help/ to the network (sw.js).
//
// Markdown is GitHub's (tables, task lists) plus blocks fenced by ":::":
//
//	:::tip Optional title         callouts: tip, note, warning
//	Text in **Markdown**.
//	:::
//
//	:::viz audio-flow             a visualization from viz/audio-flow.html,
//	Its caption, in Markdown.     the caption below it
//	:::
//
// A list of task items ("- [ ] Step") becomes a checklist that readers can
// tick off; nothing is stored. Links to other articles name their file
// ("vb-cable.md#restart"); the build fails on links to articles or
// headings that don't exist, on unknown visualizations, and on articles
// missing from SUMMARY.md.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// helpAssets are copied to /help/ as they are.
var helpAssets = []string{"help.css", "help.js"}

type helpSection struct {
	Title string
	Pages []*helpPage
}

type helpHeading struct{ ID, Text string }

type helpPage struct {
	Slug    string // file name without .md; "index" is /help/ itself
	Label   string // in the sidebar (SUMMARY.md)
	Title   string // the article's "# " heading
	Section string
	Summary string // the first paragraph, as plain text
	Body    template.HTML
	TOC     []helpHeading // its "## " headings
	Prev    *helpPage
	Next    *helpPage

	doc ast.Node
	src []byte
	ids map[string]bool // heading IDs, for links to them
}

// helpView is what page.html gets.
type helpView struct {
	*helpPage
	Home     bool
	Sections []helpSection
	Site     string // site, for link previews
	URL      string // the page's address there, its canonical URL
}

// helpPath is an article's path on the site: /help/ for the overview,
// /help/<slug> for the others.
func helpPath(slug string) string {
	if slug == "index" {
		return "/help/"
	}
	return "/help/" + slug
}

// buildHelp renders the help center from the folder fsys into files for
// the site, keyed by their path there ("help/install.html").
func buildHelp(fsys fs.FS) (map[string][]byte, error) {
	summary, err := fs.ReadFile(fsys, "SUMMARY.md")
	if err != nil {
		return nil, err
	}
	sections, err := parseSummary(summary)
	if err != nil {
		return nil, fmt.Errorf("SUMMARY.md: %w", err)
	}
	viz, err := readViz(fsys)
	if err != nil {
		return nil, err
	}
	md := newHelpMarkdown(viz)

	// Read and parse every article first, so links can be checked against
	// all of them while rendering.
	pages := map[string]*helpPage{}
	names, err := fs.Glob(fsys, "*.md")
	if err != nil {
		return nil, err
	}
	for _, name := range names {
		if name == "SUMMARY.md" {
			continue
		}
		src, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, err
		}
		p, err := parseHelpPage(md, strings.TrimSuffix(name, ".md"), src)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		pages[p.Slug] = p
	}
	home := pages["index"]
	if home == nil {
		return nil, errors.New("index.md is missing (the help center's first page)")
	}

	// The sidebar's order is the reading order: Previous and Next follow it.
	var order []*helpPage
	listed := map[string]bool{}
	for i := range sections {
		for j, entry := range sections[i].Pages {
			p := pages[entry.Slug]
			switch {
			case p == nil:
				return nil, fmt.Errorf("SUMMARY.md lists %s.md, which doesn't exist", entry.Slug)
			case listed[p.Slug]:
				return nil, fmt.Errorf("SUMMARY.md lists %s.md twice", entry.Slug)
			case p.Slug == "index":
				return nil, errors.New("SUMMARY.md must not list index.md: it is the help center's first page")
			}
			listed[p.Slug] = true
			p.Label, p.Section = entry.Label, sections[i].Title
			sections[i].Pages[j] = p
			order = append(order, p)
		}
	}
	for slug := range pages {
		if slug != "index" && !listed[slug] {
			return nil, fmt.Errorf("%s.md is not listed in SUMMARY.md", slug)
		}
	}
	for i, p := range order {
		if i > 0 {
			p.Prev = order[i-1]
		}
		if i+1 < len(order) {
			p.Next = order[i+1]
		}
	}
	if len(order) > 0 {
		home.Next = order[0]
	}

	tmplSrc, err := fs.ReadFile(fsys, "page.html")
	if err != nil {
		return nil, err
	}
	tmpl, err := template.New("page.html").Parse(string(tmplSrc))
	if err != nil {
		return nil, err
	}

	out := map[string][]byte{}
	type entry struct {
		Slug, Title, Section, Text string
		Headings                   []helpHeading
	}
	var index []entry
	for _, p := range append([]*helpPage{home}, order...) {
		if err := renderHelpPage(md, p, pages); err != nil {
			return nil, fmt.Errorf("%s.md: %w", p.Slug, err)
		}
		var buf bytes.Buffer
		view := helpView{helpPage: p, Home: p == home, Sections: sections, Site: site, URL: site + helpPath(p.Slug)}
		if err := tmpl.Execute(&buf, view); err != nil {
			return nil, fmt.Errorf("%s.md: %w", p.Slug, err)
		}
		out["help/"+p.Slug+".html"] = buf.Bytes()
		if p != home {
			index = append(index, entry{p.Slug, p.Title, p.Section, plainText(p.doc, p.src), p.TOC})
		}
	}
	data, err := json.Marshal(index)
	if err != nil {
		return nil, err
	}
	out["help/search.json"] = data
	for _, name := range helpAssets {
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, err
		}
		out["help/"+name] = data
	}
	return out, nil
}

// parseSummary reads the sidebar: "## Section" headings, each followed by
// a list of "- [Label](article.md)" links.
func parseSummary(src []byte) ([]helpSection, error) {
	item := regexp.MustCompile(`^[-*] \[([^\]]+)\]\(([a-z0-9-]+)\.md\)$`)
	var sections []helpSection
	comment := false
	for n, line := range strings.Split(strings.ReplaceAll(string(src), "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "<!--") {
			comment = true
		}
		if comment {
			comment = !strings.HasSuffix(line, "-->")
			continue
		}
		switch {
		case line == "" || strings.HasPrefix(line, "# "):
		case strings.HasPrefix(line, "## "):
			sections = append(sections, helpSection{Title: strings.TrimSpace(line[3:])})
		case item.MatchString(line):
			if len(sections) == 0 {
				return nil, fmt.Errorf("line %d: an article before the first ## section", n+1)
			}
			m := item.FindStringSubmatch(line)
			s := &sections[len(sections)-1]
			s.Pages = append(s.Pages, &helpPage{Slug: m[2], Label: m[1]})
		default:
			return nil, fmt.Errorf("line %d: expected \"## Section\" or \"- [Label](article.md)\", got %q", n+1, line)
		}
	}
	return sections, nil
}

// readViz reads the visualizations: viz/<name>.html, inserted as they are.
func readViz(fsys fs.FS) (map[string][]byte, error) {
	names, err := fs.Glob(fsys, "viz/*.html")
	if err != nil {
		return nil, err
	}
	viz := map[string][]byte{}
	for _, name := range names {
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, err
		}
		viz[strings.TrimSuffix(path.Base(name), ".html")] = data
	}
	return viz, nil
}

func newHelpMarkdown(viz map[string][]byte) goldmark.Markdown {
	return goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
			parser.WithBlockParsers(util.Prioritized(containerParser{}, 50)),
			parser.WithASTTransformers(util.Prioritized(checklistTransformer{}, 100)),
		),
		goldmark.WithRendererOptions(
			html.WithUnsafe(), // our own articles: <kbd> and the like
			renderer.WithNodeRenderers(util.Prioritized(containerRenderer{viz}, 100)),
		),
	)
}

// parseHelpPage parses an article and takes out its title, the "# "
// heading it must start with.
func parseHelpPage(md goldmark.Markdown, slug string, src []byte) (*helpPage, error) {
	doc := md.Parser().Parse(text.NewReader(src))
	h, ok := doc.FirstChild().(*ast.Heading)
	if !ok || h.Level != 1 {
		return nil, errors.New(`an article must start with its title, a "# " heading`)
	}
	p := &helpPage{Slug: slug, Title: plainText(h, src), doc: doc, src: src, ids: map[string]bool{}}
	doc.RemoveChild(doc, h)

	err := ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := n.(type) {
		case *ast.Heading:
			id, _ := n.AttributeString("id")
			idText, _ := id.([]byte)
			p.ids[string(idText)] = true
			switch {
			case n.Level == 1:
				return ast.WalkStop, errors.New(`only the title may be a "# " heading`)
			case n.Level == 2:
				p.TOC = append(p.TOC, helpHeading{string(idText), plainText(n, src)})
			}
		case *ast.Paragraph:
			if p.Summary == "" && n.Parent() == doc {
				p.Summary = plainText(n, src)
			}
		}
		return ast.WalkContinue, nil
	})
	return p, err
}

// renderHelpPage turns links to articles into their addresses, checking
// that they exist, and renders the body.
func renderHelpPage(md goldmark.Markdown, p *helpPage, pages map[string]*helpPage) error {
	var errs []error
	_ = ast.Walk(p.doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		link, ok := n.(*ast.Link)
		if !entering || !ok {
			return ast.WalkContinue, nil
		}
		dest, err := helpLink(string(link.Destination), p, pages)
		if err != nil {
			errs = append(errs, err)
		}
		link.Destination = []byte(dest)
		return ast.WalkContinue, nil
	})
	if err := errors.Join(errs...); err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := md.Renderer().Render(&buf, p.src, p.doc); err != nil {
		return err
	}
	p.Body = template.HTML(buf.String())
	return nil
}

// helpLink resolves a link in an article: other articles by their file
// ("install.md", "install.md#heading"), headings of this one ("#heading"),
// pages of the site ("/connect") and the web (https://…) as they are.
func helpLink(dest string, from *helpPage, pages map[string]*helpPage) (string, error) {
	switch {
	case strings.HasPrefix(dest, "https://"), strings.HasPrefix(dest, "http://"), strings.HasPrefix(dest, "mailto:"):
		return dest, nil
	case strings.HasPrefix(dest, "/"):
		if strings.HasPrefix(dest, "/help") {
			return dest, fmt.Errorf("link %q: link to articles by their file, e.g. install.md", dest)
		}
		return dest, nil
	case strings.HasPrefix(dest, "#"):
		if !from.ids[dest[1:]] {
			return dest, fmt.Errorf("link %q: no such heading in this article", dest)
		}
		return dest, nil
	}
	file, frag, _ := strings.Cut(dest, "#")
	slug, ok := strings.CutSuffix(file, ".md")
	to := pages[slug]
	if !ok || to == nil {
		return dest, fmt.Errorf("link %q: no such article", dest)
	}
	if frag != "" && !to.ids[frag] {
		return dest, fmt.Errorf("link %q: %s.md has no such heading", dest, slug)
	}
	url := helpPath(slug)
	if frag != "" {
		url += "#" + frag
	}
	return url, nil
}

// plainText is the text of n and its children, without markup, for titles,
// descriptions and the search index.
func plainText(n ast.Node, src []byte) string {
	var b strings.Builder
	_ = ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			if c.Type() == ast.TypeBlock && b.Len() > 0 {
				b.WriteByte(' ')
			}
			return ast.WalkContinue, nil
		}
		switch c := c.(type) {
		case *ast.Text:
			b.Write(c.Segment.Value(src))
			if c.SoftLineBreak() || c.HardLineBreak() {
				b.WriteByte(' ')
			}
		case *ast.String:
			b.Write(c.Value)
		case *ast.CodeSpan:
			for t := c.FirstChild(); t != nil; t = t.NextSibling() {
				if t, ok := t.(*ast.Text); ok {
					b.Write(t.Segment.Value(src))
				}
			}
			return ast.WalkSkipChildren, nil
		case *ast.RawHTML:
			return ast.WalkSkipChildren, nil
		case *ast.HTMLBlock, *ast.FencedCodeBlock, *ast.CodeBlock:
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
	return strings.Join(strings.Fields(b.String()), " ")
}

// --- ":::" blocks -------------------------------------------------------------

var kindContainer = ast.NewNodeKind("Container")

// container is a ":::name arg" … ":::" block.
type container struct {
	ast.BaseBlock
	Name string // tip, note, warning, viz
	Arg  string // a callout's title, a visualization's name
}

func (n *container) Kind() ast.NodeKind { return kindContainer }
func (n *container) Dump(src []byte, level int) {
	ast.DumpHelper(n, src, level, map[string]string{"Name": n.Name, "Arg": n.Arg}, nil)
}

var containerNames = []string{"tip", "note", "warning", "viz"}

var containerOpen = regexp.MustCompile(`^:::([a-z]+)(?:[ \t]+(.*?))?[ \t]*$`)

type containerParser struct{}

func (containerParser) Trigger() []byte { return []byte{':'} }

func (containerParser) Open(parent ast.Node, reader text.Reader, pc parser.Context) (ast.Node, parser.State) {
	line, _ := reader.PeekLine()
	pos := pc.BlockOffset()
	if pos < 0 {
		return nil, parser.NoChildren
	}
	m := containerOpen.FindSubmatch(bytes.TrimRight(line[pos:], "\r\n"))
	if m == nil || !slices.Contains(containerNames, string(m[1])) {
		return nil, parser.NoChildren
	}
	reader.AdvanceToEOL()
	return &container{Name: string(m[1]), Arg: string(m[2])}, parser.HasChildren
}

func (containerParser) Continue(node ast.Node, reader text.Reader, pc parser.Context) parser.State {
	line, _ := reader.PeekLine()
	if string(bytes.TrimSpace(line)) == ":::" {
		reader.AdvanceToEOL()
		return parser.Close
	}
	return parser.Continue | parser.HasChildren
}

func (containerParser) Close(node ast.Node, reader text.Reader, pc parser.Context) {}
func (containerParser) CanInterruptParagraph() bool                                { return true }
func (containerParser) CanAcceptIndentedLine() bool                                { return false }

type containerRenderer struct{ viz map[string][]byte }

func (r containerRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(kindContainer, r.render)
}

var calloutTitles = map[string]string{"tip": "Tip", "note": "Note", "warning": "Watch out"}

func (r containerRenderer) render(w util.BufWriter, src []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n := node.(*container)
	if n.Name == "viz" {
		if !entering {
			if n.HasChildren() {
				_, _ = w.WriteString("</figcaption>")
			}
			_, _ = w.WriteString("</figure>\n")
			return ast.WalkContinue, nil
		}
		viz, ok := r.viz[n.Arg]
		if !ok {
			return ast.WalkStop, fmt.Errorf("no visualization %q (viz/%s.html)", n.Arg, n.Arg)
		}
		fmt.Fprintf(w, "<figure class=\"viz\" data-viz=\"%s\">\n", util.EscapeHTML([]byte(n.Arg)))
		_, _ = w.Write(viz)
		if n.HasChildren() {
			_, _ = w.WriteString("<figcaption>")
		}
		return ast.WalkContinue, nil
	}
	if !entering {
		_, _ = w.WriteString("</div></aside>\n")
		return ast.WalkContinue, nil
	}
	title := n.Arg
	if title == "" {
		title = calloutTitles[n.Name]
	}
	fmt.Fprintf(w, "<aside class=\"callout callout-%s\"><p class=\"callout-title\">%s</p><div class=\"callout-body\">\n",
		n.Name, util.EscapeHTML([]byte(title)))
	return ast.WalkContinue, nil
}

// checklistTransformer marks lists of task items ("- [ ] Step") as
// checklists, which help.js makes tickable.
type checklistTransformer struct{}

func (checklistTransformer) Transform(doc *ast.Document, reader text.Reader, pc parser.Context) {
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		list, ok := n.(*ast.List)
		if !entering || !ok {
			return ast.WalkContinue, nil
		}
		for item := list.FirstChild(); item != nil; item = item.NextSibling() {
			first := item.FirstChild()
			if first == nil || first.FirstChild() == nil || first.FirstChild().Kind() != extast.KindTaskCheckBox {
				return ast.WalkContinue, nil
			}
		}
		list.SetAttributeString("class", []byte("checklist"))
		return ast.WalkContinue, nil
	})
}
