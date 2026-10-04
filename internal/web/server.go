// Package web serves the panel (the Windows counterpart of the LuCI app) on 127.0.0.1 and its
// JSON API. Access needs the token stored in an admin-only file; the launcher passes it once and
// the browser keeps it as an HttpOnly SameSite=Strict cookie.
package web

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/zapretmanager/zmwin/internal/app"
	"github.com/zapretmanager/zmwin/internal/byetube"
)

//go:embed ui
var uiFS embed.FS

func tokenPath() string { return app.P("state", "token") }

// Token returns (creating if needed) the panel access token.
func Token() string {
	t := strings.TrimSpace(app.ReadText(tokenPath()))
	if len(t) >= 32 {
		return t
	}
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	t = hex.EncodeToString(b)
	_ = app.WriteText(tokenPath(), t)
	return t
}

func Port() int { return app.S().Port }

func BaseURL() string { return fmt.Sprintf("http://127.0.0.1:%d", Port()) }

// LoginURL is what the launcher opens: it sets the cookie and redirects to the panel.
func LoginURL() string { return BaseURL() + "/login?t=" + Token() }

type handler func(ctx context.Context, a Args) (any, error)

var routes = map[string]handler{}

func reg(name string, h handler) { routes[name] = h }

// Args is the decoded JSON body of an API call.
type Args map[string]any

func (a Args) S(k string) string {
	switch v := a[k].(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	}
	return ""
}

func (a Args) B(k string) bool {
	switch v := a[k].(type) {
	case bool:
		return v
	case string:
		return v == "1" || v == "true" || v == "on"
	case float64:
		return v != 0
	}
	return false
}

func (a Args) I(k string) int { n, _ := strconv.Atoi(a.S(k)); return n }

// Into decodes a nested object argument into a struct.
func (a Args) Into(k string, v any) error {
	b, err := json.Marshal(a[k])
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func hostOK(r *http.Request) bool {
	h := r.Host
	p := strconv.Itoa(Port())
	return h == "127.0.0.1:"+p || h == "localhost:"+p || h == "[::1]:"+p
}

func authed(r *http.Request) bool {
	c, err := r.Cookie("zm_token")
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(c.Value), []byte(Token())) == 1
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func Handler() http.Handler {
	registerAll()
	mux := http.NewServeMux()
	sub, _ := fs.Sub(uiFS, "ui")
	static := http.FileServer(http.FS(sub))

	mux.HandleFunc("/byetube.pac", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ns-proxy-autoconfig")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = io.WriteString(w, byetube.PAC())
	})
	mux.HandleFunc("/skin/", func(w http.ResponseWriter, r *http.Request) {
		if !authed(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		skinHandler(w, r)
	})
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		t := r.URL.Query().Get("t")
		if subtle.ConstantTimeCompare([]byte(t), []byte(Token())) != 1 {
			http.Error(w, "Неверный токен. Откройте панель ярлыком Zapret Manager.", http.StatusForbidden)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "zm_token", Value: t, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: 86400 * 365})
		next := r.URL.Query().Get("next")
		if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.ContainsAny(next, "\\\r\n") {
			next = "/"
		}
		http.Redirect(w, r, next, http.StatusFound)
	})
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("X-ZM") != "1" {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST + X-ZM"})
			return
		}
		if !authed(r) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "нет доступа — откройте панель ярлыком Zapret Manager", "auth": "1"})
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/api/")
		h, ok := routes[name]
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "неизвестная команда " + name})
			return
		}
		args := Args{}
		body, _ := io.ReadAll(io.LimitReader(r.Body, 8<<20))
		if len(body) > 0 {
			if err := json.Unmarshal(body, &args); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "неверный JSON"})
				return
			}
		}
		res, err := safeCall(r.Context(), h, args)
		if err != nil {
			app.Logf("api %s: %v", name, err)
			writeJSON(w, http.StatusOK, map[string]any{"error": err.Error()})
			return
		}
		if res == nil {
			res = map[string]bool{"ok": true}
		}
		writeJSON(w, http.StatusOK, res)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if !authed(r) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `<!doctype html><meta charset="utf-8"><title>Zapret Manager</title><body style="font:15px system-ui;padding:40px;background:#0d1117;color:#e6edf3">
<h2>Zapret Manager</h2><p>Откройте панель ярлыком «Zapret Manager» на рабочем столе или командой <code>ZapretManager.exe open</code> — она передаст ключ доступа.</p>`)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			serveIndex(w)
			return
		}
		static.ServeHTTP(w, r)
	})
	return guard(mux)
}

// guard blocks DNS-rebinding (foreign Host) and adds security headers.
func guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !hostOK(r) {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

func safeCall(ctx context.Context, h handler, a Args) (res any, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("внутренняя ошибка: %v", r)
		}
	}()
	return h(ctx, a)
}

// Serve listens on 127.0.0.1:<port> until ctx is cancelled.
func Serve(ctx context.Context) error {
	byetube.PACURL = func() string { return BaseURL() + "/byetube.pac" }
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", Port()))
	if err != nil {
		return fmt.Errorf("порт %d занят: %v", Port(), err)
	}
	srv := &http.Server{Handler: Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		c, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(c)
	}()
	app.Logf("panel on %s", BaseURL())
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// serveIndex renders index.html with the saved theme already applied (no flash of the wrong skin).
func serveIndex(w http.ResponseWriter) {
	b, err := uiFS.ReadFile("ui/index.html")
	if err != nil {
		http.Error(w, "ui missing", http.StatusInternalServerError)
		return
	}
	attr := ""
	switch t := app.S().Theme; t {
	case "light", "dark":
		attr = ` data-theme="` + t + `"`
	case "ultrakill":
		attr = ` data-theme="dark" data-skin="ultrakill"`
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(strings.Replace(string(b), `<html lang="ru">`, `<html lang="ru"`+attr+`>`, 1)))
}
