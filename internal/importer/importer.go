// Package importer downloads a sound from a link dropped onto the panel: a
// link to an audio file, or to a web page that names one, such as a
// Myinstants sound page (its og:audio tag). The file is checked and saved
// into a library folder under a clean file name.
//
// Only public internet addresses are fetched (never this PC or the local
// network), over HTTP(S), with size limits on pages and sounds.
package importer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/html"
)

const (
	// MaxNameLen is the longest sound name (in characters, without the
	// extension) a file is saved under.
	MaxNameLen = 80
	// MaxSoundBytes limits a downloaded sound file.
	MaxSoundBytes = 25 << 20
	// maxPageBytes limits a web page read while looking for its sound.
	maxPageBytes = 2 << 20
	timeout      = 30 * time.Second
	userAgent    = "Resound (+https://github.com/motivated2die/resound)"
)

var (
	// ErrNoSound means the link is neither a sound file nor a page with
	// exactly one sound on it.
	ErrNoSound = errors.New("no sound file found at this link")
	// ErrManySounds means the page links several sounds (e.g. a search or
	// category page), so none is taken.
	ErrManySounds = errors.New("this page has several sounds")
	// ErrUnsupported means the file is not a format Resound plays.
	ErrUnsupported = errors.New("not a supported sound format")
	// ErrTooLarge means the sound is bigger than MaxSoundBytes.
	ErrTooLarge = errors.New("the sound file is too large")
	// ErrBlocked means the link points to this PC or the local network.
	ErrBlocked = errors.New("links to this PC or the local network are not downloaded")
)

// Fetcher downloads sounds. The zero value is not usable; use New.
type Fetcher struct {
	Client *http.Client
	// Extensions are the file types that can be saved (".mp3", …).
	Extensions []string
	// Check verifies a downloaded file before it is kept, e.g. by decoding
	// its start. It gets the temporary file's path, which ends in the
	// sound's extension. Nil skips the check.
	Check func(path string) error
}

// New returns a Fetcher whose client only connects to public addresses.
func New(extensions []string, check func(string) error) *Fetcher {
	dialer := &net.Dialer{Timeout: 10 * time.Second, Control: publicOnly}
	tr := &http.Transport{
		Proxy:                 nil, // a proxy on the LAN would be refused by publicOnly
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		MaxIdleConns:          4,
		IdleConnTimeout:       30 * time.Second,
	}
	return &Fetcher{
		Client: &http.Client{
			Transport: tr,
			Timeout:   timeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return errors.New("too many redirects")
				}
				return checkScheme(req.URL)
			},
		},
		Extensions: extensions,
		Check:      check,
	}
}

// publicOnly refuses connections to loopback, private, link-local and other
// non-public addresses. It runs after name resolution, for every address
// tried, so a public name that resolves to a LAN address is refused too.
func publicOnly(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil || !isPublic(ip) {
		return ErrBlocked
	}
	return nil
}

var cgnat = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

func isPublic(ip net.IP) bool {
	return !(ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() || cgnat.Contains(ip))
}

func checkScheme(u *url.URL) error {
	if u.Scheme != "https" && u.Scheme != "http" {
		return fmt.Errorf("%w: only web links (http, https) can be downloaded", ErrNoSound)
	}
	return nil
}

// ParseLink returns the web link in a dropped text, or false: the text
// must be a single http(s) URL (browsers drop links that way).
func ParseLink(text string) (*url.URL, bool) {
	text = strings.TrimSpace(text)
	if text == "" || strings.ContainsAny(text, " \t\r\n") {
		return nil, false
	}
	u, err := url.Parse(text)
	if err != nil || u.Host == "" || checkScheme(u) != nil {
		return nil, false
	}
	return u, true
}

// Found is the sound a link leads to.
type Found struct {
	AudioURL *url.URL
	Name     string // suggested name, not yet sanitized
}

// Saved is a downloaded sound.
type Saved struct {
	Path string // the file in the target folder
	Name string // the file name without its extension
}

// Fetch resolves link to a sound, downloads it and saves it into dir under
// its suggested name (made unique there).
func (f *Fetcher) Fetch(ctx context.Context, link, dir string) (Saved, error) {
	u, ok := ParseLink(link)
	if !ok {
		return Saved{}, ErrNoSound
	}
	found, body, err := f.resolve(ctx, u)
	if err != nil {
		return Saved{}, err
	}
	if body == nil {
		var ext string
		if body, ext, err = f.open(ctx, found.AudioURL); err != nil {
			return Saved{}, err
		}
		found.ext = ext
	}
	defer body.Close()
	return f.save(body, found.ext, SanitizeName(found.Name), dir)
}

