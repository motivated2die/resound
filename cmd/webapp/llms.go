package main

// The help center for AI assistants: every article as Markdown, so readers
// can hand Resound's documentation to ChatGPT, Claude or another assistant
// and ask it for help (the "Ask an AI" card on the overview and below each
// article, help.js), and so assistants that browse can read it themselves.
//
//	/llms.txt             the index (llmstxt.org): every article, linked to its Markdown
//	/llms-full.txt        all articles in reading order, in one file
//	/help/<slug>.md       one article
//	/help/ai-context.txt  ai-prompt.txt with /llms-full.txt in it: what "Copy for your AI" copies
//
// The Markdown is the articles' own, made readable outside the site: links
// become absolute URLs, callouts become quotes, and visualizations keep
// only their caption.
//
// ai-prompt.txt has a {{documentation}} slot for the documentation, and
// ends with "## My question" and an empty line for the reader to write in.
// Assistants that take a prompt in their address get it with a link to
// /llms-full.txt in the slot, which they read with their web tools; the
// whole help center is far too long for an address (Claude cuts it at about
// 14,000 characters). The others open as they are, and the reader pastes
// what "Copy for your AI" copied. Nothing is sent anywhere by us: these are
// plain links.

import (
	"fmt"
	"io/fs"
	"net/url"
	"regexp"
	"strings"
)

const (
	llmsIndexPath = "/llms.txt"
	llmsFullPath  = "/llms-full.txt"
	aiContextPath = "/help/ai-context.txt"
)

// aiAssistant is one "Open in …" link. With a param, the prompt goes into
// the address (the assistant fills it in, some send it at once); without
// one, the reader pastes it.
type aiAssistant struct{ Name, URL, Param string }

// aiAssistants, in the card's order. Only parameters these assistants'
// own pages accept: ChatGPT's hints=search turns on browsing, so it reads
// the link.
var aiAssistants = []aiAssistant{
	{"ChatGPT", "https://chatgpt.com/?hints=search", "q"},
	{"Claude", "https://claude.ai/new", "q"},
	{"Gemini", "https://gemini.google.com/app", ""},
	{"Copilot", "https://copilot.microsoft.com/", ""},
	{"Perplexity", "https://www.perplexity.ai/search", "q"},
	{"Grok", "https://grok.com/", "q"},
}

// aiLink is an assistant's link on the page.
type aiLink struct {
	Name, Href string
	Paste      bool // opens empty: paste what was copied
}

// helpAI is what the "Ask an AI" card shows.
type helpAI struct {
	Links    []aiLink
	Articles int
}

// aiView is the card in one of its looks (page.html), on the page slug.
func aiView(ai helpAI, look, slug string) any {
	return struct {
		helpAI
		Look, Slug string
	}{ai, look, slug}
}

// aiSlot is where ai-prompt.txt takes the documentation.
const aiSlot = "{{documentation}}"

