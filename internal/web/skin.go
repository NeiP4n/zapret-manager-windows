package web

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/zapretmanager/zmwin/internal/app"
	"github.com/zapretmanager/zmwin/internal/osx"
)

func osxPS(script string) (string, error) { return osx.PS(nil, 15e9, script) }

// User images for UI skins (e.g. the ULTRAKILL skin's background, portrait and rank art). They are
// supplied by the user and stay on this PC in %ProgramData%\ZapretManager\skins\<skin>\.

var (
	skinRx    = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)
	SkinSlots = map[string][]string{
		"ultrakill": {"bg", "portrait", "logo", "header", "rank_d", "rank_c", "rank_b", "rank_a", "rank_s", "rank_ss", "rank_sss", "rank_ultrakill"},
	}
	imgExt = map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/webp": ".webp", "image/gif": ".gif"}
)

func skinDir(skin string) string { return app.P("skins", skin) }

func slotOK(skin, slot string) bool {
	for _, s := range SkinSlots[skin] {
		if s == slot {
			return true
		}
	}
	return false
}

func slotFile(skin, slot string) string {
	for _, ext := range []string{".png", ".jpg", ".webp", ".gif"} {
		p := filepath.Join(skinDir(skin), slot+ext)
		if app.Exists(p) {
			return p
		}
	}
	return ""
}

// skinHandler serves /skin/<skin>/<slot> (auth required, same as the panel).
func skinHandler(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/skin/"), "/")
	if len(parts) != 2 || !skinRx.MatchString(parts[0]) || !slotOK(parts[0], parts[1]) {
		http.NotFound(w, r)
		return
	}
	p := slotFile(parts[0], parts[1])
	if p == "" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFile(w, r, p)
}

func registerSkin() {
	reg("skin_list", func(ctx context.Context, a Args) (any, error) {
		skin := a.S("skin")
		out := map[string]bool{}
		for _, s := range SkinSlots[skin] {
			out[s] = slotFile(skin, s) != ""
		}
		return M{"slots": SkinSlots[skin], "have": out}, nil
	})
	reg("skin_upload", func(ctx context.Context, a Args) (any, error) {
		skin, slot, data := a.S("skin"), a.S("slot"), a.S("data")
		if !slotOK(skin, slot) {
			return nil, fmt.Errorf("неизвестный слот")
		}
		i := strings.Index(data, ";base64,")
		if !strings.HasPrefix(data, "data:") || i < 0 {
			return nil, fmt.Errorf("ожидается картинка")
		}
		ext, ok := imgExt[data[5:i]]
		if !ok {
			return nil, fmt.Errorf("нужна картинка PNG, JPG, WEBP или GIF")
		}
		b, err := base64.StdEncoding.DecodeString(data[i+8:])
		if err != nil || len(b) > 6<<20 {
			return nil, fmt.Errorf("картинка повреждена или больше 6 МБ")
		}
		if old := slotFile(skin, slot); old != "" {
			_ = os.Remove(old)
		}
		return nil, app.WriteFileAtomic(filepath.Join(skinDir(skin), slot+ext), b)
	})
	reg("skin_steam_import", func(ctx context.Context, a Args) (any, error) {
		n, err := importSteamArt()
		return M{"imported": n}, err
	})
	reg("skin_steam_cdn", func(ctx context.Context, a Args) (any, error) {
		// official store art of the game, fetched on the user's PC only (never shipped in the repo)
		base := "https://cdn.cloudflare.steamstatic.com/steam/apps/" + ultrakillAppID + "/"
		files := map[string]string{"bg": "library_hero.jpg", "logo": "logo.png", "portrait": "library_600x900_2x.jpg", "header": "header.jpg"}
		n := 0
		var last error
		for slot, f := range files {
			b, err := app.Fetch(ctx, base+f, 40e9)
			if err != nil || len(b) < 1000 {
				last = err
				continue
			}
			if old := slotFile("ultrakill", slot); old != "" {
				_ = os.Remove(old)
			}
			if app.WriteFileAtomic(filepath.Join(skinDir("ultrakill"), slot+filepath.Ext(f)), b) == nil {
				n++
			}
		}
		if n == 0 {
			return nil, fmt.Errorf("не удалось скачать арты из Steam: %v", last)
		}
		return M{"imported": n}, nil
	})
	reg("skin_remove", func(ctx context.Context, a Args) (any, error) {
		if !slotOK(a.S("skin"), a.S("slot")) {
			return nil, fmt.Errorf("неизвестный слот")
		}
		if p := slotFile(a.S("skin"), a.S("slot")); p != "" {
			_ = os.Remove(p)
		}
		return nil, nil
	})
}

// ULTRAKILL's Steam app id: its official library art is cached locally by the user's own Steam.
const ultrakillAppID = "1229490"

func steamRoots() []string {
	var roots []string
	if out, err := osxPS(`(Get-ItemProperty 'HKCU:\Software\Valve\Steam' -EA SilentlyContinue).SteamPath; (Get-ItemProperty 'HKLM:\SOFTWARE\WOW6432Node\Valve\Steam' -EA SilentlyContinue).InstallPath`); err == nil {
		for _, l := range strings.Split(out, "\n") {
			if l = strings.TrimSpace(l); l != "" {
				roots = append(roots, filepath.FromSlash(l))
			}
		}
	}
	if h, err := os.UserHomeDir(); err == nil { // dev mode on Linux
		roots = append(roots, filepath.Join(h, ".steam", "steam"), filepath.Join(h, ".local", "share", "Steam"))
	}
	for _, env := range []string{"ProgramFiles(x86)", "ProgramFiles"} {
		if v := os.Getenv(env); v != "" {
			roots = append(roots, filepath.Join(v, "Steam"))
		}
	}
	return roots
}

// importSteamArt copies ULTRAKILL's library hero / logo / capsule / header from the local Steam
// cache (both the old flat and the new per-app folder layout) into the skin slots.
func importSteamArt() (int, error) {
	want := map[string][]string{
		"bg":       {"library_hero.jpg", "library_hero.png"},
		"logo":     {"logo.png"},
		"portrait": {"library_600x900.jpg", "library_600x900_2x.jpg", "library_capsule.jpg"},
		"header":   {"header.jpg", "library_header.jpg"},
	}
	n := 0
	for _, root := range steamRoots() {
		cache := filepath.Join(root, "appcache", "librarycache")
		if !app.Exists(cache) {
			continue
		}
		for slot, names := range want {
			for _, name := range names {
				cands := []string{filepath.Join(cache, ultrakillAppID+"_"+name), filepath.Join(cache, ultrakillAppID, name)}
				if sub, _ := filepath.Glob(filepath.Join(cache, ultrakillAppID, "*", name)); len(sub) > 0 {
					cands = append(cands, sub...)
				}
				found := false
				for _, c := range cands {
					if b, err := os.ReadFile(c); err == nil && len(b) > 0 {
						if old := slotFile("ultrakill", slot); old != "" {
							_ = os.Remove(old)
						}
						_ = app.WriteFileAtomic(filepath.Join(skinDir("ultrakill"), slot+filepath.Ext(c)), b)
						n++
						found = true
						break
					}
				}
				if found {
					break
				}
			}
		}
		if n > 0 {
			return n, nil
		}
	}
	return 0, fmt.Errorf("арты ULTRAKILL не найдены в кэше Steam — откройте игру в библиотеке Steam (чтобы он скачал обложки) или загрузите картинки вручную")
}