// found is Found plus the extension, once the sound's response is known.
type found struct {
	Found
	ext string
}

// resolve fetches link. When it is a sound, the open response body is
// returned with it; when it is a page, the sound it names is found.
func (f *Fetcher) resolve(ctx context.Context, link *url.URL) (found, io.ReadCloser, error) {
	resp, err := f.get(ctx, link, "text/html,audio/*;q=0.9,*/*;q=0.5")
	if err != nil {
		return found{}, nil, err
	}
	final := resp.Request.URL // after redirects
	if ext, ok := f.soundExt(resp, final); ok {
		return found{Found{AudioURL: final, Name: nameFromResponse(resp, final)}, ext}, resp.Body, nil
	}
	defer resp.Body.Close()
	ct, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	switch {
	case strings.HasPrefix(ct, "audio/"):
		return found{}, nil, ErrUnsupported
	case ct != "text/html" && ct != "application/xhtml+xml":
		return found{}, nil, ErrNoSound
	}
	page, err := ParsePage(io.LimitReader(resp.Body, maxPageBytes), final)
	if err != nil {
		return found{}, nil, err
	}
	return found{Found: page}, nil, nil
}

// open fetches the sound file itself.
func (f *Fetcher) open(ctx context.Context, u *url.URL) (io.ReadCloser, string, error) {
	resp, err := f.get(ctx, u, "audio/*,*/*;q=0.5")
	if err != nil {
		return nil, "", err
	}
	ext, ok := f.soundExt(resp, resp.Request.URL)
	if !ok {
		resp.Body.Close()
		return nil, "", ErrUnsupported
	}
	return resp.Body, ext, nil
}

func (f *Fetcher) get(ctx context.Context, u *url.URL, accept string) (*http.Response, error) {
	if err := checkScheme(u); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", accept)
	resp, err := f.Client.Do(req)
	if err != nil {
		if errors.Is(err, ErrBlocked) {
			return nil, ErrBlocked
		}
		return nil, fmt.Errorf("could not load %s: %w", u.Host, unwrapURLError(err))
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("%s answered %s", u.Host, resp.Status)
	}
	if resp.ContentLength > MaxSoundBytes {
		resp.Body.Close()
		return nil, ErrTooLarge
	}
	return resp, nil
}

func unwrapURLError(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

// audioTypes maps MIME types to extensions, for links without one.
var audioTypes = map[string]string{
	"audio/mpeg": ".mp3", "audio/mp3": ".mp3",
	"audio/wav": ".wav", "audio/x-wav": ".wav", "audio/wave": ".wav", "audio/vnd.wave": ".wav",
	"audio/flac": ".flac", "audio/x-flac": ".flac",
	"audio/ogg": ".ogg", "audio/vorbis": ".ogg", "application/ogg": ".ogg",
}

// soundExt reports whether a response is a sound file of a supported type:
// by its audio Content-Type, or by the URL's extension when the server
// sends no specific type.
func (f *Fetcher) soundExt(resp *http.Response, u *url.URL) (string, bool) {
	ct, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	urlExt := strings.ToLower(path.Ext(u.Path))
	switch {
	case audioTypes[ct] != "":
		if f.supported(urlExt) && audioTypes[ct] == urlExt {
			return urlExt, true
		}
		return audioTypes[ct], f.supported(audioTypes[ct])
	case strings.HasPrefix(ct, "audio/"):
		return "", false // an audio format Resound doesn't play (m4a, aac, …)
	case ct == "" || ct == "application/octet-stream" || ct == "binary/octet-stream":
		return urlExt, f.supported(urlExt)
	}
	return "", false
}

func (f *Fetcher) supported(ext string) bool {
	for _, e := range f.Extensions {
		if strings.EqualFold(e, ext) {
			return ext != ""
		}
	}
	return false
}

// save writes the sound to a temporary file in dir, checks it, and moves it
// to its final, unique name.
func (f *Fetcher) save(body io.Reader, ext, name, dir string) (Saved, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Saved{}, err
	}
	// The temporary name keeps the extension (the check decodes by it) but
	// starts with "~" and a random part, so the library ignores it.
	tmp, err := os.CreateTemp(dir, "~resound-*"+ext)
	if err != nil {
		return Saved{}, err
	}
	tmpPath := tmp.Name()
	keep := false
	defer func() {
		if !keep {
			os.Remove(tmpPath)
		}
	}()
	n, err := io.Copy(tmp, io.LimitReader(body, MaxSoundBytes+1))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	switch {
	case err != nil:
		return Saved{}, fmt.Errorf("downloading: %w", unwrapURLError(err))
	case n > MaxSoundBytes:
		return Saved{}, ErrTooLarge
	case n == 0:
		return Saved{}, ErrNoSound
	}
	if f.Check != nil {
		if err := f.Check(tmpPath); err != nil {
			return Saved{}, fmt.Errorf("%w: the file could not be read", ErrUnsupported)
		}
	}
	dst, err := MoveUnique(tmpPath, dir, name, ext)
	if err != nil {
		return Saved{}, err
	}
	keep = true
	return Saved{Path: dst, Name: strings.TrimSuffix(filepath.Base(dst), ext)}, nil
}