// buildAI adds the Markdown files to out and returns the card's links.
func buildAI(fsys fs.FS, home *helpPage, sections []helpSection, order []*helpPage, pages map[string]*helpPage, out map[string][]byte) (helpAI, error) {
	prompt, err := fs.ReadFile(fsys, "ai-prompt.txt")
	if err != nil {
		return helpAI{}, err
	}
	// Two line breaks at the end: the reader's question goes below them.
	core := strings.TrimSpace(strings.ReplaceAll(string(prompt), "\r\n", "\n")) + "\n\n"
	if strings.Count(core, aiSlot) != 1 {
		return helpAI{}, fmt.Errorf("ai-prompt.txt: wants %s once, where the documentation goes", aiSlot)
	}

	md := map[string]string{}
	for _, p := range append([]*helpPage{home}, order...) {
		md[p.Slug] = llmsMarkdown(p, pages)
		out["help/"+p.Slug+".md"] = []byte(md[p.Slug])
	}

	var index strings.Builder
	fmt.Fprintf(&index, "# Resound\n\n> %s\n\n", home.Summary)
	fmt.Fprintf(&index, "Resound's help center, as Markdown. Every article in one file: %s%s\n", site, llmsFullPath)
	for _, s := range sections {
		fmt.Fprintf(&index, "\n## %s\n\n", s.Title)
		for _, p := range s.Pages {
			fmt.Fprintf(&index, "- [%s](%s/help/%s.md): %s\n", p.Title, site, p.Slug, p.Summary)
		}
	}
	out[strings.TrimPrefix(llmsIndexPath, "/")] = []byte(index.String())

	var full strings.Builder
	full.WriteString(md["index"])
	for _, p := range order {
		full.WriteString("\n---\n\n")
		full.WriteString(md[p.Slug])
	}
	out[strings.TrimPrefix(llmsFullPath, "/")] = []byte(full.String())

	// The articles' own # headings stay apart from the prompt's in a tag.
	out[strings.TrimPrefix(aiContextPath, "/")] = []byte(strings.Replace(core, aiSlot,
		"All of it, from "+site+"/help/:\n\n<documentation>\n"+strings.TrimRight(full.String(), "\n")+"\n</documentation>", 1))

	linked := strings.Replace(core, aiSlot, "All of it is in one file: "+site+llmsFullPath+
		"\nRead it before you answer. If you can't open it, tell me, and I'll paste it here.", 1)
	ai := helpAI{Articles: len(order)}
	for _, a := range aiAssistants {
		l := aiLink{Name: a.Name, Href: a.URL, Paste: a.Param == ""}
		if !l.Paste {
			sep := "?"
			if strings.Contains(a.URL, "?") {
				sep = "&"
			}
			// %20, not +: not every assistant reads + as a space.
			l.Href += sep + a.Param + "=" + strings.ReplaceAll(url.QueryEscape(linked), "+", "%20")
		}
		ai.Links = append(ai.Links, l)
	}
	return ai, nil
}

var (
	mdLinkDest = regexp.MustCompile(`\]\(([^)\s]+)\)`)
	mdFence    = regexp.MustCompile("^\\s*(```|~~~)")
)

// llmsMarkdown is an article as Markdown for reading elsewhere: its title,
// its address, and its text with absolute links, callouts as quotes and
// visualizations as their caption. The build has already checked its
// links (renderHelpPage).
func llmsMarkdown(p *helpPage, pages map[string]*helpPage) string {
	src := strings.ReplaceAll(string(p.src), "\r\n", "\n")
	lines := strings.Split(strings.TrimRight(src, "\n"), "\n")

	var b strings.Builder
	fence, quote := false, false
	for i, line := range lines {
		switch {
		case mdFence.MatchString(line):
			fence = !fence
		case fence:
		case i == 0:
			// The title, then where the article is on the web.
			fmt.Fprintf(&b, "%s\n\nSource: %s%s\n", line, site, helpPath(p.Slug))
			continue
		case quote && strings.TrimSpace(line) == ":::":
			quote = false
			continue
		case !quote && containerOpen.MatchString(line):
			m := containerOpen.FindStringSubmatch(line)
			title := m[2]
			switch {
			case m[1] == "viz":
				title = "Picture on the web page"
			case title == "":
				title = calloutTitles[m[1]]
			default:
				title = calloutTitles[m[1]] + ": " + title
			}
			fmt.Fprintf(&b, "> **%s**\n>\n", title)
			quote = true
			continue
		default:
			line = mdLinkDest.ReplaceAllStringFunc(line, func(s string) string {
				return "](" + llmsLink(s[2:len(s)-1], p, pages) + ")"
			})
		}
		if quote {
			line = strings.TrimRight("> "+line, " ")
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

// llmsLink is a link of an article as an absolute URL.
func llmsLink(dest string, from *helpPage, pages map[string]*helpPage) string {
	if strings.HasPrefix(dest, "#") {
		return site + helpPath(from.Slug) + dest
	}
	resolved, err := helpLink(dest, from, pages)
	if err == nil && strings.HasPrefix(resolved, "/") {
		return site + resolved
	}
	return resolved
}
