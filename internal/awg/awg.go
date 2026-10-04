// Package awg is the "AmneziaWG" page: the AmneziaWG for Windows client, tunnel services, and
// free Cloudflare WARP keys with AmneziaWG obfuscation (shared WARP.conf with Mixomo / Steer).
package awg

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/zapretmanager/zmwin/internal/app"
	"github.com/zapretmanager/zmwin/internal/osx"
)

const (
	clientRepo = "amnezia-vpn/amneziawg-windows-client"
	WarpName   = "warp"
)

var (
	apis = []string{
		"https://api.cloudflareclient.com/v0a4005/reg",
		"https://api.cloudflareclient.com/v0i1909051800/reg",
		"https://api.cloudflareclient.com/v0a2158/reg",
		"https://api.cloudflareclient.com/v0a1922/reg",
		"https://edge-client-api.vercel.app/v0a4005/reg",
	}
	cfVer     = "a-6.11-2223"
	Endpoints = []string{"engage.cloudflareclient.com:4500", "engage.cloudflareclient.com:2408", "162.159.192.1:2408",
		"162.159.193.1:2408", "162.159.195.1:2408", "188.114.97.1:2408", "188.114.98.1:500", "188.114.99.1:4500"}
	I1Kinds  = []string{"quic", "quic2", "dns", "stun", "icloud", "sip", "none"}
	nameRx   = regexp.MustCompile(`^[A-Za-z0-9_=+.-]{1,32}$`)
	i1Hosts  = []string{"www.apple.com", "www.google.com", "www.microsoft.com", "cdn.jsdelivr.net"}
	progDirs = []string{`C:\Program Files\AmneziaWG`, `C:\Program Files (x86)\AmneziaWG`}
)

func Dir() string { return app.P("awg") }

func confPath(name string) string { return filepath.Join(Dir(), name+".conf") }

// WarpConf is the shared WARP configuration path (router: /root/WARP.conf).
func WarpConf() string { return confPath(WarpName) }

func clientExe() string {
	for _, d := range progDirs {
		if p := filepath.Join(d, "amneziawg.exe"); app.Exists(p) {
			return p
		}
	}
	return ""
}

func awgExe() string {
	for _, d := range progDirs {
		if p := filepath.Join(d, "awg.exe"); app.Exists(p) {
			return p
		}
	}
	return ""
}

func ClientInstalled() bool { return clientExe() != "" || app.Dev }

func serviceName(name string) string { return "AmneziaWGTunnel$" + name }

// ---------------- client install ----------------

func InstallClient(j *app.Job) error {
	if runtime.GOOS != "windows" && !app.Dev {
		return fmt.Errorf("только для Windows")
	}
	j.Say("==> Узнаём последнюю версию AmneziaWG для Windows")
	tag := app.LatestTag(j.Ctx(), clientRepo)
	if tag == "" {
		return fmt.Errorf("не удалось узнать версию — проверьте доступ к GitHub")
	}
	ver := strings.TrimPrefix(tag, "v")
	msi := fmt.Sprintf("amneziawg-amd64-%s.msi", ver)
	url := fmt.Sprintf("%s/%s/releases/download/%s/%s", app.GHMain, clientRepo, tag, msi)
	dst := filepath.Join(app.TmpDir, msi)
	j.Say("==> Скачиваем %s", msi)
	if err := app.Download(j.Ctx(), url, dst, 4, j); err != nil {
		return err
	}
	defer os.Remove(dst)
	j.Say("==> Устанавливаем (msiexec, без окон)")
	if out, err := osx.Run(j.Ctx(), 5*time.Minute, "msiexec", "/i", dst, "/qn", "/norestart"); err != nil {
		return fmt.Errorf("msiexec: %v %s", err, out)
	}
	if !ClientInstalled() {
		return fmt.Errorf("установщик завершился, но amneziawg.exe не найден")
	}
	j.Say("==> Готово, AmneziaWG %s установлен", ver)
	return nil
}