// CopyUnique copies the file src into dir under its own name (sanitized and
// made unique there) and returns the copy's path. The original stays.
func CopyUnique(src, dir string) (string, error) {
	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer in.Close()
	ext := strings.ToLower(filepath.Ext(src))
	tmp, err := os.CreateTemp(dir, "~resound-*"+ext)
	if err != nil {
		return "", err
	}
	_, err = io.Copy(tmp, in)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		var dst string
		name := SanitizeName(strings.TrimSuffix(filepath.Base(src), filepath.Ext(src)))
		if dst, err = MoveUnique(tmp.Name(), dir, name, ext); err == nil {
			return dst, nil
		}
	}
	os.Remove(tmp.Name())
	return "", err
}

// MoveUnique renames src to name+ext in dir, adding " (2)", " (3)", … when
// that name is taken. It never replaces an existing file.
func MoveUnique(src, dir, name, ext string) (string, error) {
	for i := 1; i < 1000; i++ {
		candidate := name
		if i > 1 {
			suffix := fmt.Sprintf(" (%d)", i)
			candidate = truncate(name, MaxNameLen-utf8.RuneCountInString(suffix)) + suffix
		}
		dst := filepath.Join(dir, candidate+ext)
		if _, err := os.Lstat(dst); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		// os.Rename replaces an existing file on Windows; a link is created
		// first so a file that appeared meanwhile is never overwritten.
		if err := os.Link(src, dst); err != nil {
			if errors.Is(err, os.ErrExist) {
				continue
			}
			if err := os.Rename(src, dst); err != nil { // file systems without hard links
				return "", err
			}
			return dst, nil
		}
		os.Remove(src)
		return dst, nil
	}
	return "", errors.New("no free file name")
}

// --- pages ---------------------------------------------------------------------

// ParsePage finds the one sound a web page is about, and its name. It
// prefers the page's og:audio tag (Myinstants sound pages have it); without
// one it takes the page's sound when there is exactly one: an <audio> or
// <source> element, a data-url attribute, or a link to a sound file. Pages
// with several (search results, categories) return ErrManySounds.
func ParsePage(r io.Reader, base *url.URL) (Found, error) {
	var (
		ogAudio, ogTitle, title, heading string
		inTitle, inHeading               bool
		others                           []string
	)
	z := html.NewTokenizer(r)
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			if err := z.Err(); err != nil && !errors.Is(err, io.EOF) {
				return Found{}, err
			}
			break
		}
		switch tt {
		case html.TextToken:
			switch {
			case inTitle:
				title += string(z.Text())
			case inHeading:
				heading += string(z.Text())
			}
		case html.EndTagToken:
			name, _ := z.TagName()
			switch string(name) {
			case "title":
				inTitle = false
			case "h1":
				inHeading = false
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			attrs := map[string]string{}
			for hasAttr {
				var k, v []byte
				k, v, hasAttr = z.TagAttr()
				attrs[string(k)] = string(v)
			}
			switch tag := string(name); tag {
			case "base":
				if u, err := base.Parse(attrs["href"]); err == nil && attrs["href"] != "" {
					base = u
				}
			case "meta":
				prop := strings.ToLower(attrs["property"] + attrs["name"])
				switch prop {
				case "og:audio", "og:audio:url", "og:audio:secure_url":
					if ogAudio == "" {
						ogAudio = attrs["content"]
					}
				case "og:title":
					ogTitle = attrs["content"]
				}
			case "title":
				inTitle = tt == html.StartTagToken
			case "h1":
				// Myinstants' sound name, exactly as the page shows it.
				if attrs["id"] == "instant-page-title" {
					heading, inHeading = "", tt == html.StartTagToken
				}
			case "audio", "source":
				if attrs["src"] != "" {
					others = append(others, attrs["src"])
				}
			case "a":
				if href := attrs["href"]; hasSoundExt(href) {
					others = append(others, href)
				}
			}
			if v := attrs["data-url"]; hasSoundExt(v) {
				others = append(others, v)
			}
		}
	}

	var audio string
	if ogAudio != "" {
		audio = ogAudio
	} else {
		distinct := map[string]bool{}
		for _, o := range others {
			if u, err := base.Parse(o); err == nil {
				distinct[u.String()] = true
				audio = o
			}
		}
		switch {
		case len(distinct) == 0:
			return Found{}, ErrNoSound
		case len(distinct) > 1:
			return Found{}, ErrManySounds
		}
	}
	u, err := base.Parse(strings.TrimSpace(audio))
	if err != nil || checkScheme(u) != nil {
		return Found{}, ErrNoSound
	}
	name := strings.TrimSpace(heading) // the tokenizer has unescaped it
	if name == "" {
		name = cleanTitle(ogTitle)
	}
	if name == "" {
		name = cleanTitle(title)
	}
	if name == "" {
		name = nameFromURL(u)
	}
	return Found{AudioURL: u, Name: name}, nil
}

