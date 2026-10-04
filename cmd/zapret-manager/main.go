// Zapret Manager for Windows — port of StressOzz Zapret Manager (OpenWrt) to Windows.
//
//	ZapretManager.exe            install/update if needed and open the panel
//	ZapretManager.exe open       open the panel in the browser
//	ZapretManager.exe menu       console menu (like `zms` on the router)
//	ZapretManager.exe status     short status
//	ZapretManager.exe install    install / update the service
//	ZapretManager.exe uninstall  remove everything
//	ZapretManager.exe run        run in the foreground (debug)
//	ZapretManager.exe service    entry point used by the Windows service
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/zapretmanager/zmwin/internal/app"
	"github.com/zapretmanager/zmwin/internal/cli"
	"github.com/zapretmanager/zmwin/internal/core"
	"github.com/zapretmanager/zmwin/internal/osx"
	"github.com/zapretmanager/zmwin/internal/web"
)

func main() {
	cmd := ""
	if len(os.Args) > 1 {
		cmd = strings.ToLower(strings.TrimLeft(os.Args[1], "-/"))
	}
	if isService() || cmd == "service" {
		runService()
		return
	}
	switch cmd {
	case "version":
		fmt.Println("Zapret Manager for Windows", app.Version)
		return
	case "help", "h", "?":
		fmt.Println(usage)
		return
	}
	if !osx.IsAdmin() {
		if err := osx.Elevate(os.Args[1:]); err != nil {
			fmt.Println("Нужны права администратора:", err)
			pause()
		}
		return
	}
	var err error
	switch cmd {
	case "":
		err = launch()
	case "open":
		err = openPanel()
	case "install", "update":
		err = install()
		if err == nil {
			fmt.Println("Готово. Панель:", web.BaseURL())
		}
	case "uninstall":
		err = uninstall()
	case "run":
		err = runForeground()
	case "menu":
		err = cli.Menu()
	case "status":
		err = cli.Status()
	default:
		fmt.Println(usage)
		return
	}
	if err != nil {
		fmt.Println("ОШИБКА:", err)
		pause()
		os.Exit(1)
	}
}

var usage = `Zapret Manager for Windows ` + app.Version + `
  (без аргументов)  установить/обновить и открыть панель
  open              открыть веб-панель
  menu              консольное меню
  status            состояние
  install|update    установить или обновить службу
  uninstall         удалить всё и вернуть систему как было
  run               запустить в консоли (отладка)`

func pause() {
	fmt.Print("\nНажмите Enter…")
	_, _ = fmt.Scanln()
}

func runForeground() error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	if osx.ServiceRunning() {
		return fmt.Errorf("служба уже работает — сначала остановите её (sc stop %s)", app.ServiceName)
	}
	fmt.Printf("Zapret Manager %s — панель %s (Ctrl+C — выход)\n", app.Version, web.LoginURL())
	return core.Run(ctx)
}

// install copies the exe into the data folder, registers the service and shortcuts.
func install() error {
	app.EnsureDirs()
	self, err := os.Executable()
	if err != nil {
		return err
	}
	dst := app.ExePath()
	same := false
	if a, err1 := filepath.Abs(self); err1 == nil {
		if b, err2 := filepath.Abs(dst); err2 == nil && strings.EqualFold(a, b) {
			same = true
		}
	}
	if !same {
		fmt.Println("==> Копируем программу в", app.Base)
		if osx.ServiceRunning() {
			_ = osx.ServiceStop()
			time.Sleep(time.Second)
		}
		if err := copyFile(self, dst); err != nil {
			return fmt.Errorf("не удалось скопировать: %v", err)
		}
	}
	if !osx.ServiceInstalled() {
		fmt.Println("==> Регистрируем службу", app.ServiceName)
		if err := osx.ServiceInstall(dst); err != nil {
			return err
		}
	}
	if !osx.ServiceRunning() {
		fmt.Println("==> Запускаем службу")
		if err := osx.ServiceStart(); err != nil {
			return err
		}
	}
	createShortcuts(dst)
	return waitPanel(20 * time.Second)
}

func copyFile(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	tmp := dst + ".new"
	if err := os.WriteFile(tmp, b, 0o755); err != nil {
		return err
	}
	_ = os.Remove(dst + ".old")
	if app.Exists(dst) {
		if err := os.Rename(dst, dst+".old"); err != nil {
			return err
		}
	}
	return os.Rename(tmp, dst)
}

func createShortcuts(target string) {
	ps := `$w=New-Object -ComObject WScript.Shell; foreach($d in @([Environment]::GetFolderPath('CommonDesktopDirectory'), (Join-Path $env:ProgramData 'Microsoft\Windows\Start Menu\Programs'))){ ` +
		`$s=$w.CreateShortcut((Join-Path $d 'Zapret Manager.lnk')); $s.TargetPath=` + osx.PSQuote(target) + `; $s.Arguments='open'; ` +
		`$s.WorkingDirectory=` + osx.PSQuote(app.Base) + `; $s.IconLocation=` + osx.PSQuote(target+",0") + `; $s.Description='Zapret Manager — обход DPI'; $s.Save() }`
	_, _ = osx.PS(nil, 30*time.Second, ps)
}

func waitPanel(d time.Duration) error {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		resp, err := http.Get(web.BaseURL() + "/byetube.pac")
		if err == nil {
			resp.Body.Close()
			return nil
		}
		time.Sleep(400 * time.Millisecond)
	}
	return fmt.Errorf("панель не ответила на %s — смотрите %s", web.BaseURL(), app.P("logs", "manager.log"))
}

func launch() error {
	fmt.Println("Zapret Manager for Windows", app.Version)
	self, _ := os.Executable()
	needUpdate := !osx.ServiceInstalled() || !sameFile(self, app.ExePath())
	if needUpdate || !osx.ServiceRunning() {
		if err := install(); err != nil {
			return err
		}
	}
	return openPanel()
}

func sameFile(a, b string) bool {
	ia, err1 := os.Stat(a)
	ib, err2 := os.Stat(b)
	if err1 != nil || err2 != nil {
		return false
	}
	if os.SameFile(ia, ib) {
		return true
	}
	return ia.Size() == ib.Size() && ia.ModTime().Equal(ib.ModTime())
}

func openPanel() error {
	if !osx.ServiceRunning() && !app.Dev {
		if err := install(); err != nil {
			return err
		}
	}
	if err := waitPanel(15 * time.Second); err != nil {
		return err
	}
	fmt.Println("Открываем панель:", web.BaseURL())
	osx.OpenURL(web.LoginURL())
	return nil
}

func uninstall() error {
	fmt.Print("Удалить Zapret Manager, все компоненты и настройки? Введите «да»: ")
	var ans string
	_, _ = fmt.Scanln(&ans)
	if !strings.EqualFold(strings.TrimSpace(ans), "да") && !strings.EqualFold(strings.TrimSpace(ans), "yes") {
		fmt.Println("Отменено")
		return nil
	}
	if osx.ServiceRunning() {
		if err := cli.Uninstall(); err == nil {
			fmt.Println("Готово")
			return nil
		}
	}
	_ = osx.ServiceRemove()
	web.RemoveShortcut()
	fmt.Println("Служба удалена. Папку", app.Base, "можно удалить вручную.")
	return nil
}