func RemoveClient(j *app.Job) error {
	for _, t := range Tunnels() {
		j.Say("==> Останавливаем туннель %s", t.Name)
		_ = Stop(t.Name)
	}
	j.Say("==> Удаляем AmneziaWG")
	out, err := osx.PS(j.Ctx(), 5*time.Minute, `$p=Get-ItemProperty 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\*','HKLM:\SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall\*' -EA SilentlyContinue | ?{$_.DisplayName -like 'AmneziaWG*'}; foreach($x in $p){ Start-Process msiexec.exe -ArgumentList '/x',$x.PSChildName,'/qn','/norestart' -Wait }`)
	if err != nil {
		return fmt.Errorf("%v %s", err, out)
	}
	j.Say("==> Готово (сохранённые конфиги остались в %s)", Dir())
	return nil
}

// ---------------- tunnels ----------------

type Tunnel struct {
	Name      string `json:"name"`
	Running   bool   `json:"running"`
	Address   string `json:"address"`
	Endpoint  string `json:"endpoint"`
	Allowed   string `json:"allowed"`
	FullRoute bool   `json:"full_route"`
	Handshake int64  `json:"handshake"`
	Obfs      bool   `json:"obfs"`
	Warp      bool   `json:"warp"`
}

func field(conf, key string) string {
	rx := regexp.MustCompile(`(?mi)^\s*` + regexp.QuoteMeta(key) + `\s*=\s*(.+?)\s*$`)
	if m := rx.FindStringSubmatch(conf); m != nil {
		return m[1]
	}
	return ""
}

func Tunnels() []Tunnel {
	files, _ := filepath.Glob(filepath.Join(Dir(), "*.conf"))
	sort.Strings(files)
	var out []Tunnel
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".conf")
		c := app.ReadText(f)
		t := Tunnel{Name: name, Address: field(c, "Address"), Endpoint: field(c, "Endpoint"), Allowed: field(c, "AllowedIPs"),
			Obfs: field(c, "Jc") != "", Warp: name == WarpName}
		t.FullRoute = strings.Contains(t.Allowed, "0.0.0.0/0")
		t.Running = osx.ServiceState(serviceName(name)) == "running" || (app.Dev && devUp[name])
		if t.Running {
			t.Handshake = handshake(name)
		}
		out = append(out, t)
	}
	return out
}

var devUp = map[string]bool{}

func handshake(name string) int64 {
	exe := awgExe()
	if exe == "" {
		return 0
	}
	out, err := osx.Run(nil, 10*time.Second, exe, "show", name, "latest-handshakes")
	if err != nil {
		return 0
	}
	f := strings.Fields(out)
	if len(f) >= 2 {
		n, _ := strconv.ParseInt(f[len(f)-1], 10, 64)
		return n
	}
	return 0
}

func GetConf(name string) (string, error) {
	if !nameRx.MatchString(name) {
		return "", fmt.Errorf("недопустимое имя туннеля")
	}
	return app.ReadText(confPath(name)), nil
}

// SaveConf validates and stores a tunnel; a running tunnel is restarted with the new config.
func SaveConf(name, conf string) error {
	if !nameRx.MatchString(name) {
		return fmt.Errorf("имя туннеля: латиница, цифры, _ . - (до 32 символов)")
	}
	conf = strings.ReplaceAll(conf, "\r", "")
	if !strings.Contains(conf, "[Interface]") || !strings.Contains(conf, "[Peer]") || field(conf, "PrivateKey") == "" || field(conf, "PublicKey") == "" {
		return fmt.Errorf("вставьте конфигурацию с секциями [Interface] (PrivateKey, Address) и [Peer] (PublicKey, Endpoint)")
	}
	_ = os.MkdirAll(Dir(), 0o700)
	if err := os.WriteFile(confPath(name), []byte(strings.ReplaceAll(strings.TrimSpace(conf), "\n", "\r\n")+"\r\n"), 0o600); err != nil {
		return err
	}
	if osx.ServiceState(serviceName(name)) == "running" {
		_ = Stop(name)
		return Start(name)
	}
	return nil
}