// cleanTitle drops the site's suffixes from a page title, e.g. Myinstants'
// "NAME - Sound Button" or "NAME - Instant Sound Effect Button | Myinstants".
func cleanTitle(t string) string {
	t = strings.TrimSpace(t)
	if i := strings.LastIndex(t, " | "); i > 0 {
		t = t[:i]
	}
	for _, suffix := range []string{" - Sound Button", " - Instant Sound Effect Button"} {
		t = strings.TrimSuffix(t, suffix)
	}
	return strings.TrimSpace(t)
}

func hasSoundExt(link string) bool {
	if link == "" {
		return false
	}
	u, err := url.Parse(link)
	if err != nil {
		return false
	}
	switch strings.ToLower(path.Ext(u.Path)) {
	case ".mp3", ".wav", ".flac", ".ogg", ".oga", ".m4a", ".aac", ".opus":
		return true
	}
	return false
}

// nameFromResponse names a directly linked sound: the server's file name,
// or the URL's.
func nameFromResponse(resp *http.Response, u *url.URL) string {
	if _, params, err := mime.ParseMediaType(resp.Header.Get("Content-Disposition")); err == nil && params["filename"] != "" {
		return strings.TrimSuffix(params["filename"], path.Ext(params["filename"]))
	}
	return nameFromURL(u)
}

// nameFromURL turns "…/vine-boom_2.mp3" into "vine boom 2".
func nameFromURL(u *url.URL) string {
	base := path.Base(u.Path)
	base = strings.TrimSuffix(base, path.Ext(base))
	if unescaped, err := url.PathUnescape(base); err == nil {
		base = unescaped
	}
	return strings.Join(strings.FieldsFunc(base, func(r rune) bool { return r == '-' || r == '_' || r == '+' }), " ")
}

// --- names ---------------------------------------------------------------------

// SanitizeName makes a sound name safe as a Windows file name: characters
// Windows forbids and control characters are removed, runs of spaces
// collapsed, trailing dots and spaces dropped (Windows strips them),
// reserved device names (CON, NUL, COM1, …) avoided, and the length kept
// to MaxNameLen characters. An empty result becomes "Sound".
func SanitizeName(name string) string {
	var b strings.Builder
	space := false
	for _, r := range name {
		switch {
		case unicode.IsSpace(r): // before IsControl: tabs and line breaks are spaces too
			space = b.Len() > 0
			continue
		case r == utf8.RuneError, unicode.IsControl(r), strings.ContainsRune(`<>:"/\|?*`, r),
			unicode.Is(unicode.Cf, r): // invisible format characters (zero-width, bidi)
			continue
		}
		if space {
			b.WriteByte(' ')
			space = false
		}
		b.WriteRune(r)
	}
	out := strings.TrimRight(truncate(b.String(), MaxNameLen), ". ")
	out = strings.TrimLeft(out, " ")
	if out == "" {
		return "Sound"
	}
	if reserved(out) {
		out = truncate(out, MaxNameLen-1) + "_"
	}
	return out
}

// reserved reports Windows device names, which can't be file names even
// with an extension ("CON.mp3").
func reserved(name string) bool {
	stem := strings.ToUpper(strings.TrimRight(strings.SplitN(name, ".", 2)[0], " "))
	switch stem {
	case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
		return true
	}
	for _, dev := range []string{"COM", "LPT"} {
		if rest, ok := strings.CutPrefix(stem, dev); ok && utf8.RuneCountInString(rest) == 1 {
			return strings.ContainsAny(rest, "0123456789¹²³")
		}
	}
	return false
}

func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return strings.TrimRight(string(r[:n]), " ")
}
