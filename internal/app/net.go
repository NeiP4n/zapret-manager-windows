package app

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const UA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) ZapretManager/" + Version

var (
	GHRaw  = "https://raw.githubusercontent.com"
	GHMain = "https://github.com"
)

// Mirrors are GitHub proxies the user can switch to when github is throttled
// (Windows analogue of the router's package-feed mirror switch).
var Mirrors = []struct{ ID, Name, Prefix string }{
	{"direct", "Напрямую (GitHub)", ""},
	{"ghproxy", "gh-proxy.org", "https://gh-proxy.org/"},
	{"ghfast", "ghfast.top", "https://ghfast.top/"},
	{"ghllkk", "gh.llkk.cc", "https://gh.llkk.cc/"},
}

// Mirrored rewrites github URLs through the selected mirror.
func Mirrored(u string) string {
	pre := S().Mirror
	if pre == "" {
		return u
	}
	if strings.HasPrefix(u, "https://github.com/") || strings.HasPrefix(u, "https://raw.githubusercontent.com/") ||
		strings.HasPrefix(u, "https://objects.githubusercontent.com/") {
		return pre + u
	}
	return u
}

var httpClient = &http.Client{Timeout: 0}

// Offline disables all downloads (tests).
var Offline = os.Getenv("ZM_OFFLINE") != ""

var errOffline = fmt.Errorf("offline")

func newReq(ctx context.Context, method, u string) (*http.Request, error) {
	r, err := http.NewRequestWithContext(ctx, method, u, nil)
	if err != nil {
		return nil, err
	}
	r.Header.Set("User-Agent", UA)
	return r, nil
}

// Fetch downloads a URL into memory (small files: lists, configs, versions).
func Fetch(ctx context.Context, u string, timeout time.Duration) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if Offline {
		return nil, errOffline
	}
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := newReq(c, "GET", Mirrored(u))
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 64<<20))
}

// FetchText fetches and rejects HTML error pages (router: grep -qi '<html').
func FetchText(ctx context.Context, u string, timeout time.Duration) (string, error) {
	b, err := Fetch(ctx, u, timeout)
	if err != nil {
		return "", err
	}
	head := strings.ToLower(string(b[:min(len(b), 512)]))
	if strings.Contains(head, "<html") || strings.Contains(head, "<!doctype") {
		return "", fmt.Errorf("вместо файла пришла веб-страница")
	}
	return strings.ReplaceAll(string(b), "\r", ""), nil
}

// Download saves a URL to dst with retries (router: wget in a 5-attempt loop).
func Download(ctx context.Context, u, dst string, attempts int, j *Job) error {
	if attempts < 1 {
		attempts = 1
	}
	if Offline {
		return errOffline
	}
	var last error
	for i := 1; i <= attempts; i++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		last = downloadOnce(ctx, u, dst)
		if last == nil {
			return nil
		}
		if j != nil {
			j.Say("!! Попытка %d из %d: %v", i, attempts, last)
		}
		time.Sleep(time.Duration(i) * time.Second)
	}
	return fmt.Errorf("не удалось скачать %s: %v", u, last)
}

func downloadOnce(ctx context.Context, u, dst string) error {
	c, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	req, err := newReq(c, "GET", Mirrored(u))
	if err != nil {
		return err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	_ = os.MkdirAll(filepath.Dir(dst), 0o755)
	tmp := dst + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	n, err := io.Copy(f, resp.Body)
	_ = f.Close()
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if resp.ContentLength > 0 && n != resp.ContentLength {
		_ = os.Remove(tmp)
		return fmt.Errorf("файл скачан не целиком")
	}
	return os.Rename(tmp, dst)
}

// LatestTag resolves a repo's latest release tag through the /releases/latest redirect
// (no GitHub API, so no rate limits — same trick as the router backend).
func LatestTag(ctx context.Context, repo string) string {
	c, cancel := context.WithTimeout(ctxOr(ctx), 12*time.Second)
	defer cancel()
	req, err := newReq(c, "HEAD", Mirrored(GHMain+"/"+repo+"/releases/latest"))
	if err != nil {
		return ""
	}
	cl := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := cl.Do(req)
	if err != nil {
		return ""
	}
	resp.Body.Close()
	loc := resp.Header.Get("Location")
	if i := strings.LastIndex(loc, "/tag/"); i >= 0 {
		return loc[i+5:]
	}
	return ""
}

func ctxOr(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

// Unzip extracts files from a zip. keep(name) returns the destination path relative to dst
// ("" skips the entry); nil keeps everything as-is.
func Unzip(src, dst string, keep func(name string) string) (int, error) {
	zr, err := zip.OpenReader(src)
	if err != nil {
		return 0, err
	}
	defer zr.Close()
	n := 0
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		rel := f.Name
		if keep != nil {
			rel = keep(f.Name)
			if rel == "" {
				continue
			}
		}
		out := filepath.Join(dst, filepath.FromSlash(rel))
		if !strings.HasPrefix(filepath.Clean(out), filepath.Clean(dst)) {
			continue
		}
		if err := extractZipFile(f, out); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func extractZipFile(f *zip.File, out string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	_ = os.MkdirAll(filepath.Dir(out), 0o755)
	w, err := os.Create(out + ".zmtmp")
	if err != nil {
		return err
	}
	if _, err := io.Copy(w, rc); err != nil {
		w.Close()
		return err
	}
	w.Close()
	_ = os.Remove(out)
	return os.Rename(out+".zmtmp", out)
}

// UnTarGz extracts a .tgz into dst (used for the metacubexd web UI).
func UnTarGz(src, dst string, strip int) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		parts := strings.Split(strings.TrimPrefix(h.Name, "./"), "/")
		if len(parts) <= strip {
			continue
		}
		rel := filepath.Join(parts[strip:]...)
		out := filepath.Join(dst, rel)
		if !strings.HasPrefix(filepath.Clean(out), filepath.Clean(dst)) {
			continue
		}
		switch h.Typeflag {
		case tar.TypeDir:
			_ = os.MkdirAll(out, 0o755)
		case tar.TypeReg:
			_ = os.MkdirAll(filepath.Dir(out), 0o755)
			w, err := os.Create(out)
			if err != nil {
				return err
			}
			_, err = io.Copy(w, tr)
			w.Close()
			if err != nil {
				return err
			}
		}
	}
}

// CopyFile copies src to dst.
func CopyFile(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return WriteFileAtomic(dst, b)
}
