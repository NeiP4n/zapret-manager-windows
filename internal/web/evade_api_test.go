package web

// Проверка панельного контракта адаптивного ядра: авторизация, CSRF-заголовок и формат
// ответа. Тест пишет в app.Base, поэтому требует ZM_BASE, иначе пропускается — в
// C:\ProgramData без прав администратора он бы молча ломал токен.

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/zapretmanager/zmwin/internal/app"
)

func setupPanel(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	if os.Getenv("ZM_BASE") == "" {
		t.Skip("нужен ZM_BASE: тест пишет токен и настройки в app.Base")
	}
	app.EnsureDirs()
	tok := Token()
	if tok == "" {
		t.Fatal("токен не создан")
	}
	srv := httptest.NewServer(Handler())
	t.Cleanup(srv.Close)

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("разбор URL: %v", err)
	}
	_, portStr, err := net.SplitHostPort(u.Host)
	if err != nil {
		t.Fatalf("порт: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("порт %q: %v", portStr, err)
	}
	// guard пропускает только Host с портом панели — подставляем порт тестового сервера.
	app.Update(func(s *app.Settings) { s.Port = port })
	return srv, tok
}

func post(t *testing.T, srv *httptest.Server, tok, name, body string, csrf bool) (*http.Response, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/"+name, bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("запрос: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if csrf {
		req.Header.Set("X-ZM", "1")
	}
	if tok != "" {
		req.AddCookie(&http.Cookie{Name: "zm_token", Value: tok})
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("вызов %s: %v", name, err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp, out
}

func TestEvadeStrategyEndpoint(t *testing.T) {
	srv, tok := setupPanel(t)

	resp, out := post(t, srv, tok, "evade_strategy", `{"seed":5}`, true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("код %d: %v", resp.StatusCode, out)
	}
	lines, _ := out["lines"].([]any)
	if len(lines) < 3 {
		t.Fatalf("ожидался блок стратегии, получено %v", out)
	}
	first, _ := lines[0].(string)
	if !strings.HasPrefix(first, "#zm") {
		t.Fatalf("маркер %q, ожидался #zm…", first)
	}
	joined := ""
	for _, l := range lines {
		s, _ := l.(string)
		joined += s + "\n"
	}
	if !strings.Contains(joined, "--dpi-desync=") || !strings.Contains(joined, "--filter-tcp=443") {
		t.Fatalf("в стратегии нет обязательных флагов:\n%s", joined)
	}
}

func TestEvadeEndpointsRequireAuthAndCSRF(t *testing.T) {
	srv, tok := setupPanel(t)

	if resp, _ := post(t, srv, tok, "evade_strategy", `{"seed":1}`, false); resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("без X-ZM код %d, ожидался 405", resp.StatusCode)
	}
	if resp, _ := post(t, srv, "", "evade_strategy", `{"seed":1}`, true); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("без токена код %d, ожидался 401", resp.StatusCode)
	}
}

func TestEvadeModelEndpoint(t *testing.T) {
	srv, tok := setupPanel(t)
	resp, out := post(t, srv, tok, "evade_model", `{}`, true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("код %d", resp.StatusCode)
	}
	if out["loaded"] != true {
		t.Fatalf("модель не загружена: %v", out)
	}
	meta, _ := out["meta"].(map[string]any)
	if meta == nil || meta["trained_samples"] == nil {
		t.Fatalf("нет метаданных: %v", out)
	}
}

// Сквозная проверка: HTTP-эндпоинт → пробы по реальному TLS-серверу → классификация
// → модель → JSON для панели. Сервер локальный, поэтому тест не зависит от сети.
func TestEvadeProbeEndToEnd(t *testing.T) {
	srv, tok := setupPanel(t)

	backend := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "307200")
		_, _ = w.Write(make([]byte, 300<<10))
	}))
	defer backend.Close()
	addr := strings.TrimPrefix(backend.URL, "https://")

	resp, out := post(t, srv, tok, "evade_probe", `{"hosts":["`+addr+`"]}`, true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("код %d: %v", resp.StatusCode, out)
	}
	items, _ := out["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("ожидался один вердикт, получено %v", out)
	}
	item, _ := items[0].(map[string]any)
	if item["class"] != "ok" {
		t.Fatalf("класс %v (%v)", item["class"], item["reason"])
	}
	if item["plan"] != "direct" {
		t.Fatalf("план %v", item["plan"])
	}
	model, _ := item["model"].(map[string]any)
	if model == nil || model["class"] != "ok" {
		t.Fatalf("модель не подтвердила решение: %v", item)
	}
	if ev, _ := item["evidence"].(map[string]any); ev == nil || ev["bulk_bytes"] == nil {
		t.Fatalf("в ответе нет замеров: %v", item)
	}
}

func TestEvadeProbeRejectsEmptyHosts(t *testing.T) {
	srv, tok := setupPanel(t)
	resp, out := post(t, srv, tok, "evade_probe", `{"hosts":[]}`, true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("код %d", resp.StatusCode)
	}
	if out["error"] == nil {
		t.Fatalf("ожидалась ошибка про пустой список, получено %v", out)
	}
}
