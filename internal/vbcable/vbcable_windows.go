package vbcable

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/motivated2die/resound/internal/elevate"
	"github.com/motivated2die/resound/internal/logx"
	"github.com/motivated2die/resound/internal/routing"
	"github.com/motivated2die/resound/internal/wincom"
)

const (
	// PackURL is VB-Audio's official driver pack (Windows 10/11, x64 and
	// ARM64), linked from vb-audio.com/Cable.
	PackURL = "https://download.vb-audio.com/Download_CABLE/VBCABLE_Driver_Pack45.zip"

	// SetupArgs install silently (-i -h). Windows still shows its own
	// prompts: UAC, and on some PCs "Install this device software?".
	SetupArgs = "-i -h"

	maxZipBytes  = 32 << 20
	setupTimeout = 10 * time.Minute
)

// Signers whose signature is accepted on the setup: the certificate of
// VB-Audio's author, and the company name in case it is renamed.
var trustedSigners = []string{"BUREL VINCENT", "VB-AUDIO"}

// ErrCancelled means the user declined the UAC prompt.
var ErrCancelled = elevate.ErrCancelled

// ErrStillRunning means the setup did not finish in time; it keeps running.
var ErrStillRunning = elevate.ErrTimeout

// setupExe is the setup for this Windows: the x64 one also serves ARM64.
func setupExe() string {
	if runtime.GOARCH == "386" {
		return "VBCABLE_Setup.exe"
	}
	return "VBCABLE_Setup_x64.exe"
}

// Dir is where the pack is downloaded and unpacked, inside the app's data
// folder (so uninstalling removes it too).
func Dir(dataDir string) string { return filepath.Join(dataDir, "vbcable") }