func Start(name string) error {
	if !nameRx.MatchString(name) || !app.Exists(confPath(name)) {
		return fmt.Errorf("туннель не найден")
	}
	if app.Dev {
		devUp[name] = true
		return nil
	}
	exe := clientExe()
	if exe == "" {
		return fmt.Errorf("AmneziaWG не установлен — сначала установите клиент")
	}
	if osx.ServiceState(serviceName(name)) != "" {
		_, _ = osx.Run(nil, 30*time.Second, exe, "/uninstalltunnelservice", name)
		time.Sleep(time.Second)
	}
	out, err := osx.Run(nil, 60*time.Second, exe, "/installtunnelservice", confPath(name))
	if err != nil {
		return fmt.Errorf("не удалось поднять туннель: %v %s", err, out)
	}
	return nil
}

func Stop(name string) error {
	if app.Dev {
		devUp[name] = false
		return nil
	}
	exe := clientExe()
	if exe == "" {
		return nil
	}
	_, err := osx.Run(nil, 60*time.Second, exe, "/uninstalltunnelservice", name)
	return err
}

func Delete(name string) error {
	if !nameRx.MatchString(name) {
		return fmt.Errorf("недопустимое имя")
	}
	_ = Stop(name)
	app.Remove(confPath(name))
	return nil
}

// SetFullRoute switches a tunnel between "весь интернет в туннель" and only its own subnet.
func SetFullRoute(name string, on bool) error {
	c, err := GetConf(name)
	if err != nil || c == "" {
		return fmt.Errorf("туннель не найден")
	}
	allowed := "0.0.0.0/0, ::/0"
	if !on {
		addr := strings.Split(field(c, "Address"), ",")[0]
		ip := strings.TrimSpace(strings.Split(addr, "/")[0])
		allowed = ip + "/32"
		if pi := net.ParseIP(ip); pi != nil && pi.To4() != nil {
			allowed = pi.Mask(net.CIDRMask(24, 32)).String() + "/24"
		}
	}
	rx := regexp.MustCompile(`(?mi)^\s*AllowedIPs\s*=.*$`)
	return SaveConf(name, rx.ReplaceAllString(c, "AllowedIPs = "+allowed))
}

// ---------------- WARP ----------------

type Keys struct {
	Priv, Pub, Peer, V4, V6 string
}

func genKey() (priv, pub string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return
	}
	b[0] &= 248
	b[31] &= 127
	b[31] |= 64
	k, err := ecdh.X25519().NewPrivateKey(b)
	if err != nil {
		return
	}
	return base64.StdEncoding.EncodeToString(b), base64.StdEncoding.EncodeToString(k.PublicKey().Bytes()), nil
}

