// Package tg manages Telegram WebSocket proxies (router page "TG WS Proxy"): the Go port
// (SOCKS5 or MTProto) and the Rust port (MTProto, FakeTLS) — both ship Windows builds.
package tg

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zapretmanager/zmwin/internal/app"
	"github.com/zapretmanager/zmwin/internal/osx"
	"github.com/zapretmanager/zmwin/internal/sysinfo"
)

type Variant struct {
	ID      string
	Title   string
	Repo    string
	Asset   string // release asset (with {v} placeholder where needed)
	Exe     string
	Default int
	Zip     bool
}

var Variants = []Variant{
	{ID: "go", Title: "TG WS Proxy (Go)", Repo: "d0mhate/-tg-ws-proxy-Manager-go", Asset: "tg-ws-proxy-windows-amd64.exe", Exe: "tg-ws-proxy-go.exe", Default: 1080},
	{ID: "rs", Title: "TG WS Proxy (Rust, MTProto)", Repo: "valnesfjord/tg-ws-proxy-rs", Asset: "tg-ws-proxy-x86_64-pc-windows-gnu.zip", Exe: "tg-ws-proxy-rs.exe", Default: 1443, Zip: true},
}

type Config struct {
	Installed  bool   `json:"installed"`
	Enabled    bool   `json:"enabled"`
	Version    string `json:"version"`
	Mode       string `json:"mode"` // go: socks5|mtproto
	Port       int    `json:"port"`
	LAN        bool   `json:"lan"` // listen on 0.0.0.0 for phones / other PCs
	Secret     string `json:"secret"`
	User       string `json:"user"`
	Pass       string `json:"pass"`
	FakeTLS    string `json:"faketls"`    // rs: --listen-faketls-domain
	CFDefault  bool   `json:"cf_default"` // rs: --default-domains / go: --cf-proxy
	AutoMin    int    `json:"auto_min"`   // periodic restart, minutes (0 = off)
	LastAutoAt int64  `json:"last_auto"`
}

var (
	kv = app.NewKV("tg")
	mu sync.Mutex
)

type state map[string]*Config

func load() state {
	s := state{}
	kv.Load(&s)
	for _, v := range Variants {
		if s[v.ID] == nil {
			s[v.ID] = &Config{Port: v.Default, Mode: "socks5"}
		}
		if s[v.ID].Port == 0 {
			s[v.ID].Port = v.Default
		}
	}
	return s
}

func save(s state) { _ = kv.Save(s) }

func variant(id string) (Variant, error) {
	for _, v := range Variants {
		if v.ID == id {
			return v, nil
		}
	}
	return Variant{}, fmt.Errorf("неизвестный вариант прокси")
}

func dir() string               { return filepath.Join(app.ToolsDir, "tg") }
func exePath(v Variant) string  { return filepath.Join(dir(), v.Exe) }
func procName(v Variant) string { return "tg-" + v.ID }
func fwName(v Variant) string   { return "ZM TG Proxy " + v.ID }
func installed(v Variant) bool  { return app.Exists(exePath(v)) || (app.Dev && load()[v.ID].Installed) }

func newSecret() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func args(v Variant, c *Config) []string {
	host := "127.0.0.1"
	if c.LAN {
		host = "0.0.0.0"
	}
	a := []string{"--host", host, "--port", strconv.Itoa(c.Port)}
	switch v.ID {
	case "go":
		a = append(a, "--mode", c.Mode)
		if c.Mode == "mtproto" {
			a = append(a, "--secret", c.Secret, "--link-ip", linkIP(c))
		} else if c.User != "" {
			a = append(a, "--username", c.User, "--password", c.Pass)
		}
		if c.CFDefault {
			a = append(a, "--cf-proxy")
		}
	case "rs":
		a = append(a, "--secret", c.Secret, "--link-ip", linkIP(c))
		if c.FakeTLS != "" {
			a = append(a, "--listen-faketls-domain", c.FakeTLS)
		}
		if c.CFDefault {
			a = append(a, "--default-domains")
		}
	}
	return a
}

func linkIP(c *Config) string {
	if c.LAN {
		return sysinfo.LanIP()
	}
	return "127.0.0.1"
}

