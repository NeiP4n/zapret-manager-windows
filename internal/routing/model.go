// Package routing is the selective-VPN engine behind the Steer, Forkozz and Mixomo pages.
// The router uses sing-box/mihomo + nftables; on Windows a single mihomo instance in TUN mode
// does the steering: Forkozz sections and Steer WARP tunnels are compiled into one generated
// config, Mixomo replaces it with the user's own config.
package routing

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/zapretmanager/zmwin/internal/app"
)

const itdRaw = "https://raw.githubusercontent.com/itdoginfo/allow-domains/main"

// Service is one itdoginfo category (router SERVICES_BASE).
type Service struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Domains string `json:"-"`
	Subnets string `json:"-"`
}

var Services = []Service{
	{"russia_inside", "Россия: заблокированные внутри", itdRaw + "/Russia/inside-raw.lst", ""},
	{"russia_outside", "Россия: недоступные снаружи", itdRaw + "/Russia/outside-raw.lst", ""},
	{"ukraine_inside", "Украина: заблокированные", itdRaw + "/Ukraine/inside-raw.lst", ""},
	{"geoblock", "Геоблок (сервисы, закрывшие РФ)", itdRaw + "/Categories/geoblock.lst", ""},
	{"block", "Заблокированные (РКН)", itdRaw + "/Categories/block.lst", ""},
	{"porn", "18+", itdRaw + "/Categories/porn.lst", ""},
	{"news", "Новости", itdRaw + "/Categories/news.lst", ""},
	{"anime", "Аниме", itdRaw + "/Categories/anime.lst", ""},
	{"hodca", "HODCA", itdRaw + "/Categories/hodca.lst", ""},
	{"youtube", "YouTube", itdRaw + "/Services/youtube.lst", ""},
	{"hdrezka", "HDrezka", itdRaw + "/Services/hdrezka.lst", ""},
	{"tiktok", "TikTok", itdRaw + "/Services/tiktok.lst", ""},
	{"google_ai", "Google AI (Gemini)", itdRaw + "/Services/google_ai.lst", ""},
	{"google_play", "Google Play", itdRaw + "/Services/google_play.lst", ""},
	{"google_meet", "Google Meet", itdRaw + "/Services/google_meet.lst", itdRaw + "/Subnets/IPv4/google_meet.lst"},
	{"discord", "Discord", itdRaw + "/Services/discord.lst", itdRaw + "/Subnets/IPv4/discord.lst"},
	{"meta", "Meta (Instagram, Facebook, WhatsApp)", itdRaw + "/Services/meta.lst", itdRaw + "/Subnets/IPv4/meta.lst"},
	{"twitter", "X / Twitter", itdRaw + "/Services/twitter.lst", itdRaw + "/Subnets/IPv4/twitter.lst"},
	{"telegram", "Telegram", itdRaw + "/Services/telegram.lst", itdRaw + "/Subnets/IPv4/telegram.lst"},
	{"roblox", "Roblox", itdRaw + "/Services/roblox.lst", itdRaw + "/Subnets/IPv4/roblox.lst"},
	{"cloudflare", "Cloudflare", itdRaw + "/Services/cloudflare.lst", itdRaw + "/Subnets/IPv4/cloudflare.lst"},
	{"cloudfront", "CloudFront", itdRaw + "/Services/cloudfront.lst", itdRaw + "/Subnets/IPv4/cloudfront.lst"},
	{"digitalocean", "DigitalOcean", itdRaw + "/Services/digitalocean.lst", itdRaw + "/Subnets/IPv4/digitalocean.lst"},
	{"hetzner", "Hetzner", itdRaw + "/Services/hetzner.lst", itdRaw + "/Subnets/IPv4/hetzner.lst"},
	{"ovh", "OVH", itdRaw + "/Services/ovh.lst", itdRaw + "/Subnets/IPv4/ovh.lst"},
}

func serviceByID(id string) (Service, bool) {
	for _, s := range Services {
		if s.ID == id {
			return s, true
		}
	}
	return Service{}, false
}

// Section is a Forkozz routing section: what goes where.
type Section struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Enabled  bool     `json:"enabled"`
	Mode     string   `json:"mode"` // links | subscription | warp | interface
	Links    string   `json:"links"`
	SubURLs  string   `json:"sub_urls"`
	SubIv    string   `json:"sub_interval"`
	Filter   string   `json:"filter"`
	ExFilter string   `json:"exclude_filter"`
	Pinned   string   `json:"pinned"` // "" = auto (url-test)
	Iface    string   `json:"iface"`
	Services []string `json:"services"`
	Domains  string   `json:"domains"`
	Subnets  string   `json:"subnets"`
	ListURLs string   `json:"list_urls"`
}

// Steer: automatic free WARP tunnels for chosen services.
type Steer struct {
	Enabled  bool     `json:"enabled"`
	Tunnels  int      `json:"tunnels"`
	Services []string `json:"services"`
	Domains  string   `json:"domains"`
	Endpoint string   `json:"endpoint"`
	I1       string   `json:"i1"`
}

// Mixomo: the user's own mihomo config (or one generated from a subscription).
type Mixomo struct {
	Enabled     bool   `json:"enabled"`
	SubURL      string `json:"sub_url"`
	Filter      string `json:"filter"`
	ListPreset  string `json:"list_preset"`
	UI          bool   `json:"ui"`
	AutoRestart string `json:"auto_restart"` // "" | HH:MM
	WarpProxy   bool   `json:"warp_proxy"`
	LastRestart string `json:"last_restart"`
}

