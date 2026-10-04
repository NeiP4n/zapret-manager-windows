package zapret

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/zapretmanager/zmwin/internal/app"
)

// Hostlist editor (zapret_lists_status / zapret_list_get / zapret_list_set / zapret_list_restore).

var listFiles = map[string]string{
	"exclude": ExcludeList,
	"user":    UserList,
	"google":  GoogleList,
}

var ListOrder = []string{"exclude", "user", "google"}

type ListInfo struct {
	ID       string `json:"id"`
	Path     string `json:"path"`
	Exists   bool   `json:"exists"`
	Count    int    `json:"count"`
	Size     int64  `json:"size"`
	Mtime    int64  `json:"mtime"`
	Auto     string `json:"auto,omitempty"`
	Nochange bool   `json:"nochange"`
}

func ListInfoOf(id string) (ListInfo, error) {
	rel, ok := listFiles[id]
	if !ok {
		return ListInfo{}, fmt.Errorf("неизвестный список")
	}
	p := ListPath(rel)
	li := ListInfo{ID: id, Path: p}
	if fi, err := os.Stat(p); err == nil {
		li.Exists, li.Size, li.Mtime = true, fi.Size(), fi.ModTime().Unix()
	}
	li.Count = countLines(p)
	li.Nochange = Nochange(app.ReadText(p))
	if id == "exclude" {
		li.Auto = app.S().ExclAuto
	}
	return li, nil
}

func ListsStatus() ([]ListInfo, error) {
	if !Installed() {
		return nil, ErrNotInstalled
	}
	var out []ListInfo
	for _, id := range ListOrder {
		li, _ := ListInfoOf(id)
		out = append(out, li)
	}
	return out, nil
}

func ListGet(id string) (string, ListInfo, error) {
	li, err := ListInfoOf(id)
	if err != nil {
		return "", li, err
	}
	return app.ReadText(li.Path), li, nil
}

// ListSet saves a list: trims lines, collapses runs of blank lines, restarts winws.
func ListSet(id, content string) (ListInfo, error) {
	li, err := ListInfoOf(id)
	if err != nil {
		return li, err
	}
	if !Installed() {
		return li, ErrNotInstalled
	}
	var out []string
	blank := 0
	for _, l := range strings.Split(strings.ReplaceAll(content, "\r", ""), "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			blank++
			continue
		}
		if len(out) > 0 {
			for ; blank > 0; blank-- {
				out = append(out, "")
			}
		}
		blank = 0
		out = append(out, l)
	}
	if err := app.WriteText(li.Path, strings.Join(out, "\n")+"\n"); err != nil {
		return li, err
	}
	_ = Restart()
	return ListInfoOf(id)
}

// ListRestore re-downloads the default exclusion list.
func ListRestore(ctx context.Context, id string) (ListInfo, error) {
	if id != "exclude" {
		return ListInfo{}, fmt.Errorf("восстановить можно только список исключений")
	}
	p := ListPath(ExcludeList)
	if Nochange(app.ReadText(p)) {
		return ListInfo{}, fmt.Errorf("включено «Не изменять список» (#nochange) — выключите этот переключатель, чтобы восстановить исходный")
	}
	t, err := app.FetchText(ctx, ExcludeURL, 25*time.Second)
	if err != nil || strings.TrimSpace(t) == "" {
		return ListInfo{}, fmt.Errorf("не удалось скачать список исключений — текущий список не тронут")
	}
	if err := app.WriteText(p, t); err != nil {
		return ListInfo{}, err
	}
	_ = Restart()
	return ListInfoOf("exclude")
}

// ListNochange toggles the #nochange protection on a list or on the strategy ("strategy").
func ListNochange(id string, on bool) error {
	if id == "strategy" {
		return SetNochange(on)
	}
	rel, ok := listFiles[id]
	if !ok {
		return fmt.Errorf("неизвестный список")
	}
	p := ListPath(rel)
	cur := app.ReadText(p)
	if on {
		if !Nochange(cur) {
			return app.WriteText(p, "#nochange\n"+cur)
		}
		return nil
	}
	out := Join(StripNochange(strings.Split(cur, "\n")))
	if Nochange(out) {
		return fmt.Errorf("пометка #nochange стоит внутри другой строки — уберите её вручную в редакторе списка")
	}
	return app.WriteText(p, out)
}

// SetExclAuto sets the periodic exclusion-list refresh: off|h2|h4|h6|h12.
func SetExclAuto(v string) error {
	switch v {
	case "off", "h2", "h4", "h6", "h12":
	default:
		return fmt.Errorf("неизвестный интервал")
	}
	if v != "off" && !Installed() {
		return ErrNotInstalled
	}
	app.Update(func(s *app.Settings) { s.ExclAuto = v })
	return nil
}

// ExclTick refreshes the exclusion list if it changed (zapret_excl_tick, run by the scheduler).
func ExclTick(ctx context.Context) {
	if !Installed() {
		return
	}
	p := ListPath(ExcludeList)
	cur := app.ReadText(p)
	if Nochange(cur) {
		return
	}
	t, err := app.FetchText(ctx, ExcludeURL, 25*time.Second)
	if err != nil || !strings.Contains(t, ".") || t == cur {
		return
	}
	if app.WriteText(p, t) == nil {
		app.Logf("Список исключений Zapret обновлён по расписанию")
		_ = Restart()
	}
}

// AddUserDomain appends a domain to the user hostlist (used by the domain test "add to list").
func AddUserDomain(domains []string) error {
	p := ListPath(UserList)
	cur := app.ReadText(p)
	have := map[string]bool{}
	for _, l := range strings.Split(cur, "\n") {
		have[strings.TrimSpace(l)] = true
	}
	for _, d := range domains {
		if !have[d] {
			cur = strings.TrimRight(cur, "\n") + "\n" + d + "\n"
		}
	}
	return app.WriteText(p, strings.TrimLeft(cur, "\n"))
}