// Link builds the tg:// link that adds the proxy to Telegram in one tap.
func Link(id string, c *Config) string {
	ip := linkIP(c)
	if id == "go" && c.Mode != "mtproto" {
		l := fmt.Sprintf("tg://socks?server=%s&port=%d", ip, c.Port)
		if c.User != "" {
			l += "&user=" + c.User + "&pass=" + c.Pass
		}
		return l
	}
	sec := "dd" + c.Secret
	if id == "rs" && c.FakeTLS != "" {
		sec = "ee" + c.Secret + hex.EncodeToString([]byte(c.FakeTLS))
	}
	return fmt.Sprintf("tg://proxy?server=%s&port=%d&secret=%s", ip, c.Port, sec)
}

func start(v Variant, c *Config) error {
	if !c.Enabled {
		osx.ProcStop(procName(v))
		return nil
	}
	if c.Secret == "" {
		c.Secret = newSecret()
	}
	if c.LAN {
		_ = osx.FirewallAllowInbound(fwName(v), "TCP", strconv.Itoa(c.Port))
	} else {
		osx.FirewallRemove(fwName(v))
	}
	return osx.ProcStart(osx.ProcSpec{Name: procName(v), Exe: exePath(v), Args: args(v, c), Dir: dir(), Restart: true})
}

type ItemStatus struct {
	ID       string        `json:"id"`
	Title    string        `json:"title"`
	Config   Config        `json:"config"`
	Running  bool          `json:"running"`
	Link     string        `json:"link"`
	Latest   string        `json:"latest,omitempty"`
	Newer    bool          `json:"newer"`
	Proc     osx.ProcState `json:"proc"`
	Conflict string        `json:"conflict,omitempty"`
}

func Status() []ItemStatus {
	mu.Lock()
	s := load()
	mu.Unlock()
	var out []ItemStatus
	for _, v := range Variants {
		c := s[v.ID]
		c.Installed = installed(v)
		st := ItemStatus{ID: v.ID, Title: v.Title, Config: *c, Proc: osx.ProcStatus(procName(v))}
		st.Running = st.Proc.Running
		st.Config.Pass = ""
		if c.Installed {
			st.Link = Link(v.ID, c)
		}
		if lt := latestCached(v); lt != "" {
			st.Latest = lt
			st.Newer = c.Version != "" && strings.TrimPrefix(lt, "v") != strings.TrimPrefix(c.Version, "v")
		}
		for _, o := range Variants {
			if o.ID != v.ID && s[o.ID].Enabled && s[o.ID].Port == c.Port && installed(o) {
				st.Conflict = fmt.Sprintf("порт %d занят прокси «%s» — оба на одном порту не заработают", c.Port, o.Title)
			}
		}
		out = append(out, st)
	}
	return out
}

var (
	latestMu sync.Mutex
	latestC  = map[string]string{}
	latestAt = map[string]time.Time{}
)

func latestCached(v Variant) string {
	latestMu.Lock()
	defer latestMu.Unlock()
	if time.Since(latestAt[v.ID]) < 6*time.Hour {
		return latestC[v.ID]
	}
	latestAt[v.ID] = time.Now()
	go func() {
		t := app.LatestTag(nil, v.Repo)
		latestMu.Lock()
		latestC[v.ID] = t
		latestMu.Unlock()
	}()
	return latestC[v.ID]
}

// Install downloads (or updates) a variant and starts it.
func Install(j *app.Job, id string) error {
	v, err := variant(id)
	if err != nil {
		return err
	}
	j.Say("==> Устанавливаем %s", v.Title)
	tag := app.LatestTag(j.Ctx(), v.Repo)
	if tag == "" {
		return fmt.Errorf("не удалось узнать последнюю версию %s", v.Repo)
	}
	j.Say("   ✓ Последняя версия: %s", tag)
	url := fmt.Sprintf("%s/%s/releases/download/%s/%s", app.GHMain, v.Repo, tag, v.Asset)
	_ = os.MkdirAll(dir(), 0o755)
	tmp := filepath.Join(app.TmpDir, v.Asset)
	j.Say("==> Скачиваем %s", v.Asset)
	if err := app.Download(j.Ctx(), url, tmp, 5, j); err != nil {
		return err
	}
	defer os.Remove(tmp)
	osx.ProcStop(procName(v))
	if v.Zip {
		if _, err := app.Unzip(tmp, dir(), func(n string) string {
			if strings.HasSuffix(strings.ToLower(n), ".exe") {
				return v.Exe
			}
			return ""
		}); err != nil {
			return err
		}
	} else if err := app.CopyFile(tmp, exePath(v)); err != nil {
		return err
	}
	mu.Lock()
	s := load()
	c := s[v.ID]
	c.Installed, c.Enabled, c.Version = true, true, tag
	if c.Secret == "" {
		c.Secret = newSecret()
	}
	err = start(v, c)
	save(s)
	mu.Unlock()
	if err != nil {
		return err
	}
	j.Say("==> Готово, %s запущен на порту %d", v.Title, c.Port)
	return nil
}