// Download fetches the driver pack into dir, unpacks it and checks the
// setup's signature. It returns the setup's path. progress (may be nil) is
// called from this goroutine with the bytes received and the total (-1 when
// unknown).
func Download(ctx context.Context, dir string, progress func(done, total int64)) (string, error) {
	if err := os.RemoveAll(dir); err != nil {
		return "", fmt.Errorf("clearing %s: %w", dir, err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	zipPath := filepath.Join(dir, filepath.Base(PackURL))
	if err := fetch(ctx, PackURL, zipPath, progress); err != nil {
		return "", err
	}
	pack := filepath.Join(dir, "pack")
	if err := extract(zipPath, pack); err != nil {
		return "", err
	}
	setup := filepath.Join(pack, setupExe())
	if _, err := os.Stat(setup); err != nil {
		return "", fmt.Errorf("the driver pack has no %s", setupExe())
	}
	signer, err := verifySigner(setup)
	if err != nil {
		return "", fmt.Errorf("%s: %w", setupExe(), err)
	}
	logx.Printf("vbcable: %s is signed by %q", setupExe(), signer)
	return setup, nil
}

// fetch downloads u to path over HTTPS, following redirects only within
// vb-audio.com.
func fetch(ctx context.Context, u, path string, progress func(done, total int64)) error {
	client := &http.Client{
		Timeout: 5 * time.Minute,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			if !vbAudioURL(req.URL) {
				return fmt.Errorf("redirected away from vb-audio.com to %s", req.URL.Host)
			}
			return nil
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("downloading VB-CABLE: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("downloading VB-CABLE: server answered %s", resp.Status)
	}
	if resp.ContentLength > maxZipBytes {
		return fmt.Errorf("downloading VB-CABLE: unexpected size %d bytes", resp.ContentLength)
	}
	out, err := os.Create(path)
	if err != nil {
		return err
	}
	w := &progressWriter{w: out, total: resp.ContentLength, fn: progress}
	n, err := io.Copy(w, io.LimitReader(resp.Body, maxZipBytes+1))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	switch {
	case err != nil:
		return fmt.Errorf("downloading VB-CABLE: %w", err)
	case n > maxZipBytes:
		return errors.New("downloading VB-CABLE: the file is larger than expected")
	case resp.ContentLength > 0 && n != resp.ContentLength:
		return fmt.Errorf("downloading VB-CABLE: got %d of %d bytes", n, resp.ContentLength)
	}
	logx.Printf("vbcable: downloaded %s (%d bytes)", u, n)
	return nil
}

func vbAudioURL(u *url.URL) bool {
	host := strings.ToLower(u.Hostname())
	return u.Scheme == "https" && (host == "vb-audio.com" || strings.HasSuffix(host, ".vb-audio.com"))
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

// RunSetup runs the setup elevated and silently, and waits for it. It
// blocks for as long as the user takes with Windows' prompts, so call it
// off the UI thread. The setup's exit code is only logged: it is not
// documented, so success is judged by the devices appearing.
func RunSetup(setup string) error {
	logx.Printf("vbcable: running %s %s", setup, SetupArgs)
	// Shown normally: with -h it has no window, and should a future setup
	// ignore -h, its dialogs must stay answerable rather than hidden.
	code, err := elevate.RunFile(setup, SetupArgs, filepath.Dir(setup), windows.SW_SHOWNORMAL, setupTimeout)
	if err != nil {
		return err
	}
	logx.Printf("vbcable: setup exited with code %d", code)
	return nil
}

// BootTime is when Windows started.
func BootTime() time.Time {
	ms, _, _ := procGetTickCount64.Call()
	return time.Now().Add(-time.Duration(ms) * time.Millisecond).Truncate(time.Second)
}

var procGetTickCount64 = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetTickCount64")

// --- default devices ---------------------------------------------------------

// SnapshotDefaults reads the current default playback and recording devices.
func SnapshotDefaults() (Defaults, error) {
	var d Defaults
	err := wincom.Run(func() error {
		e, err := wincom.NewEnumerator()
		if err != nil {
			return err
		}
		defer e.Release()
		d = Defaults{
			Render:       wincom.DefaultID(e, wincom.Render, wincom.RoleConsole),
			RenderComms:  wincom.DefaultID(e, wincom.Render, wincom.RoleCommunications),
			Capture:      wincom.DefaultID(e, wincom.Capture, wincom.RoleConsole),
			CaptureComms: wincom.DefaultID(e, wincom.Capture, wincom.RoleCommunications),
		}
		return nil
	})
	return d, err
}

// RestoreDefaults puts back each default that Windows moved to the cable
// during the install. Defaults the user chose themselves (anything but the
// cable) and devices that are gone are left alone. It returns what it
// changed, for the log and the panel.
func RestoreDefaults(d Defaults) (changed []string, err error) {
	err = wincom.Run(func() error {
		e, err := wincom.NewEnumerator()
		if err != nil {
			return err
		}
		defer e.Release()
		var errs []error
		for _, flow := range []uint32{wincom.Render, wincom.Capture} {
			eps, err := wincom.Enumerate(e, flow)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			names := make(map[string]string, len(eps))
			for _, ep := range eps {
				names[ep.ID] = ep.Name
			}
			prevConsole, prevComms := d.Render, d.RenderComms
			if flow == wincom.Capture {
				prevConsole, prevComms = d.Capture, d.CaptureComms
			}
			for _, r := range []struct {
				prev  string
				roles []uint32
			}{
				{prevConsole, []uint32{wincom.RoleConsole, wincom.RoleMultimedia}},
				{prevComms, []uint32{wincom.RoleCommunications}},
			} {
				cur := wincom.DefaultID(e, flow, r.roles[0])
				prevName, prevActive := names[r.prev]
				if r.prev == "" || cur == r.prev || !prevActive || routing.IsCableName(prevName) || !routing.IsCableName(names[cur]) {
					continue
				}
				for _, role := range r.roles {
					if err := wincom.SetDefaultEndpoint(r.prev, role); err != nil {
						errs = append(errs, fmt.Errorf("setting %q as default: %w", prevName, err))
					}
				}
				changed = append(changed, prevName)
			}
		}
		return errors.Join(errs...)
	})
	return changed, err
}

// --- signature -----------------------------------------------------------------

// verifySigner checks that the file has a valid Authenticode signature that
// chains to a trusted root, and that its signer is VB-Audio. It returns the
// signer's name.
func verifySigner(path string) (string, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	data := &windows.WinTrustData{
		Size:             uint32(unsafe.Sizeof(windows.WinTrustData{})),
		UIChoice:         windows.WTD_UI_NONE,
		RevocationChecks: windows.WTD_REVOKE_NONE, // UAC checks again when it starts
		UnionChoice:      windows.WTD_CHOICE_FILE,
		StateAction:      windows.WTD_STATEACTION_VERIFY,
		FileOrCatalogOrBlobOrSgnrOrCert: unsafe.Pointer(&windows.WinTrustFileInfo{
			Size:     uint32(unsafe.Sizeof(windows.WinTrustFileInfo{})),
			FilePath: p,
		}),
	}
	verifyErr := windows.WinVerifyTrustEx(windows.InvalidHWND, &windows.WINTRUST_ACTION_GENERIC_VERIFY_V2, data)
	data.StateAction = windows.WTD_STATEACTION_CLOSE
	_ = windows.WinVerifyTrustEx(windows.InvalidHWND, &windows.WINTRUST_ACTION_GENERIC_VERIFY_V2, data)
	if verifyErr != nil {
		return "", fmt.Errorf("signature not valid: %w", verifyErr)
	}
	name, err := signerName(p)
	if err != nil {
		return "", fmt.Errorf("reading the signer: %w", err)
	}
	upper := strings.ToUpper(name)
	for _, s := range trustedSigners {
		if strings.Contains(upper, s) {
			return name, nil
		}
	}
	return "", fmt.Errorf("signed by %q, not by VB-Audio", name)
}

var (
	crypt32              = windows.NewLazySystemDLL("crypt32.dll")
	procCryptMsgGetParam = crypt32.NewProc("CryptMsgGetParam")
	procCryptMsgClose    = crypt32.NewProc("CryptMsgClose")
)

const cmsgSignerInfoParam = 6

// cmsgSignerInfoHead is the start of CMSG_SIGNER_INFO: enough to find the
// signer's certificate by issuer and serial number.
type cmsgSignerInfoHead struct {
	Version      uint32
	Issuer       windows.CertNameBlob
	SerialNumber windows.CryptIntegerBlob
}

// signerName returns the display name of the file's primary signer: the
// certificate that signed it, not just any certificate in the signature.
func signerName(path *uint16) (string, error) {
	var encoding, contentType, formatType uint32
	var store, msg windows.Handle
	if err := windows.CryptQueryObject(windows.CERT_QUERY_OBJECT_FILE, unsafe.Pointer(path),
		windows.CERT_QUERY_CONTENT_FLAG_PKCS7_SIGNED_EMBED, windows.CERT_QUERY_FORMAT_FLAG_BINARY, 0,
		&encoding, &contentType, &formatType, &store, &msg, nil); err != nil {
		return "", err
	}
	defer windows.CertCloseStore(store, 0)
	defer procCryptMsgClose.Call(uintptr(msg))

	var size uint32
	if r, _, err := procCryptMsgGetParam.Call(uintptr(msg), cmsgSignerInfoParam, 0, 0, uintptr(unsafe.Pointer(&size))); r == 0 {
		return "", fmt.Errorf("CryptMsgGetParam: %w", err)
	}
	if size < uint32(unsafe.Sizeof(cmsgSignerInfoHead{})) {
		return "", errors.New("signer info too small")
	}
	// Pointer-aligned buffer: the struct holds pointers into it.
	buf := make([]uintptr, (size+7)/8)
	if r, _, err := procCryptMsgGetParam.Call(uintptr(msg), cmsgSignerInfoParam, 0,
		uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size))); r == 0 {
		return "", fmt.Errorf("CryptMsgGetParam: %w", err)
	}
	info := (*cmsgSignerInfoHead)(unsafe.Pointer(&buf[0]))
	certInfo := windows.CertInfo{Issuer: info.Issuer, SerialNumber: info.SerialNumber}
	cert, err := windows.CertFindCertificateInStore(store, windows.X509_ASN_ENCODING|windows.PKCS_7_ASN_ENCODING, 0,
		windows.CERT_FIND_SUBJECT_CERT, unsafe.Pointer(&certInfo), nil)
	runtime.KeepAlive(buf)
	if err != nil {
		return "", fmt.Errorf("finding the signer certificate: %w", err)
	}
	defer windows.CertFreeCertificateContext(cert)
	name := make([]uint16, 256)
	n := windows.CertGetNameString(cert, windows.CERT_NAME_SIMPLE_DISPLAY_TYPE, 0, nil, &name[0], uint32(len(name)))
	if n <= 1 {
		return "", errors.New("signer certificate has no name")
	}
	return windows.UTF16ToString(name[:n]), nil
}
