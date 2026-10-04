package app

import (
	"encoding/json"
	"os"
	"sync"
)

// Settings is the manager's persistent configuration (the router keeps the same knobs in UCI
// /etc/config/zapret and flag files under /opt/zapret-manager-luci).
type Settings struct {
	// Zapret (winws)
	ZapretEnabled bool   `json:"zapret_enabled"`
	PortsTCP      string `json:"ports_tcp"`
	PortsUDP      string `json:"ports_udp"`
	DisableIPv6   bool   `json:"disable_ipv6"`
	FlowsealName  string `json:"flowseal_name"`
	YvOff         bool   `json:"yv_off"`
	DvOff         bool   `json:"dv_off"`
	RknOn         bool   `json:"rkn_on"`
	Expert        bool   `json:"expert"`
	ZapretVersion string `json:"zapret_version"`
	// Game Xtreme restore data (router: /opt/zapret/tmp/GvXtreme)
	XtremeBackup []string `json:"xtreme_backup,omitempty"`

	// Zapret2 (winws2)
	Zapret2Enabled bool   `json:"zapret2_enabled"`
	Zapret2Version string `json:"zapret2_version"`

	// Schedules
	ExclAuto     string `json:"excl_auto"`     // off|h2|h4|h6|h12
	AutobestTime string `json:"autobest_time"` // HH:MM or ""
	AutobestMode string `json:"autobest_mode"` // v|flowseal|v_flowseal

	// UI
	Theme  string `json:"theme"`
	Mirror string `json:"mirror"` // GitHub proxy prefix ("" = direct)
	Port   int    `json:"port"`
}

func defaultSettings() Settings {
	return Settings{
		PortsTCP:     "80,443",
		PortsUDP:     "443",
		DisableIPv6:  true,
		ExclAuto:     "off",
		AutobestMode: "v_flowseal",
		Theme:        "auto",
		Port:         DefaultPort,
	}
}

var (
	setMu sync.Mutex
	cur   *Settings
)

func settingsPath() string { return P("state", "settings.json") }

// S returns a copy of the current settings.
func S() Settings {
	setMu.Lock()
	defer setMu.Unlock()
	loadLocked()
	return *cur
}

// Update mutates settings under lock and persists them.
func Update(fn func(s *Settings)) Settings {
	setMu.Lock()
	defer setMu.Unlock()
	loadLocked()
	fn(cur)
	b, _ := json.MarshalIndent(cur, "", "  ")
	_ = WriteFileAtomic(settingsPath(), b)
	return *cur
}

func loadLocked() {
	if cur != nil {
		return
	}
	s := defaultSettings()
	if b, err := os.ReadFile(settingsPath()); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	if s.Port == 0 {
		s.Port = DefaultPort
	}
	cur = &s
}

// KV is a tiny JSON key-value store for module state that doesn't belong in Settings.
type KV struct {
	path string
	mu   sync.Mutex
}

func NewKV(name string) *KV { return &KV{path: P("state", name+".json")} }

func (k *KV) Load(v any) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if b, err := os.ReadFile(k.path); err == nil {
		_ = json.Unmarshal(b, v)
	}
}

func (k *KV) Save(v any) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return WriteFileAtomic(k.path, b)
}