func Remove(j *app.Job, id string) error {
	v, err := variant(id)
	if err != nil {
		return err
	}
	j.Say("==> Удаляем %s", v.Title)
	osx.ProcStop(procName(v))
	osx.FirewallRemove(fwName(v))
	app.Remove(exePath(v))
	mu.Lock()
	s := load()
	*s[v.ID] = Config{Port: v.Default, Mode: "socks5"}
	save(s)
	mu.Unlock()
	j.Say("==> Готово")
	return nil
}

// Action: start | stop | restart | regen (new secret).
func Action(id, action string) error {
	v, err := variant(id)
	if err != nil {
		return err
	}
	mu.Lock()
	defer mu.Unlock()
	s := load()
	c := s[v.ID]
	defer save(s)
	switch action {
	case "start", "restart":
		c.Enabled = true
	case "stop":
		c.Enabled = false
	case "regen":
		c.Secret = newSecret()
	default:
		return fmt.Errorf("неизвестное действие")
	}
	return start(v, c)
}

var hostRx = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)

// Configure updates settings; fields not given keep their value.
func Configure(id string, in Config) error {
	v, err := variant(id)
	if err != nil {
		return err
	}
	if in.Port != 0 && (in.Port < 1 || in.Port > 65535) {
		return fmt.Errorf("неверный порт")
	}
	if in.Secret != "" && !regexp.MustCompile(`^[0-9a-fA-F]{32}$`).MatchString(in.Secret) {
		return fmt.Errorf("secret — 32 шестнадцатеричных символа")
	}
	if in.FakeTLS != "" && !hostRx.MatchString(strings.ToLower(in.FakeTLS)) {
		return fmt.Errorf("домен FakeTLS указан неверно")
	}
	if in.Mode != "" && in.Mode != "socks5" && in.Mode != "mtproto" {
		return fmt.Errorf("режим: socks5 или mtproto")
	}
	mu.Lock()
	defer mu.Unlock()
	s := load()
	c := s[v.ID]
	if in.Port != 0 {
		c.Port = in.Port
	}
	if in.Mode != "" {
		c.Mode = in.Mode
	}
	if in.Secret != "" {
		c.Secret = strings.ToLower(in.Secret)
	}
	c.LAN, c.FakeTLS, c.CFDefault, c.User = in.LAN, strings.ToLower(in.FakeTLS), in.CFDefault, in.User
	if in.Pass != "" || in.User == "" {
		c.Pass = in.Pass
	}
	if in.AutoMin >= 0 {
		c.AutoMin = in.AutoMin
	}
	save(s)
	if installed(v) {
		return start(v, c)
	}
	return nil
}

// Boot starts enabled proxies when the service starts.
func Boot() {
	mu.Lock()
	defer mu.Unlock()
	s := load()
	for _, v := range Variants {
		if c := s[v.ID]; c.Enabled && installed(v) {
			if err := start(v, c); err != nil {
				app.Logf("tg %s: %v", v.ID, err)
			}
		}
	}
}

// Tick restarts proxies on their own interval (router: tg_auto_tick).
func Tick() {
	mu.Lock()
	defer mu.Unlock()
	s := load()
	changed := false
	for _, v := range Variants {
		c := s[v.ID]
		if !c.Enabled || c.AutoMin <= 0 || !installed(v) {
			continue
		}
		if time.Since(time.Unix(c.LastAutoAt, 0)) >= time.Duration(c.AutoMin)*time.Minute {
			c.LastAutoAt = time.Now().Unix()
			changed = true
			_ = start(v, c)
		}
	}
	if changed {
		save(s)
	}
}

func RestartAll() {
	mu.Lock()
	defer mu.Unlock()
	s := load()
	for _, v := range Variants {
		if c := s[v.ID]; c.Enabled && installed(v) {
			_ = start(v, c)
		}
	}
}

func StopAll() {
	for _, v := range Variants {
		osx.ProcStop(procName(v))
		osx.FirewallRemove(fwName(v))
	}
}