type Config struct {
	Sections  []Section `json:"sections"`
	Steer     Steer     `json:"steer"`
	Mixomo    Mixomo    `json:"mixomo"`
	Exclude   string    `json:"exclude"`
	DNSMode   string    `json:"dns_mode"` // fake-ip | redir-host
	ListsIv   string    `json:"lists_interval"`
	Secret    string    `json:"secret"`
	CtrlPort  int       `json:"controller_port"`
	Version   string    `json:"version"`
	ListsAt   int64     `json:"lists_at"`
	BlockQUIC bool      `json:"block_quic"`
}

var (
	kv = app.NewKV("routing")
	mu sync.Mutex
)

func Load() Config {
	c := Config{DNSMode: "fake-ip", ListsIv: "1d", CtrlPort: 9097, Steer: Steer{Tunnels: 2, I1: "quic"}}
	kv.Load(&c)
	if c.CtrlPort == 0 {
		c.CtrlPort = 9097
	}
	if c.Steer.Tunnels == 0 {
		c.Steer.Tunnels = 2
	}
	return c
}

func save(c Config) error { return kv.Save(&c) }

var (
	secIDRx  = regexp.MustCompile(`^s[0-9]{1,3}$`)
	schemeRx = regexp.MustCompile(`(?i)^(vless|vmess|trojan|ss|socks4|socks4a|socks5|hysteria2|hy2|tuic|wireguard|anytls)://\S+$`)
	domRx    = regexp.MustCompile(`^[a-z0-9*]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)
	cidrRx   = regexp.MustCompile(`^[0-9a-fA-F:.]+(/[0-9]{1,3})?$`)
)

func splitList(s string) []string {
	var out []string
	for _, l := range strings.FieldsFunc(s, func(r rune) bool { return r == '\n' || r == '\r' || r == ',' || r == ' ' || r == '\t' }) {
		l = strings.TrimSpace(l)
		if l != "" && !strings.HasPrefix(l, "#") {
			out = append(out, l)
		}
	}
	return out
}

func normDomain(d string) string {
	d = strings.ToLower(strings.TrimSpace(d))
	if i := strings.Index(d, "://"); i >= 0 {
		d = d[i+3:]
	}
	d = strings.SplitN(d, "/", 2)[0]
	return strings.Trim(strings.TrimPrefix(d, "*."), ".")
}

// Validate checks a section the way netshift.uc cmd_set does.
func (s *Section) Validate() error {
	if !secIDRx.MatchString(s.ID) {
		return fmt.Errorf("неверный идентификатор секции")
	}
	if strings.TrimSpace(s.Name) == "" {
		s.Name = s.ID
	}
	switch s.Mode {
	case "links":
		links := splitLines(s.Links)
		if len(links) == 0 {
			return fmt.Errorf("«%s»: добавьте хотя бы одну ссылку vless:// / vmess:// / trojan:// / ss:// / hy2://", s.Name)
		}
		for _, l := range links {
			if !schemeRx.MatchString(l) {
				return fmt.Errorf("«%s»: не похоже на ссылку прокси: %.60s", s.Name, l)
			}
		}
	case "subscription":
		urls := splitList(s.SubURLs)
		if len(urls) == 0 || len(urls) > 10 {
			return fmt.Errorf("«%s»: укажите от 1 до 10 ссылок на подписку", s.Name)
		}
		for _, u := range urls {
			if !regexp.MustCompile(`^https?://[^ \t/]+`).MatchString(u) {
				return fmt.Errorf("«%s»: ссылка на подписку должна начинаться с http(s)://", s.Name)
			}
		}
	case "warp":
	case "interface":
		if strings.TrimSpace(s.Iface) == "" {
			return fmt.Errorf("«%s»: выберите сетевой интерфейс (например, туннель AmneziaWG)", s.Name)
		}
	default:
		return fmt.Errorf("«%s»: неизвестный способ подключения", s.Name)
	}
	for _, d := range splitList(s.Domains) {
		if !domRx.MatchString(normDomain(d)) {
			return fmt.Errorf("«%s»: не похоже на домен: %s", s.Name, d)
		}
	}
	for _, n := range splitList(s.Subnets) {
		if !cidrRx.MatchString(n) {
			return fmt.Errorf("«%s»: адрес или подсеть, например 203.0.113.0/24: %s", s.Name, n)
		}
	}
	for _, id := range s.Services {
		if _, ok := serviceByID(id); !ok {
			return fmt.Errorf("неизвестный сервис %s", id)
		}
	}
	return nil
}

func splitLines(s string) []string {
	var out []string
	for _, l := range strings.Split(strings.ReplaceAll(s, "\r", ""), "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") {
			out = append(out, l)
		}
	}
	return out
}

// usedServices is the union of services referenced by enabled sections and Steer.
func (c Config) usedServices() []string {
	set := map[string]bool{}
	for _, s := range c.Sections {
		if s.Enabled {
			for _, id := range s.Services {
				set[id] = true
			}
		}
	}
	if c.Steer.Enabled {
		for _, id := range c.Steer.Services {
			set[id] = true
		}
	}
	var out []string
	for id := range set {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func newSectionID(c Config) string {
	for i := 1; ; i++ {
		id := fmt.Sprintf("s%d", i)
		free := true
		for _, s := range c.Sections {
			if s.ID == id {
				free = false
			}
		}
		if free {
			return id
		}
	}
}

func jsonPretty(v any) string {
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b) + "\n"
}
