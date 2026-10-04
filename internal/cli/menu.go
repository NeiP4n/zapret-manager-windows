// Package cli is the console menu — the same experience as `zms` on the router. It is a client
// of the service's API, so the console and the web panel always show the same state.
package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/zapretmanager/zmwin/internal/app"
	"github.com/zapretmanager/zmwin/internal/osx"
	"github.com/zapretmanager/zmwin/internal/web"
)

const (
	green   = "\033[1;32m"
	red     = "\033[1;31m"
	cyan    = "\033[1;36m"
	yellow  = "\033[1;33m"
	magenta = "\033[1;35m"
	nc      = "\033[0m"
)

type obj = map[string]any

var in = bufio.NewReader(os.Stdin)

func call(cmd string, args obj) (obj, error) {
	b, _ := json.Marshal(args)
	req, _ := http.NewRequest("POST", web.BaseURL()+"/api/"+cmd, bytes.NewReader(b))
	req.Header.Set("X-ZM", "1")
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "zm_token", Value: web.Token()})
	resp, err := (&http.Client{Timeout: 120 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("служба Zapret Manager не отвечает — запустите ZapretManager.exe")
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	var out obj
	if err := json.Unmarshal(data, &out); err != nil {
		var arr []any
		if json.Unmarshal(data, &arr) == nil {
			return obj{"items": arr}, nil
		}
		return nil, err
	}
	if e, ok := out["error"].(string); ok && e != "" {
		return out, fmt.Errorf("%s", e)
	}
	return out, nil
}

func str(m obj, path ...string) string {
	var cur any = m
	for _, p := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = mm[p]
	}
	switch v := cur.(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	}
	return ""
}

func boolv(m obj, path ...string) bool { return str(m, path...) == "true" }

func read(prompt string) string {
	fmt.Print(yellow + prompt + nc)
	s, _ := in.ReadString('\n')
	return strings.TrimSpace(s)
}

func pause() { read("\nНажмите Enter…") }

func clear() { fmt.Print("\033[H\033[2J") }

func say(ok bool, msg string) {
	if ok {
		fmt.Println(green + msg + nc)
	} else {
		fmt.Println(red + msg + nc)
	}
}

// do runs an API command; jobs are tailed until they finish.
func do(cmd string, args obj) {
	r, err := call(cmd, args)
	if err != nil {
		say(false, "\n"+err.Error())
		pause()
		return
	}
	if j := str(r, "job"); j != "" {
		tail(j)
		pause()
		return
	}
	say(true, "\nГотово")
	time.Sleep(700 * time.Millisecond)
}