// Register gets a free WARP account from Cloudflare (or the community relay) — _awg_cf_register.
func Register(ctx context.Context, say func(string, ...any)) (*Keys, error) {
	priv, pub, err := genKey()
	if err != nil {
		return nil, err
	}
	dead := map[string]bool{}
	cl := &http.Client{Timeout: 25 * time.Second}
	for _, api := range apis {
		host := strings.SplitN(strings.TrimPrefix(api, "https://"), "/", 2)[0]
		if dead[host] {
			continue
		}
		body := fmt.Sprintf(`{"key":"%s","install_id":"","fcm_token":"","tos":"%s","type":"Android","model":"","locale":"en_US"}`,
			pub, time.Now().UTC().Format("2006-01-02T15:04:05.000Z"))
		req, _ := http.NewRequestWithContext(ctx, "POST", api, bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "okhttp/3.12.1")
		req.Header.Set("CF-Client-Version", cfVer)
		resp, err := cl.Do(req)
		if err != nil {
			say("   %s: нет связи", host)
			dead[host] = true
			continue
		}
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if resp.StatusCode != 200 && resp.StatusCode != 201 {
			say("   %s: ответ %d", host, resp.StatusCode)
			continue
		}
		var r struct {
			ID     string `json:"id"`
			Token  string `json:"token"`
			Config struct {
				Peers []struct {
					PublicKey string `json:"public_key"`
				} `json:"peers"`
				Interface struct {
					Addresses struct{ V4, V6 string } `json:"addresses"`
				} `json:"interface"`
			} `json:"config"`
			Result *json.RawMessage `json:"result"`
		}
		if err := json.Unmarshal(data, &r); err != nil {
			continue
		}
		if r.Result != nil {
			_ = json.Unmarshal(*r.Result, &r)
		}
		if len(r.Config.Peers) == 0 || r.Config.Interface.Addresses.V4 == "" {
			say("   %s: в ответе нет ключа сервера", host)
			continue
		}
		if r.ID != "" && r.Token != "" {
			req, _ := http.NewRequestWithContext(ctx, "PATCH", api+"/"+r.ID, bytes.NewReader([]byte(`{"warp_enabled":true}`)))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("User-Agent", "okhttp/3.12.1")
			req.Header.Set("CF-Client-Version", cfVer)
			req.Header.Set("Authorization", "Bearer "+r.Token)
			if resp, err := cl.Do(req); err == nil {
				resp.Body.Close()
				if resp.StatusCode/100 != 2 {
					say("   %s: ключ выдан, но WARP не включился (%d)", host, resp.StatusCode)
					dead[host] = true
					continue
				}
			}
		}
		say("   ключи выданы (%s)", host)
		return &Keys{Priv: priv, Pub: pub, Peer: r.Config.Peers[0].PublicKey, V4: r.Config.Interface.Addresses.V4, V6: r.Config.Interface.Addresses.V6}, nil
	}
	return nil, fmt.Errorf("ключи WARP не получены — ни один источник не ответил")
}

func rnd(a, b int) int {
	n, _ := rand.Int(rand.Reader, big.NewInt(int64(b-a+1)))
	return a + int(n.Int64())
}

func rstr(n int) string {
	const al = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = al[rnd(0, len(al)-1)]
	}
	return string(b)
}

// I1 builds the AmneziaWG 1.5 "special junk" signature packet (_awg_i1).
func I1(kind string) string {
	host := i1Hosts[rnd(0, len(i1Hosts)-1)]
	switch kind {
	case "quic":
		return i1Quic
	case "quic2":
		return i1Quic2
	case "icloud":
		return i1ICloud
	case "dns":
		q := ""
		for _, l := range strings.Split(host, ".") {
			q += fmt.Sprintf("%02x", len(l)) + hex.EncodeToString([]byte(l))
		}
		p := rnd(60, 180)
		return fmt.Sprintf("<r 2><b 0x01000001000000000001%s0000010001000029100000000000%04x000c%04x><r %d>", q, p+4, p, p)
	case "stun":
		s := rnd(4, 8) * 4
		return fmt.Sprintf("<b 0x0001%04x2112a442><r 12><b 0x8022%04x%s>", s+4, s, hex.EncodeToString([]byte(rstr(s))))
	case "sip":
		msg := strings.Join([]string{
			"OPTIONS sip:" + host + " SIP/2.0",
			fmt.Sprintf("Via: SIP/2.0/UDP 192.168.%d.%d:5060;branch=z9hG4bK%s", rnd(0, 255), rnd(2, 254), rstr(10)),
			fmt.Sprintf("From: <sip:%s@%s>;tag=%d", rstr(8), host, rnd(100000, 999999)),
			"To: <sip:" + host + ">", "Call-ID: " + rstr(16) + "@" + host, fmt.Sprintf("CSeq: %d OPTIONS", rnd(1000, 9999)),
			"Max-Forwards: 70", "User-Agent: PJSIP/2.13", "Content-Length: 0", "", ""}, "\r\n")
		return "<b 0x" + hex.EncodeToString([]byte(msg)) + ">"
	}
	return ""
}

