package routing

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/zapretmanager/zmwin/internal/app"
	"github.com/zapretmanager/zmwin/internal/awg"
)

// TestGenerateValid feeds the generated config to a real mihomo binary (ZM_MIHOMO) with -t.
func TestGenerateValid(t *testing.T) {
	bin := os.Getenv("ZM_MIHOMO")
	if bin == "" {
		t.Skip("ZM_MIHOMO")
	}
	_ = os.MkdirAll(rulesDir(), 0o755)
	_ = app.WriteText(listFile("youtube"), "+.youtube.com\n+.googlevideo.com\n")
	_ = app.WriteText(listFile("discord"), "+.discord.com\n")
	_ = app.WriteText(subnetFile("discord"), "162.159.128.0/19\n")
	_ = app.WriteText(listFile("russia_inside"), "+.rutracker.org\n")
	ws := []warpAcc{{Keys: awg.Keys{Priv: "aGVsbG8tdGhpcy1pcy1hLWR1bW15LXRlc3Qta2V5IT0=", Peer: "bmXOC+F1FxEMF9dyiK2H5/1SUtzH0JuVo51h2wPfgyo=", V4: "172.16.0.2", V6: "2606:4700:110:85f2::1"}, Endpoint: "162.159.192.1:2408"}}
	b, _ := json.Marshal(ws)
	_ = app.WriteFileAtomic(warpKeysPath(), b)
	c := Load()
	c.Exclude = "sberbank.ru\n10.10.0.0/16"
	c.BlockQUIC = true
	c.Steer = Steer{Enabled: true, Tunnels: 1, Services: []string{"youtube"}, Domains: "chatgpt.com", I1: "dns"}
	c.Sections = []Section{
		{ID: "s1", Name: "VPN", Enabled: true, Mode: "links", Links: "vless://11111111-2222-3333-4444-555555555555@example.com:443?security=reality&sni=www.google.com&fp=chrome&pbk=abc&sid=01&type=tcp&flow=xtls-rprx-vision#NL\nss://YWVzLTI1Ni1nY206cGFzcw@1.2.3.4:8388#SS", Services: []string{"discord", "russia_inside"}, Domains: "x.com", Subnets: "91.108.4.0/22"},
		{ID: "s2", Name: "Sub", Enabled: true, Mode: "subscription", SubURLs: "https://example.com/sub", SubIv: "1h", Filter: "NL,DE", ExFilter: "RU"},
		{ID: "s3", Name: "WG", Enabled: true, Mode: "interface", Iface: "awg0", Domains: "instagram.com"},
		{ID: "s4", Name: "W", Enabled: true, Mode: "warp", Domains: "openai.com"},
	}
	m, err := Generate(c)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(Dir(), "test.json")
	out, _ := json.MarshalIndent(m, "", " ")
	_ = os.WriteFile(p, out, 0o644)
	res, err := exec.Command(bin, "-t", "-d", Dir(), "-f", p).CombinedOutput()
	t.Logf("%s", res)
	if err != nil {
		t.Fatalf("mihomo -t failed: %v", err)
	}
	c.Mixomo = Mixomo{SubURL: "https://example.com/sub", ListPreset: "russia_inside,discord", WarpProxy: true}
	out, _ = json.MarshalIndent(MixomoTemplate(c), "", " ")
	_ = os.WriteFile(p, out, 0o644)
	res, err = exec.Command(bin, "-t", "-d", Dir(), "-f", p).CombinedOutput()
	t.Logf("mixomo: %s", res)
	if err != nil {
		t.Fatalf("mixomo template rejected: %v", err)
	}
}