func tail(job string) {
	from := 0
	for {
		r, err := call("log_tail", obj{"job": job, "from": from})
		if err != nil {
			say(false, err.Error())
			return
		}
		if ls, ok := r["lines"].([]any); ok {
			for _, l := range ls {
				s := fmt.Sprint(l)
				switch {
				case strings.HasPrefix(s, "==>"):
					fmt.Println(cyan + s + nc)
				case strings.HasPrefix(s, "ОШИБКА"), strings.HasPrefix(s, "!!"):
					fmt.Println(red + s + nc)
				default:
					fmt.Println(s)
				}
			}
			from += len(ls)
		}
		if st, ok := r["state"].(map[string]any); ok && st["running"] != true {
			if st["failed"] == true {
				say(false, "Операция завершилась с ошибкой")
			}
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func Status() error {
	enableVT()
	r, err := call("status", nil)
	if err != nil {
		return err
	}
	printInfo(r)
	return nil
}

func Uninstall() error {
	_, err := call("uninstall_all", nil)
	return err
}

func onoff(b bool, yes, no string) string {
	if b {
		return green + yes + nc
	}
	return red + no + nc
}

func printInfo(r obj) {
	z := r["zapret"].(map[string]any)
	if boolv(z, "installed") {
		fmt.Printf("%sZapret:%s        %s %s, %s\n", yellow, nc, green+"установлен"+nc, str(z, "version"), onoff(boolv(z, "running"), "работает", "остановлен"))
		fmt.Printf("%sСтратегия:%s     %s\n", yellow, nc, str(z, "strategy"))
	} else {
		fmt.Printf("%sZapret:%s        %s\n", yellow, nc, red+"не установлен"+nc)
	}
	z2 := r["zapret2"].(map[string]any)
	if boolv(z2, "installed") {
		fmt.Printf("%sZapret2:%s       установлен, %s\n", yellow, nc, onoff(boolv(z2, "running"), "работает", "остановлен"))
	}
	d := r["doh"].(map[string]any)
	fmt.Printf("%sDNS over HTTPS:%s %s\n", yellow, nc, onoff(boolv(d, "installed"), str(d, "current"), "выключен"))
	fmt.Printf("%shosts:%s         включено блоков: %s\n", yellow, nc, str(r, "hosts_blocks"))
	if cs, ok := z["conflicts"].([]any); ok && len(cs) > 0 {
		for _, c := range cs {
			if m, ok := c.(map[string]any); ok {
				fmt.Println(red + "!! " + fmt.Sprint(m["text"]) + nc)
			}
		}
		fmt.Println(yellow + "   Найден другой zapret — он мешает работе. Пункт «k» отключит его." + nc)
	}
	fmt.Printf("%sИнтернет:%s      %s\n", yellow, nc, onoff(boolv(r, "internet"), "есть", "нет связи"))
}

// Menu is the interactive console menu.
func Menu() error {
	enableVT()
	for {
		r, err := call("status", nil)
		if err != nil {
			return err
		}
		clear()
		fmt.Printf("╔═══════════════════════════════════════╗\n║  %sZapret Manager for Windows %-9s%s  ║\n╚═══════════════════════════════════════╝\n\n", cyan, app.Version, nc)
		printInfo(r)
		z := r["zapret"].(map[string]any)
		z2 := r["zapret2"].(map[string]any)
		z2t := "Установить"
		if boolv(z2, "installed") {
			z2t = "Удалить"
		}
		fmt.Printf("\n%s1)%s %sМеню%s Zapret\n%s2)%s %s%s%s Zapret2\n%s3)%s %sМеню%s TG WS Proxy\n%s4)%s %sМеню%s DNS over HTTPS\n%s5)%s %sМеню управления доменами в%s hosts\n%s6)%s %sОткрыть веб-панель%s\n",
			cyan, nc, green, nc, cyan, nc, green, z2t, nc, cyan, nc, green, nc, cyan, nc, green, nc, cyan, nc, green, nc, cyan, nc, green, nc)
		fmt.Printf("%sf)%s %sУдалить → установить → настроить%s Zapret\n%sm)%s %sСистемное меню%s\n", cyan, nc, green, nc, cyan, nc, green, nc)
		if boolv(z, "installed") {
			fmt.Printf("%ss)%s %s%s%s Zapret\n", cyan, nc, green, map[bool]string{true: "Остановить", false: "Запустить"}[boolv(z, "running")], nc)
		}
		switch strings.ToLower(read(cyan + "Enter)" + nc + green + " Выход" + nc + "\n\nВыберите пункт: ")) {
		case "1":
			zapretMenu()
		case "2":
			if boolv(z2, "installed") {
				if confirm("Удалить Zapret2?") {
					do("z2_action", obj{"action": "remove"})
				}
			} else {
				do("z2_action", obj{"action": "install"})
			}
		case "3":
			tgMenu()
		case "4":
			dohMenu()
		case "5":
			hostsMenu()
		case "6":
			osx.OpenURL(web.LoginURL())
		case "f", "а":
			if confirm("Удалить Zapret, поставить заново, применить v7, hosts и Gv1?") {
				do("zapret_action", obj{"action": "full"})
			}
		case "k", "л":
			if confirm("Отключить сторонний zapret / GoodbyeDPI (службы — в ручной запуск, процессы — остановить)?") {
				do("conflicts_fix", nil)
			}
		case "m", "ь":
			sysMenu()
		case "s", "ы":
			if boolv(z, "running") {
				do("zapret_action", obj{"action": "stop"})
			} else {
				do("zapret_action", obj{"action": "start"})
			}
		case "":
			return nil
		}
	}
}

func confirm(q string) bool {
	a := strings.ToLower(read(q + " (y/n): "))
	return a == "y" || a == "д" || a == "да" || a == "yes"
}

func zapretMenu() {
	for {
		r, err := call("zapret_status", nil)
		if err != nil {
			say(false, err.Error())
			pause()
			return
		}
		clear()
		fmt.Print(magenta + "Меню Zapret" + nc + "\n\n")
		act := "Установить"
		if boolv(r, "installed") {
			act = "Обновить / переустановить"
			fmt.Printf("Стратегия: %s\nПорты TCP: %s\nПорты UDP: %s\n", str(r, "strategy"), str(r, "ports_tcp"), str(r, "ports_udp"))
		}
		fmt.Printf("\n%s1)%s %s%s%s Zapret\n", cyan, nc, green, act, nc)
		if boolv(r, "installed") {
			fmt.Printf("%s2)%s Стратегии v1–v10\n%s3)%s Стратегии Flowseal\n%s4)%s Стратегии YouTube\n%s5)%s Discord\n%s6)%s Игры\n%s7)%s Тест стратегий\n%s8)%s %sУдалить%s Zapret\n",
				cyan, nc, cyan, nc, cyan, nc, cyan, nc, cyan, nc, cyan, nc, cyan, nc, red, nc)
		}
		switch read("Enter) Назад\n\nВыберите пункт: ") {
		case "1":
			do("zapret_action", obj{"action": "install"})
		case "2":
			pick("v", "")
		case "3":
			pick("flowseal", "")
		case "4":
			pick("youtube", "off")
		case "5":
			numMenu("discord_set", "num", "Номер Dv (1–17) или off: ")
		case "6":
			numMenu("game_set", "choice", "Номер Gv (1–4) или off: ")
		case "7":
			testMenu()
		case "8":
			if confirm("Удалить Zapret полностью?") {
				do("zapret_action", obj{"action": "remove"})
			}
		case "":
			return
		}
	}
}

func numMenu(cmd, key, prompt string) {
	v := strings.ToLower(read(prompt))
	if v == "" {
		return
	}
	if v != "off" && cmd == "discord_set" {
		v = "Dv" + strings.TrimPrefix(strings.ToLower(v), "dv")
	}
	if v != "off" && cmd == "game_set" {
		v = "Gv" + strings.TrimPrefix(strings.ToLower(v), "gv")
	}
	do(cmd, obj{key: v})
}

func pick(kind, extra string) {
	r, err := call("strategy_list", obj{"kind": kind})
	if err != nil {
		say(false, err.Error())
		pause()
		return
	}
	if j := str(r, "job"); j != "" {
		tail(j)
		if r, err = call("strategy_list", obj{"kind": kind}); err != nil {
			say(false, err.Error())
			pause()
			return
		}
	}
	var ids []string
	if items, ok := r["items"].([]any); ok {
		for _, it := range items {
			if m, ok := it.(map[string]any); ok {
				ids = append(ids, fmt.Sprint(m["id"]))
			} else {
				ids = append(ids, fmt.Sprint(it))
			}
		}
	}
	clear()
	for i, id := range ids {
		fmt.Printf("%s%2d)%s %s\n", cyan, i+1, nc, id)
	}
	if extra != "" {
		fmt.Printf("%s %s)%s выключить\n", cyan, extra, nc)
	}
	v := read("\nEnter) Назад\n\nНомер стратегии: ")
	if v == "" {
		return
	}
	id := v
	if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= len(ids) {
		id = ids[n-1]
	}
	if kind == "v" {
		id = strings.TrimPrefix(id, "v")
	}
	do("strategy_set", obj{"kind": kind, "id": id})
}

func testMenu() {
	clear()
	fmt.Print(magenta + "Тест стратегий" + nc + "\n\n")
	modes := []struct{ id, title string }{{"v", "v1–v10"}, {"flowseal", "Flowseal"}, {"v_flowseal", "v1–v10 + Flowseal"},
		{"youtube", "YouTube-стратегии"}, {"current", "Текущая стратегия"}, {"domain", "Свои домены"}}
	for i, m := range modes {
		fmt.Printf("%s%d)%s %s\n", cyan, i+1, nc, m.title)
	}
	v := read("\nEnter) Назад\n\nРежим: ")
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 || n > len(modes) {
		return
	}
	args := obj{"mode": modes[n-1].id}
	if modes[n-1].id == "domain" {
		args["domains"] = read("Домены через пробел: ")
	}
	r, err := call("test_start", args)
	if err != nil {
		say(false, err.Error())
		pause()
		return
	}
	tail(str(r, "job"))
	res, err := call("test_results", obj{"mode": modes[n-1].id})
	if err == nil {
		if items, ok := res["items"].([]any); ok {
			fmt.Print("\n" + magenta + "Результаты:" + nc + "\n")
			for _, it := range items {
				m := it.(map[string]any)
				fmt.Printf("  %-40s %s/%s\n", fmt.Sprint(m["name"]), str(m, "ok"), str(m, "total"))
			}
		}
	}
	pause()
}

func tgMenu() {
	r, err := call("tg_status", nil)
	if err != nil {
		say(false, err.Error())
		pause()
		return
	}
	clear()
	fmt.Print(magenta + "TG WS Proxy" + nc + "\n\n")
	items, _ := r["items"].([]any)
	for i, it := range items {
		m := it.(map[string]any)
		cfg, _ := m["config"].(map[string]any)
		st := red + "не установлен" + nc
		if boolv(cfg, "installed") {
			st = onoff(boolv(m, "running"), "работает", "остановлен") + "  " + str(m, "link")
		}
		fmt.Printf("%s%d)%s %s — %s\n", cyan, i+1, nc, str(m, "title"), st)
	}
	v := read("\nEnter) Назад\n\nВыберите прокси: ")
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 || n > len(items) {
		return
	}
	m := items[n-1].(map[string]any)
	id := str(m, "id")
	cfg, _ := m["config"].(map[string]any)
	if !boolv(cfg, "installed") {
		do("tg_install", obj{"id": id})
		return
	}
	switch read("1) Перезапустить  2) Остановить  3) Запустить  4) Удалить\nВыбор: ") {
	case "1":
		do("tg_action", obj{"id": id, "action": "restart"})
	case "2":
		do("tg_action", obj{"id": id, "action": "stop"})
	case "3":
		do("tg_action", obj{"id": id, "action": "start"})
	case "4":
		do("tg_remove", obj{"id": id})
	}
}

func dohMenu() {
	r, err := call("doh_status", nil)
	if err != nil {
		say(false, err.Error())
		pause()
		return
	}
	clear()
	fmt.Print(magenta + "DNS over HTTPS" + nc + "\n\n")
	fmt.Printf("Сейчас: %s\n\n", onoff(boolv(r, "installed"), str(r, "current"), "выключен"))
	ps, _ := r["providers"].([]any)
	for i, p := range ps {
		m := p.(map[string]any)
		fmt.Printf("%s%2d)%s %s\n", cyan, i+1, nc, str(m, "title"))
	}
	fmt.Printf("%s 0)%s выключить DoH\n", cyan, nc)
	v := read("\nEnter) Назад\n\nВыбор: ")
	if v == "" {
		return
	}
	if v == "0" {
		do("doh_enable", obj{"on": false})
		return
	}
	if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= len(ps) {
		do("doh_set", obj{"provider": str(ps[n-1].(map[string]any), "id")})
	}
}

func hostsMenu() {
	for {
		r, err := call("hosts_status", nil)
		if err != nil {
			say(false, err.Error())
			pause()
			return
		}
		clear()
		fmt.Print(magenta + "Домены в hosts" + nc + "\n\n")
		items, _ := r["items"].([]any)
		for i, it := range items {
			m := it.(map[string]any)
			fmt.Printf("%s%2d)%s %-45s %s\n", cyan, i+1, nc, str(m, "title"), onoff(boolv(m, "enabled"), "добавлен", "нет"))
		}
		fmt.Printf("%s r)%s вернуть hosts по умолчанию\n", cyan, nc)
		v := read("\nEnter) Назад\n\nВыберите блок (вкл/выкл): ")
		switch {
		case v == "":
			return
		case v == "r":
			if confirm("Вернуть стандартный hosts Windows?") {
				do("hosts_reset", nil)
			}
		default:
			if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= len(items) {
				do("hosts_toggle", obj{"id": str(items[n-1].(map[string]any), "id")})
			}
		}
	}
}

func sysMenu() {
	for {
		r, err := call("system_status", nil)
		if err != nil {
			say(false, err.Error())
			pause()
			return
		}
		clear()
		fmt.Print(magenta + "Системное меню" + nc + "\n\n")
		fmt.Printf("%s1)%s %s QUIC (UDP 443/80) в брандмауэре\n", cyan, nc, map[bool]string{true: "Разблокировать", false: "Заблокировать"}[boolv(r, "quic_blocked")])
		fmt.Printf("%s2)%s %s IPv6 в Zapret\n", cyan, nc, map[bool]string{true: "Выключить", false: "Включить"}[boolv(r, "ipv6_enabled")])
		fmt.Printf("%s3)%s %s Expert mode\n", cyan, nc, map[bool]string{true: "Выключить", false: "Включить"}[boolv(r, "expert_mode")])
		fmt.Printf("%s4)%s Проверить связь\n%s5)%s Версии компонентов\n%s6)%s Синхронизировать время\n%s9)%s %sУдалить Zapret Manager полностью%s\n",
			cyan, nc, cyan, nc, cyan, nc, cyan, nc, red, nc)
		switch read("\nEnter) Назад\n\nВыбор: ") {
		case "1":
			do("quic_toggle", nil)
		case "2":
			do("ipv6_toggle", nil)
		case "3":
			do("expert_toggle", nil)
		case "4":
			c, err := call("connectivity", nil)
			if err == nil {
				fmt.Printf("\nIPv4: %s %s мс\nIPv6: %s %s мс\n", onoff(boolv(c, "ipv4_ok"), "есть", "нет"), str(c, "ipv4_ms"), onoff(boolv(c, "ipv6_ok"), "есть", "нет"), str(c, "ipv6_ms"))
			}
			pause()
		case "5":
			v, err := call("versions", obj{"refresh": true})
			if err == nil {
				items, _ := v["items"].([]any)
				var rows []string
				for _, it := range items {
					m := it.(map[string]any)
					rows = append(rows, fmt.Sprintf("  %-32s %-14s %s", str(m, "name"), str(m, "installed"), str(m, "latest")))
				}
				sort.Strings(rows[1:])
				fmt.Println("\n  Компонент                        Установлен     Последняя")
				fmt.Println(strings.Join(rows, "\n"))
			}
			pause()
		case "6":
			do("time_sync", nil)
		case "9":
			if confirm("Удалить Zapret Manager, все компоненты и вернуть систему как было?") {
				do("uninstall_all", nil)
				os.Exit(0)
			}
		case "":
			return
		}
	}
}
