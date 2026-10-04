// Package hosts manages %SystemRoot%\System32\drivers\etc\hosts — the router panel's /etc/hosts
// presets (Instagram, AI services, Telegram Web…), GeoHide replacement, reset and raw editing.
package hosts

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/zapretmanager/zmwin/internal/app"
	"github.com/zapretmanager/zmwin/internal/osx"
)

var Order = strings.Fields("nalog ntc instagram librusec ai twitch telegram spotify spotifyext rutor rutracker scell githubraw github tapeop finland")

var Titles = map[string]string{
	"nalog": "Налог (ЛК ФЛ, НПД)", "ntc": "ntc.party", "instagram": "Instagram и Facebook", "librusec": "lib.rus.ec",
	"ai": "ИИ-сервисы (ChatGPT, Gemini, Claude, Grok…)", "twitch": "Twitch", "telegram": "Telegram Web",
	"spotify": "Spotify", "spotifyext": "Spotify (расширенный)", "rutor": "rutor", "rutracker": "Rutracker",
	"scell": "Supercell (Brawl Stars, Clash…)", "githubraw": "githubusercontent.com", "github": "github.com",
	"tapeop": "tapeop.dev", "finland": "Discord Finland (голос)",
}

func Path() string {
	if runtime.GOOS == "windows" {
		root := os.Getenv("SystemRoot")
		if root == "" {
			root = `C:\Windows`
		}
		return filepath.Join(root, "System32", "drivers", "etc", "hosts")
	}
	return app.P("dev-hosts")
}

const defaultHosts = `# Copyright (c) 1993-2009 Microsoft Corp.
#
# This is a sample HOSTS file used by Microsoft TCP/IP for Windows.
#
# localhost name resolution is handled within DNS itself.
#	127.0.0.1       localhost
#	::1             localhost
`

// blockLines returns the preset expanded to one "IP host" entry per line (Windows ignores
// names beyond the ninth on a single hosts line).
func blockLines(id string) ([]string, error) {
	var src []string
	if id == "finland" {
		src = append(src, "#Discord Finland")
		for i := 10000; i <= 10199; i++ {
			src = append(src, fmt.Sprintf("104.25.158.178 finland%d.discord.media", i))
		}
	} else {
		b, ok := blocks[id]
		if !ok {
			return nil, fmt.Errorf("неизвестный блок")
		}
		src = b
	}
	var out []string
	for _, l := range src {
		if strings.HasPrefix(l, "#") {
			out = append(out, l)
			continue
		}
		f := strings.Fields(l)
		for _, h := range f[1:] {
			out = append(out, f[0]+" "+h)
		}
	}
	return out, nil
}

func norm(s string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(s, "\r", "")), " ")
}

func readLines() []string {
	return strings.Split(strings.ReplaceAll(app.ReadText(Path()), "\r", ""), "\n")
}

func write(lines []string) error {
	text := strings.Join(lines, "\r\n")
	if !strings.HasSuffix(text, "\r\n") {
		text += "\r\n"
	}
	p := Path()
	_ = app.CopyFile(p, app.P("state", "hosts.bak"))
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		return fmt.Errorf("не удалось записать hosts (%v) — возможно, файл заблокирован антивирусом", err)
	}
	osx.FlushDNS()
	return nil
}

func blockEnabled(id string, have map[string]bool) bool {
	ls, err := blockLines(id)
	if err != nil {
		return false
	}
	total, found := 0, 0
	seen := map[string]bool{}
	for _, l := range ls {
		k := norm(l)
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		total++
		if have[k] {
			found++
		}
	}
	return total > 0 && found == total
}

type Item struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Enabled bool   `json:"enabled"`
	Count   int    `json:"count"`
}

type StatusInfo struct {
	Path    string `json:"path"`
	GeoHide string `json:"geohide"`
	Items   []Item `json:"items"`
	Lines   int    `json:"lines"`
	Size    int64  `json:"size"`
}

