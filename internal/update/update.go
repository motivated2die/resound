// Package update finds the latest published Resound release on GitHub and
// downloads its exe, checked against the release's SHA256SUMS.txt.
//
// Only full releases count: GitHub's "latest release" skips drafts and
// pre-releases (v1.2.3-rc.1), and a tag with a pre-release suffix is
// ignored as well. Installing is not done here: the downloaded exe is
// started and installs itself like any downloaded copy (package install).
package update

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultAPI is the GitHub API base of this project's repository.
	DefaultAPI = "https://api.github.com/repos/motivated2die/resound"
	// ReleasesPage lists every release, for when no release page is known.
	ReleasesPage = "https://github.com/motivated2die/resound/releases"

	// AssetName and SumsName are the files each release carries
	// (.github/workflows/release.yml).
	AssetName = "Resound.exe"
	SumsName  = "SHA256SUMS.txt"

	maxJSONBytes = 1 << 20
	maxSumsBytes = 64 << 10
	maxExeBytes  = 64 << 20
)

// ErrNoRelease means the repository has no published full release yet.
var ErrNoRelease = errors.New("no published release")

// Release is a published release that carries the exe and its checksums.
type Release struct {
	Version string // "0.8.0", from the tag without the "v"
	Page    string // the release's GitHub page (notes)
	exeURL  string
	sumsURL string
	exeSize int64
}

// Client talks to the GitHub API (or a test server, see API).
type Client struct {
	// API is the repository's API base; empty means DefaultAPI. Assets
	// are only fetched from GitHub's hosts, or with another API base
	// (tests) from that base's host.
	API string
	// UserAgent identifies the app; GitHub rejects requests without one.
	UserAgent string
	// HTTP is used for all requests; nil means a client with timeouts.
	HTTP *http.Client
}

func (c Client) api() string {
	if c.API == "" {
		return DefaultAPI
	}
	return strings.TrimRight(c.API, "/")
}

// CheckAPI reports whether an API base other than DefaultAPI may be used:
// HTTPS, or plain HTTP to this PC only (a local test server).
func CheckAPI(api string) error {
	u, err := url.Parse(api)
	if err != nil {
		return err
	}
	switch {
	case u.Scheme == "https" && u.Host != "":
		return nil
	case u.Scheme == "http" && isLoopback(u.Hostname()):
		return nil
	}
	return fmt.Errorf("%q must be https, or http to localhost", api)
}

func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// allowed reports whether u may be fetched: GitHub's HTTPS hosts, or the
// API's own host when a test API is set.
func (c Client) allowed(u *url.URL) bool {
	if c.api() != DefaultAPI {
		base, err := url.Parse(c.api())
		return err == nil && u.Scheme == base.Scheme && strings.EqualFold(u.Host, base.Host)
	}
	host := strings.ToLower(u.Hostname())
	return u.Scheme == "https" &&
		(host == "github.com" || host == "api.github.com" || strings.HasSuffix(host, ".githubusercontent.com"))
}

func (c Client) client(timeout time.Duration) *http.Client {
	base := c.HTTP
	if base == nil {
		base = &http.Client{}
	}
	cl := *base
	if cl.Timeout == 0 {
		cl.Timeout = timeout
	}
	cl.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		if !c.allowed(req.URL) {
			return fmt.Errorf("redirected to an unexpected host %s", req.URL.Host)
		}
		return nil
	}
	return &cl
}

func (c Client) get(ctx context.Context, u string, timeout time.Duration, accept string) (*http.Response, error) {
	parsed, err := url.Parse(u)
	if err != nil {
		return nil, err
	}
	if !c.allowed(parsed) {
		return nil, fmt.Errorf("unexpected download host %s", parsed.Host)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	return c.client(timeout).Do(req)
}

// Latest returns the latest published full release. ErrNoRelease means
// there is none (yet), or it has no exe.
func (c Client) Latest(ctx context.Context) (Release, error) {
	resp, err := c.get(ctx, c.api()+"/releases/latest", 20*time.Second, "application/vnd.github+json")
	if err != nil {
		return Release{}, err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return Release{}, ErrNoRelease
	case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests:
		return Release{}, fmt.Errorf("GitHub refused the request (%s), probably its rate limit; try again later", resp.Status)
	case resp.StatusCode != http.StatusOK:
		return Release{}, fmt.Errorf("GitHub answered %s", resp.Status)
	}
	var body struct {
		Tag        string `json:"tag_name"`
		Page       string `json:"html_url"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
		Assets     []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
			Size int64  `json:"size"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxJSONBytes)).Decode(&body); err != nil {
		return Release{}, fmt.Errorf("reading GitHub's answer: %w", err)
	}
	v, ok := parse(strings.TrimPrefix(body.Tag, "v"))
	if !ok || !strings.HasPrefix(body.Tag, "v") {
		return Release{}, fmt.Errorf("the latest release has no version tag (%q)", body.Tag)
	}
	if body.Draft || body.Prerelease || v.pre != "" {
		return Release{}, ErrNoRelease
	}
	rel := Release{Version: strings.TrimPrefix(body.Tag, "v"), Page: body.Page}
	for _, a := range body.Assets {
		switch a.Name {
		case AssetName:
			rel.exeURL, rel.exeSize = a.URL, a.Size
		case SumsName:
			rel.sumsURL = a.URL
		}
	}
	if rel.exeURL == "" || rel.sumsURL == "" {
		return Release{}, fmt.Errorf("release %s has no %s or %s: %w", body.Tag, AssetName, SumsName, ErrNoRelease)
	}
	return rel, nil
}

