package web

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zapretmanager/zmwin/internal/app"
	"github.com/zapretmanager/zmwin/internal/osx"
)

// Self-update from this project's GitHub releases.
const (
	SelfRepo  = "NeiP4n/zapret-manager-windows"
	SelfAsset = "ZapretManager.exe"
)

// newerVersion reports a > b for dotted versions ("1.10.0" > "1.9.3").
func newerVersion(a, b string) bool {
	pa, pb := strings.Split(strings.TrimPrefix(a, "v"), "."), strings.Split(strings.TrimPrefix(b, "v"), ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			return x > y
		}
	}
	return false
}

type SelfInfo struct {
	Current string `json:"current"`
	Latest  string `json:"latest"`
	Newer   bool   `json:"newer"`
	URL     string `json:"url"`
	Checked int64  `json:"checked"`
}

var (
	selfMu    sync.Mutex
	selfCache SelfInfo
)

func SelfCheck(force bool) SelfInfo {
	selfMu.Lock()
	defer selfMu.Unlock()
	if !force && selfCache.Checked > 0 && time.Since(time.Unix(selfCache.Checked, 0)) < 3*time.Hour {
		return selfCache
	}
	tag := app.LatestTag(nil, SelfRepo)
	selfCache = SelfInfo{Current: app.Version, Latest: strings.TrimPrefix(tag, "v"), Checked: time.Now().Unix(),
		URL: app.GHMain + "/" + SelfRepo + "/releases/latest"}
	selfCache.Newer = tag != "" && newerVersion(tag, app.Version)
	return selfCache
}

// SelfUpdate downloads the latest ZapretManager.exe, swaps it in and restarts the service.
func SelfUpdate(j *app.Job) error {
	info := SelfCheck(true)
	if info.Latest == "" {
		return fmt.Errorf("не удалось узнать последнюю версию — проверьте доступ к GitHub или смените зеркало")
	}
	if !info.Newer {
		j.Say("==> У вас последняя версия (%s)", app.Version)
		return nil
	}
	j.Say("==> Обновляем Zapret Manager %s → %s", app.Version, info.Latest)
	url := fmt.Sprintf("%s/%s/releases/download/v%s/%s", app.GHMain, SelfRepo, info.Latest, SelfAsset)
	dst := app.ExePath()
	tmp := dst + ".new"
	j.Say("==> Скачиваем %s", url)
	if err := app.Download(j.Ctx(), url, tmp, 4, j); err != nil {
		return err
	}
	b, err := os.ReadFile(tmp)
	if err != nil || len(b) < 1<<20 || !bytes.HasPrefix(b, []byte("MZ")) {
		os.Remove(tmp)
		return fmt.Errorf("скачанный файл не похож на программу Windows — обновление отменено")
	}
	j.Say("   ✓ Файл проверен (%d КБ)", len(b)>>10)
	if app.Dev {
		os.Remove(tmp)
		j.Say("==> [dev] замена exe и перезапуск службы пропущены")
		return nil
	}
	// a running exe can be renamed on Windows: old → .old, new → ZapretManager.exe
	_ = os.Remove(dst + ".old")
	if app.Exists(dst) {
		if err := os.Rename(dst, dst+".old"); err != nil {
			os.Remove(tmp)
			return fmt.Errorf("не удалось заменить программу: %v", err)
		}
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Rename(dst+".old", dst)
		return fmt.Errorf("не удалось заменить программу: %v", err)
	}
	j.Say("==> Перезапускаем службу — панель переподключится через несколько секунд")
	restartServiceDetached()
	return nil
}

// restartServiceDetached stops and starts the service from a helper that outlives this process.
func restartServiceDetached() {
	script := fmt.Sprintf("ping -n 3 127.0.0.1 >nul & sc.exe stop %s & ping -n 6 127.0.0.1 >nul & sc.exe start %s", app.ServiceName, app.ServiceName)
	cmd := exec.Command("cmd.exe", "/c", script)
	cmd.Dir = filepath.Dir(app.ExePath())
	osx.Detach(cmd)
	if err := cmd.Start(); err != nil {
		app.Logf("self-update restart: %v", err)
		go func() { time.Sleep(2 * time.Second); os.Exit(1) }() // SCM recovery restarts the service
	}
}
