package zapret

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/zapretmanager/zmwin/internal/app"
)

// TestRouterParity replays operation sequences through the router's original shell functions
// (ZM_HARNESS=run.sh) and through this port, comparing strategy body and ports after every step.
// Run with: ZM_BASE=$(mktemp -d) ZM_OFFLINE=1 ZM_HARNESS=… ZM_YT=StrYoutube ZM_FS_REF=… go test
func TestRouterParity(t *testing.T) {
	h := os.Getenv("ZM_HARNESS")
	if h == "" {
		t.Skip("set ZM_HARNESS")
	}
	yt, fs := os.Getenv("ZM_YT"), os.Getenv("ZM_FS_REF")
	scenarios := [][]string{
		{"strategy_set_v v7", "youtube_quic_set on", "discord_set_dv 5", "game_set 2", "game_toggle_xtreme", "strategy_set_v v3"},
		{"strategy_set_v v2", "strategy_set_youtube Yv03", "discord_set_fake stun2.bin", "game_set 1", "game_set_fake quic_initial_5ka_ru.bin", "strategy_set_v v9", "youtube_quic_set on", "youtube_quic_set off"},
		{"strategy_set_v v5", "discord_set_dv off", "strategy_set_v v6", "discord_set_dv 12", "strategy_set_youtube off", "strategy_set_v v1", "strategy_set_youtube Yv11", "game_set 3", "game_set off"},
		{"strategy_set_v v4", "youtube_quic_set on", "strategy_set_flowseal 'general (ALT11)'", "strategy_set_flowseal general", "strategy_set_v v8", "strategy_set_youtube Yv20", "game_set 4", "game_toggle_xtreme", "game_toggle_xtreme", "strategy_set_v v10"},
		{"strategy_set_v v7", "_zo_wss_add", "strategy_set_v v2", "_zo_wss_del", "strategy_set_youtube Yv05", "discord_set_dv 17", "discord_set_dv 3", "strategy_set_flowseal 'general (FAKE TLS AUTO)'", "discord_set_dv 9"},
	}
	for si, sc := range scenarios {
		w := t.TempDir()
		_ = os.MkdirAll(w+"/jobs", 0o755)
		copyF(t, yt, w+"/jobs/youtube_strategies.txt")
		copyF(t, fs, w+"/jobs/flowseal_strategies.txt")
		out, err := exec.Command("bash", append([]string{h, w}, sc...)...).Output()
		if err != nil {
			t.Fatalf("harness: %v", err)
		}
		router := parseDump(NormalizePaths(string(out)))

		// fresh Go state
		app.Remove(app.StateDir)
		_ = os.MkdirAll(app.StateDir, 0o755)
		resetSettings()
		_ = app.WriteText(youtubeFile(), NormalizePaths(readFile(t, yt)))
		_ = app.WriteText(flowsealFile(), NormalizePaths(readFile(t, fs)))
		_ = SaveBody([]string{"--filter-tcp=443"})
		for i, op := range sc {
			runOp(t, op)
			s := app.S()
			got := Join(LoadBody()) + "\nTCP=" + s.PortsTCP + "\nUDP=" + s.PortsUDP
			if got != router[i] {
				t.Errorf("scenario %d step %d %q differs:\n--- go\n%s\n--- router\n%s", si, i, op, got, router[i])
				break
			}
		}
	}
}

func resetSettings() {
	app.Update(func(s *app.Settings) {
		*s = app.Settings{PortsTCP: "80,443", PortsUDP: "443", ZapretEnabled: true}
	})
}

func runOp(t *testing.T, op string) {
	f := strings.SplitN(op, " ", 2)
	arg := ""
	if len(f) > 1 {
		arg = strings.Trim(f[1], "'")
	}
	var err error
	switch f[0] {
	case "strategy_set_v":
		err = SetV(atoi(strings.TrimPrefix(arg, "v")))
	case "youtube_quic_set":
		err = SetYoutubeQuic(arg == "on")
	case "discord_set_dv":
		err = SetDiscord(arg)
	case "discord_set_fake":
		err = SetDiscordFake(arg)
	case "game_set":
		err = SetGame(arg)
	case "game_set_fake":
		err = SetGameFake(arg)
	case "game_toggle_xtreme":
		err = ToggleXtreme()
	case "strategy_set_youtube":
		err = SetYoutube(arg)
	case "strategy_set_flowseal":
		err = SetFlowseal(arg)
	case "_zo_wss_add":
		err = SetOpt("wssize", "on", nil)
	case "_zo_wss_del":
		err = SetOpt("wssize", "off", nil)
	default:
		t.Fatalf("unknown op %s", op)
	}
	if err != nil {
		t.Logf("op %q: %v", op, err)
	}
}

func parseDump(s string) []string {
	var steps []string
	for _, part := range strings.Split(s, "=== ")[1:] {
		i := strings.Index(part, "\n")
		steps = append(steps, strings.TrimRight(part[i+1:], "\n"))
	}
	return steps
}

func copyF(t *testing.T, a, b string) {
	if err := os.WriteFile(b, []byte(readFile(t, a)), 0o644); err != nil {
		t.Fatal(err)
	}
}