// Download fetches the release's exe into dir (replacing an earlier
// download), checks it against SHA256SUMS.txt and returns its path.
// progress (may be nil) is called from this goroutine with the bytes
// received and the total (-1 when unknown).
func (c Client) Download(ctx context.Context, rel Release, dir string, progress func(done, total int64)) (string, error) {
	want, err := c.checksum(ctx, rel)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, AssetName)
	part := path + ".part"
	_ = os.Remove(part)

	resp, err := c.get(ctx, rel.exeURL, 10*time.Minute, "application/octet-stream")
	if err != nil {
		return "", fmt.Errorf("downloading %s: %w", AssetName, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("downloading %s: server answered %s", AssetName, resp.Status)
	}
	total := resp.ContentLength
	if total <= 0 {
		total = rel.exeSize
	}
	if total > maxExeBytes {
		return "", fmt.Errorf("downloading %s: unexpected size %d bytes", AssetName, total)
	}
	out, err := os.Create(part)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	w := &progressWriter{w: io.MultiWriter(out, h), total: total, fn: progress}
	n, err := io.Copy(w, io.LimitReader(resp.Body, maxExeBytes+1))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	fail := func(err error) (string, error) {
		_ = os.Remove(part)
		return "", err
	}
	switch {
	case err != nil:
		return fail(fmt.Errorf("downloading %s: %w", AssetName, err))
	case n > maxExeBytes:
		return fail(fmt.Errorf("downloading %s: the file is larger than expected", AssetName))
	case total > 0 && n != total:
		return fail(fmt.Errorf("downloading %s: got %d of %d bytes", AssetName, n, total))
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return fail(fmt.Errorf("%s does not match its checksum (got %s, want %s)", AssetName, got, want))
	}
	if err := os.Rename(part, path); err != nil {
		_ = os.Remove(path)
		if err := os.Rename(part, path); err != nil {
			return fail(err)
		}
	}
	return path, nil
}

// checksum reads the exe's SHA-256 from the release's SHA256SUMS.txt.
func (c Client) checksum(ctx context.Context, rel Release) (string, error) {
	resp, err := c.get(ctx, rel.sumsURL, time.Minute, "application/octet-stream")
	if err != nil {
		return "", fmt.Errorf("downloading %s: %w", SumsName, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("downloading %s: server answered %s", SumsName, resp.Status)
	}
	sum, ok := parseSums(io.LimitReader(resp.Body, maxSumsBytes), AssetName)
	if !ok {
		return "", fmt.Errorf("%s lists no checksum for %s", SumsName, AssetName)
	}
	return sum, nil
}

// parseSums finds name in sha256sum output ("<hex>  <name>", or
// "<hex> *<name>" for binary mode).
func parseSums(r io.Reader, name string) (string, bool) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) != 2 || strings.TrimPrefix(fields[1], "*") != name {
			continue
		}
		sum := strings.ToLower(fields[0])
		if b, err := hex.DecodeString(sum); err == nil && len(b) == sha256.Size {
			return sum, true
		}
	}
	return "", false
}

type progressWriter struct {
	w     io.Writer
	done  int64
	total int64
	fn    func(done, total int64)
}

func (p *progressWriter) Write(b []byte) (int, error) {
	n, err := p.w.Write(b)
	p.done += int64(n)
	if p.fn != nil {
		p.fn(p.done, p.total)
	}
	return n, err
}

// --- versions ----------------------------------------------------------------

type version struct {
	nums [3]int
	pre  string // pre-release suffix without the "-", e.g. "rc.1" or "dev"
}

// parse reads major.minor.patch with an optional -suffix.
func parse(s string) (version, bool) {
	core, pre, _ := strings.Cut(s, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return version{}, false
	}
	var v version
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || p == "" || p[0] == '+' {
			return version{}, false
		}
		v.nums[i] = n
	}
	v.pre = pre
	return v, true
}

// Newer reports whether release version latest is newer than current. A
// full release is newer than a pre-release or development build of the
// same numbers (0.8.0 > 0.8.0-dev). An unreadable current version (a
// development build without one) counts as older than any release.
func Newer(latest, current string) bool {
	l, ok := parse(latest)
	if !ok || l.pre != "" {
		return false
	}
	c, ok := parse(current)
	if !ok {
		return true
	}
	for i := range l.nums {
		if l.nums[i] != c.nums[i] {
			return l.nums[i] > c.nums[i]
		}
	}
	return c.pre != ""
}
