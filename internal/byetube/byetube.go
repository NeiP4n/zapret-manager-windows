// Package byetube is the ByeTube page: ByeDPI (ciadpi.exe) as a local SOCKS5 proxy and a PAC
// file that sends only YouTube domains through it. On the router the same is done with
// hev-socks5-tunnel + nftables; on Windows the per-user proxy auto-config does the steering.
package byetube

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zapretmanager/zmwin/internal/app"
	"github.com/zapretmanager/zmwin/internal/osx"
)

const (
	repo     = "hufrea/byedpi"
	procName = "byedpi"
)

type Preset struct {
	ID, Label, Name, Opts string
}

// Presets — router presets.js.
var Presets = []Preset{
	{"p1", "Стратегия 1", "Каскад disorder/split + tlsrec + md5sig, авто-режим -As (по умолчанию)", "-d1 -d3+s -s6+s -d9+s -s12+s -d15+s -s20+s -d25+s -s30+s -d35+s -r1+s -S -a1 -As -d1 -d3+s -s6+s -d9+s -s12+s -d15+s -s20+s -d25+s -s30+s -d35+s -S -a1"},
	{"p2", "Стратегия 2", "Короткая: OOB + tlsrec у SNI", "-o1 -a1 -r-5+se"},
	{"p3", "Стратегия 3", "Fake SNI google.com + disorder/OOB (TTL 4)", `-n "google.com" -Qr -d5+sm -f3+sm -o2 -t4 -a1`},
	{"p4", "Стратегия 4", "Каскад disorder/split без tlsrec и авто-режима", "-d1 -s1+s -d3+s -s6+s -d9+s -s12+s -d15+s -s20+s -d25+s -s30+s -d35+s -a1"},
	{"p5", "Стратегия 5", "Fake + disoob + tlsrec (TTL 5 и 15)", `-f1 -t5 -n "google.com" -q3+h -Qr -f2 -q1 -r1+s -t15 -q1 -o2 -a1`},
	{"p6", "Стратегия 6", "OOB + tlsrec + авто-режим -At,r,s, fake google.com", `-o1 -r-5+se -a1 -At,r,s -d1 -n "google.com" -Qr -f-1 -a1`},
}

type Config struct {
	Enabled    bool              `json:"enabled"`
	Port       int               `json:"port"`
	Opts       string            `json:"opts"`
	UseDefault bool              `json:"default_domains"`
	Custom     string            `json:"custom_domains"`
	PAC        bool              `json:"pac"`
	Version    string            `json:"version"`
	PrevPAC    map[string]string `json:"prev_pac,omitempty"`
}

var (
	kv     = app.NewKV("byetube")
	mu     sync.Mutex
	PACURL func() string // set by the web server
)

func load() Config {
	c := Config{Port: 1088, Opts: Presets[0].Opts, UseDefault: true, PAC: true}
	kv.Load(&c)
	if c.Port == 0 {
		c.Port = 1088
	}
	return c
}

func dir() string     { return filepath.Join(app.ToolsDir, "byedpi") }
func exe() string     { return filepath.Join(dir(), "ciadpi.exe") }
func Installed() bool { return app.Exists(exe()) || (app.Dev && load().Version != "") }