// WarpConfText renders a WARP config with the router's obfuscation defaults.
func WarpConfText(k *Keys, endpoint, i1 string, full bool) string {
	addr := k.V4
	if k.V6 != "" {
		addr += ", " + k.V6
	}
	allowed := "0.0.0.0/0, ::/0"
	if !full {
		allowed = "162.159.0.0/16, 188.114.96.0/22"
	}
	lines := []string{"[Interface]", "PrivateKey = " + k.Priv, "Address = " + addr, "DNS = 1.1.1.1, 1.0.0.1", "MTU = 1280",
		"S1 = 0", "S2 = 0", "Jc = 4", "Jmin = 40", "Jmax = 70", "H1 = 1", "H2 = 2", "H3 = 3", "H4 = 4"}
	if i1 != "" {
		lines = append(lines, "I1 = "+i1)
	}
	lines = append(lines, "", "[Peer]", "PublicKey = "+k.Peer, "AllowedIPs = "+allowed, "Endpoint = "+endpoint, "PersistentKeepalive = 25")
	return strings.Join(lines, "\n") + "\n"
}

type WarpOpts struct {
	Endpoint string `json:"endpoint"` // "" / "auto" = probe
	I1       string `json:"i1"`
	Full     bool   `json:"full"`
	NewKeys  bool   `json:"new_keys"`
}

func keysFromConf(c string) *Keys {
	if c == "" || field(c, "PrivateKey") == "" {
		return nil
	}
	addr := strings.Split(field(c, "Address"), ",")
	k := &Keys{Priv: field(c, "PrivateKey"), Peer: field(c, "PublicKey"), V4: strings.TrimSpace(addr[0])}
	if len(addr) > 1 {
		k.V6 = strings.TrimSpace(addr[1])
	}
	return k
}

// GenerateWarp creates/refreshes WARP.conf and (optionally) probes endpoints by bringing the
// tunnel up and waiting for a handshake (_awg_try).
func GenerateWarp(j *app.Job, o WarpOpts) error {
	if o.I1 == "" {
		o.I1 = "quic"
	}
	k := keysFromConf(app.ReadText(WarpConf()))
	if k == nil || o.NewKeys {
		j.Say("==> Получаем ключи WARP")
		var err error
		if k, err = Register(j.Ctx(), j.Say); err != nil {
			return err
		}
	} else {
		j.Say("==> Ключи WARP уже есть — оставляем их")
	}
	i1 := I1(o.I1)
	eps := []string{o.Endpoint}
	if o.Endpoint == "" || o.Endpoint == "auto" {
		eps = Endpoints
	}
	probe := len(eps) > 1 && ClientInstalled() && !app.Dev
	for idx, ep := range eps {
		if err := SaveConf(WarpName, WarpConfText(k, ep, i1, o.Full)); err != nil {
			return err
		}
		if !probe {
			j.Say("==> WARP.conf сохранён (точка входа %s)", ep)
			break
		}
		j.Say("==> [%d/%d] Пробуем %s", idx+1, len(eps), ep)
		if err := Start(WarpName); err != nil {
			j.Say("   !! %v", err)
			continue
		}
		ok := false
		for i := 0; i < 10 && !j.Stopped(); i++ {
			time.Sleep(time.Second)
			if handshake(WarpName) > 0 {
				ok = true
				break
			}
		}
		if ok {
			j.Say("   ✓ рукопожатие есть — используем %s", ep)
			return nil
		}
		j.Say("   нет рукопожатия")
		_ = Stop(WarpName)
	}
	if probe {
		_ = SaveConf(WarpName, WarpConfText(k, Endpoints[0], i1, o.Full))
		return fmt.Errorf("ни одна точка входа WARP не ответила — провайдер, вероятно, блокирует WARP; попробуйте другой вариант маскировки I1")
	}
	return nil
}

type StatusInfo struct {
	Client    bool     `json:"client"`
	Tunnels   []Tunnel `json:"tunnels"`
	Warp      bool     `json:"warp"`
	Endpoints []string `json:"endpoints"`
	I1Kinds   []string `json:"i1_kinds"`
	Dir       string   `json:"dir"`
}

func Status() StatusInfo {
	return StatusInfo{Client: ClientInstalled(), Tunnels: Tunnels(), Warp: app.Exists(WarpConf()),
		Endpoints: Endpoints, I1Kinds: I1Kinds, Dir: Dir()}
}