func Status() StatusInfo {
	have := map[string]bool{}
	text := app.ReadText(Path())
	for _, l := range readLines() {
		have[norm(l)] = true
	}
	st := StatusInfo{Path: Path(), Lines: strings.Count(text, "\n")}
	if fi, err := os.Stat(Path()); err == nil {
		st.Size = fi.Size()
	}
	if strings.Contains(text, "### geohide.ru: hosts file") {
		switch {
		case strings.Contains(text, "# Регион серверов: US"):
			st.GeoHide = "us"
		case strings.Contains(text, "# Регион серверов: EU"):
			st.GeoHide = "eu"
		case strings.Contains(text, "# Регион серверов: RU"):
			st.GeoHide = "ru"
		default:
			st.GeoHide = "unknown"
		}
	}
	for _, id := range Order {
		ls, _ := blockLines(id)
		n := 0
		for _, l := range ls {
			if !strings.HasPrefix(l, "#") {
				n++
			}
		}
		st.Items = append(st.Items, Item{ID: id, Title: Titles[id], Enabled: blockEnabled(id, have), Count: n})
	}
	return st
}

// Toggle adds or removes a preset block (hosts_toggle).
func Toggle(id string) (bool, error) {
	ls, err := blockLines(id)
	if err != nil {
		return false, err
	}
	cur := readLines()
	have := map[string]bool{}
	for _, l := range cur {
		have[norm(l)] = true
	}
	if blockEnabled(id, have) {
		drop := map[string]bool{}
		for _, l := range ls {
			drop[norm(l)] = true
		}
		var out []string
		for _, l := range cur {
			if !drop[norm(l)] {
				out = append(out, l)
			}
		}
		return false, write(trimTail(out))
	}
	out := trimTail(cur)
	for _, l := range ls {
		if !have[norm(l)] {
			out = append(out, l)
			have[norm(l)] = true
		}
	}
	return true, write(out)
}

// AddBlocks enables several presets at once (full install adds the router's default set).
func AddBlocks(ids []string) error {
	cur := trimTail(readLines())
	have := map[string]bool{}
	for _, l := range cur {
		have[norm(l)] = true
	}
	for _, id := range ids {
		ls, err := blockLines(id)
		if err != nil {
			continue
		}
		for _, l := range ls {
			if !have[norm(l)] {
				cur = append(cur, l)
				have[norm(l)] = true
			}
		}
	}
	return write(cur)
}

func trimTail(ls []string) []string {
	for len(ls) > 0 && strings.TrimSpace(ls[len(ls)-1]) == "" {
		ls = ls[:len(ls)-1]
	}
	return ls
}

var geoURLs = map[string]string{
	"ru": "https://raw.githubusercontent.com/Internet-Helper/GeoHideDNS/refs/heads/main/hosts/hosts",
	"eu": "https://raw.githubusercontent.com/Internet-Helper/GeoHideDNS/refs/heads/main/hosts/eu/hosts",
	"us": "https://raw.githubusercontent.com/Internet-Helper/GeoHideDNS/refs/heads/main/hosts/us/hosts",
}

// ReplaceGeoHide replaces hosts entirely with the GeoHide DNS list for a region.
func ReplaceGeoHide(ctx context.Context, region string) error {
	u, ok := geoURLs[region]
	if !ok {
		return fmt.Errorf("неизвестный регион")
	}
	t, err := app.FetchText(ctx, u, 40*time.Second)
	if err != nil || strings.TrimSpace(t) == "" {
		return fmt.Errorf("не удалось скачать GeoHide hosts")
	}
	return write(strings.Split(t, "\n"))
}

// Reset returns the stock Windows hosts file.
func Reset() error { return write(strings.Split(defaultHosts, "\n")) }

const editMax = 4 << 20

func FileGet() (string, error) {
	b, err := os.ReadFile(Path())
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	if len(b) > editMax {
		return "", fmt.Errorf("файл слишком большой для редактора (%d КБ)", len(b)>>10)
	}
	return strings.ReplaceAll(string(b), "\r", ""), nil
}

func FileSet(content string) error {
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("пустой файл — сохранение отменено")
	}
	if len(content) > editMax {
		return fmt.Errorf("файл слишком большой")
	}
	return write(strings.Split(strings.ReplaceAll(content, "\r", ""), "\n"))
}

// HasExtra reports custom entries (DoH page warns that hosts overrides DNS).
func HasExtra() bool {
	for _, l := range readLines() {
		if i := strings.Index(l, "#"); i >= 0 {
			l = l[:i]
		}
		f := strings.Fields(l)
		if len(f) < 2 {
			continue
		}
		for _, h := range f[1:] {
			if h != "localhost" {
				return true
			}
		}
	}
	return false
}