// SplitOpts splits a ByeDPI option string honouring double quotes.
func SplitOpts(s string) []string {
	var out []string
	var cur strings.Builder
	inq := false
	for _, r := range s {
		switch {
		case r == '"':
			inq = !inq
		case (r == ' ' || r == '\t') && !inq:
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// Domains returns the effective list routed through ByeDPI.
func Domains() []string {
	c := load()
	set := map[string]bool{}
	if c.UseDefault {
		for _, d := range defaultDomains {
			set[strings.ToLower(d)] = true
		}
	}
	for _, d := range strings.FieldsFunc(c.Custom, func(r rune) bool { return r == '\n' || r == ',' || r == ' ' || r == '\r' }) {
		d = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(d)), "*.")
		if i := strings.Index(d, "://"); i >= 0 {
			d = d[i+3:]
		}
		if d = strings.Trim(strings.SplitN(d, "/", 2)[0], "."); d != "" && !strings.HasPrefix(d, "#") {
			set[d] = true
		}
	}
	var out []string
	for d := range set {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

// PAC renders the proxy auto-config served at /byetube.pac.
func PAC() string {
	c := load()
	var b strings.Builder
	b.WriteString("// Zapret Manager — ByeTube\nvar D = {")
	for i, d := range Domains() {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, "%q:1", d)
	}
	fmt.Fprintf(&b, `};
function FindProxyForURL(url, host) {
  host = host.toLowerCase();
  if (!%v) return "DIRECT";
  for (var h = host; h; ) {
    if (D[h]) return "SOCKS5 127.0.0.1:%d; SOCKS 127.0.0.1:%d; DIRECT";
    var i = h.indexOf(".");
    if (i < 0) break;
    h = h.substring(i + 1);
  }
  return "DIRECT";
}
`, c.Enabled && osx.ProcRunning(procName), c.Port, c.Port)
	return b.String()
}

func start(c Config) error {
	if !c.Enabled || !Installed() {
		osx.ProcStop(procName)
		setPAC(&c, false)
		return nil
	}
	args := append([]string{"-i", "127.0.0.1", "-p", strconv.Itoa(c.Port)}, SplitOpts(c.Opts)...)
	if err := osx.ProcStart(osx.ProcSpec{Name: procName, Exe: exe(), Args: args, Dir: dir(), Restart: true}); err != nil {
		return err
	}
	setPAC(&c, c.PAC)
	return nil
}

// setPAC writes AutoConfigURL into every signed-in user's Internet Settings (the service runs as
// SYSTEM, so HKCU is not the user's hive). Chromium/Edge watch this key and apply it at once.
func setPAC(c *Config, on bool) {
	if PACURL == nil {
		return
	}
	url := PACURL()
	script := `$o=@{}; Get-ChildItem Registry::HKEY_USERS | ?{ $_.PSChildName -match '^S-1-5-21-[\d-]+$' } | %{ ` +
		`$k='Registry::HKEY_USERS\'+$_.PSChildName+'\Software\Microsoft\Windows\CurrentVersion\Internet Settings'; ` +
		`if(Test-Path $k){ $v=(Get-ItemProperty $k -EA SilentlyContinue).AutoConfigURL; $o[$_.PSChildName]=[string]$v; `
	if on {
		script += `if($v -ne ` + osx.PSQuote(url) + `){ Set-ItemProperty $k -Name AutoConfigURL -Value ` + osx.PSQuote(url) + ` } } }; ConvertTo-Json $o -Compress`
		out, _ := osx.PS(nil, 30*time.Second, script)
		if c.PrevPAC == nil {
			c.PrevPAC = map[string]string{}
			_ = jsonInto(out, &c.PrevPAC)
			for k, v := range c.PrevPAC {
				if v == url {
					c.PrevPAC[k] = ""
				}
			}
			_ = kv.Save(c)
		}
		return
	}
	if c.PrevPAC == nil {
		return
	}
	var rs []string
	for sid, v := range c.PrevPAC {
		k := `Registry::HKEY_USERS\` + sid + `\Software\Microsoft\Windows\CurrentVersion\Internet Settings`
		if v == "" {
			rs = append(rs, fmt.Sprintf("if((Get-ItemProperty %s -EA SilentlyContinue).AutoConfigURL -eq %s){Remove-ItemProperty %s -Name AutoConfigURL -EA SilentlyContinue}", osx.PSQuote(k), osx.PSQuote(url), osx.PSQuote(k)))
		} else {
			rs = append(rs, fmt.Sprintf("Set-ItemProperty %s -Name AutoConfigURL -Value %s -EA SilentlyContinue", osx.PSQuote(k), osx.PSQuote(v)))
		}
	}
	_, _ = osx.PS(nil, 30*time.Second, strings.Join(rs, "; "))
	c.PrevPAC = nil
	_ = kv.Save(c)
}

type StatusInfo struct {
	Installed bool          `json:"installed"`
	Config    Config        `json:"config"`
	Running   bool          `json:"running"`
	Proc      osx.ProcState `json:"proc"`
	Presets   []Preset      `json:"presets"`
	Domains   int           `json:"domains"`
	PACURL    string        `json:"pac_url"`
	Default   []string      `json:"default_list"`
}

func Status() StatusInfo {
	c := load()
	st := StatusInfo{Installed: Installed(), Config: c, Proc: osx.ProcStatus(procName), Presets: Presets,
		Domains: len(Domains()), Default: defaultDomains}
	st.Config.PrevPAC = nil
	st.Running = st.Proc.Running
	if PACURL != nil {
		st.PACURL = PACURL()
	}
	return st
}

func Install(j *app.Job) error {
	j.Say("==> Узнаём последнюю версию ByeDPI")
	tag := app.LatestTag(j.Ctx(), repo)
	if tag == "" {
		tag = "v0.17.3"
	}
	short := strings.TrimPrefix(strings.TrimPrefix(tag, "v"), "0.")
	asset := fmt.Sprintf("byedpi-%s-x86_64-w64.zip", short)
	url := fmt.Sprintf("%s/%s/releases/download/%s/%s", app.GHMain, repo, tag, asset)
	tmp := filepath.Join(app.TmpDir, asset)
	j.Say("==> Скачиваем %s", asset)
	if err := app.Download(j.Ctx(), url, tmp, 4, j); err != nil {
		return err
	}
	defer os.Remove(tmp)
	osx.ProcStop(procName)
	if _, err := app.Unzip(tmp, dir(), func(n string) string {
		if strings.EqualFold(filepath.Base(n), "ciadpi.exe") {
			return "ciadpi.exe"
		}
		return ""
	}); err != nil {
		return err
	}
	mu.Lock()
	c := load()
	c.Version, c.Enabled = tag, true
	_ = kv.Save(&c)
	err := start(c)
	mu.Unlock()
	if err != nil {
		return err
	}
	j.Say("==> Готово, ByeDPI %s запущен на 127.0.0.1:%d, YouTube идёт через него", tag, c.Port)
	return nil
}

func Remove(j *app.Job) error {
	mu.Lock()
	defer mu.Unlock()
	c := load()
	c.Enabled = false
	osx.ProcStop(procName)
	setPAC(&c, false)
	app.Remove(dir())
	_ = kv.Save(&Config{})
	j.Say("==> ByeTube удалён, системный прокси возвращён")
	return nil
}

func SetEnabled(on bool) error {
	mu.Lock()
	defer mu.Unlock()
	c := load()
	c.Enabled = on
	_ = kv.Save(&c)
	return start(c)
}

// Configure saves options (opts / domains / pac / port) and restarts.
func Configure(in Config) error {
	if in.Port != 0 && (in.Port < 1024 || in.Port > 65535) {
		return fmt.Errorf("порт: 1024–65535")
	}
	if strings.ContainsAny(in.Opts, "&|;<>`$") {
		return fmt.Errorf("недопустимые символы в параметрах ByeDPI")
	}
	mu.Lock()
	defer mu.Unlock()
	c := load()
	if in.Port != 0 {
		c.Port = in.Port
	}
	if strings.TrimSpace(in.Opts) != "" {
		c.Opts = strings.Join(strings.Fields(in.Opts), " ")
	}
	c.UseDefault, c.Custom = in.UseDefault, in.Custom
	if c.PAC && !in.PAC {
		setPAC(&c, false)
	}
	c.PAC = in.PAC
	_ = kv.Save(&c)
	return start(c)
}

func Boot() {
	mu.Lock()
	defer mu.Unlock()
	if c := load(); c.Enabled && Installed() {
		if err := start(c); err != nil {
			app.Logf("byetube: %v", err)
		}
	}
}

func Shutdown() {
	mu.Lock()
	defer mu.Unlock()
	c := load()
	if c.PAC {
		setPAC(&c, false)
	}
}

// ---------------- strategy test (router: bytetube test) ----------------

type TestRow struct {
	Opts  string   `json:"opts"`
	OK    int      `json:"ok"`
	Total int      `json:"total"`
	Fails []string `json:"fails"`
}

var resultsKV = app.NewKV("byetube_test")

func Results() []TestRow {
	var r []TestRow
	resultsKV.Load(&r)
	return r
}

func checkVia(ctx context.Context, proxy, host string) bool {
	tr := &http.Transport{
		DialContext:     func(ctx context.Context, _, addr string) (net.Conn, error) { return DialSOCKS5(ctx, proxy, addr) },
		TLSClientConfig: &tls.Config{}, DisableKeepAlives: true, ForceAttemptHTTP2: true,
	}
	defer tr.CloseIdleConnections()
	cl := &http.Client{Transport: tr, Timeout: 7 * time.Second}
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://"+host+"/", nil)
	req.Header.Set("Range", "bytes=0-65535")
	resp, err := cl.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, err = io.Copy(io.Discard, io.LimitReader(resp.Body, 65536))
	return err == nil
}

// RunTest starts each candidate in a temporary ciadpi on its own port (the main instance is not
// touched) and checks the YouTube test domains through it.
func RunTest(j *app.Job, extra []string) error {
	if !app.Exists(exe()) && !app.Dev {
		return fmt.Errorf("ByeDPI не установлен")
	}
	cands := []string{}
	for _, p := range Presets {
		cands = append(cands, p.Opts)
	}
	cands = append(cands, testStrategies...)
	cands = append(cands, extra...)
	seen := map[string]bool{}
	var rows []TestRow
	base := 21080
	for i, opts := range cands {
		if seen[opts] || j.Stopped() {
			continue
		}
		seen[opts] = true
		port := base + i%50
		name := "byedpi-test"
		j.Say("==> [%d/%d] %s", i+1, len(cands), opts)
		if err := osx.ProcStart(osx.ProcSpec{Name: name, Exe: exe(), Args: append([]string{"-i", "127.0.0.1", "-p", strconv.Itoa(port)}, SplitOpts(opts)...), Dir: dir()}); err != nil {
			j.Say("   !! %v", err)
			continue
		}
		time.Sleep(400 * time.Millisecond)
		row := TestRow{Opts: opts, Total: len(testDomains)}
		var wg sync.WaitGroup
		var rmu sync.Mutex
		sem := make(chan struct{}, 8)
		for _, d := range testDomains {
			wg.Add(1)
			sem <- struct{}{}
			go func(d string) {
				defer wg.Done()
				defer func() { <-sem }()
				ok := checkVia(j.Ctx(), "127.0.0.1:"+strconv.Itoa(port), d)
				rmu.Lock()
				if ok {
					row.OK++
				} else {
					row.Fails = append(row.Fails, d)
				}
				rmu.Unlock()
			}(d)
		}
		wg.Wait()
		osx.ProcStop(name)
		j.Say("   результат: %d из %d", row.OK, row.Total)
		rows = append(rows, row)
		sort.SliceStable(rows, func(a, b int) bool { return rows[a].OK > rows[b].OK })
		_ = resultsKV.Save(rows)
	}
	if len(rows) > 0 {
		j.Say("==> Лучшая: %s → %d/%d", rows[0].Opts, rows[0].OK, rows[0].Total)
	}
	return nil
}

func jsonInto(s string, v any) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return jsonUnmarshal([]byte(s), v)
}
