package zapret

import (
	"archive/zip"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/zapretmanager/zmwin/internal/app"
)

var (
	fsMatchRx = regexp.MustCompile(`^--filter-udp=19294-19344,50000-50100|^--filter-tcp=%GameFilterTCP%|^--filter-udp=%GameFilterUDP%|^--filter-tcp=2053,2083,2087,2096,8443|^--filter-tcp=443 --hostlist="%LISTS%list-google.txt"|^--filter-tcp=80,443 --hostlist="%LISTS%list-general.txt"`)
	fsDropRx  = []string{
		`--hostlist="%LISTS%list-general.txt"`, `--ipset="%LISTS%ipset-all.txt"`, `--hostlist="%LISTS%list-general-user.txt"`,
		`--ipset-exclude="%LISTS%ipset-exclude.txt"`, `--ipset-exclude="%LISTS%ipset-exclude-user.txt"`, `--hostlist-exclude="%LISTS%list-exclude-user.txt"`,
	}
	fsBinRx  = regexp.MustCompile(`"%BIN%([^"]+)"`)
	fsListRx = regexp.MustCompile(`"%LISTS%([^"]+)"`)
	fsNewRx  = regexp.MustCompile(`--new[ \t]\^`)
	fsNumRx  = regexp.MustCompile(`\d+`)
)

// fsAlias maps Flowseal payload names to the files the router (and this port) actually ship.
var fsAlias = map[string]string{
	"tls_clienthello_4pda_to.bin": "4pda.bin",
	"ACTIVE_DISCORD_UDP.bin":      "quic_initial_steamcommunity_com.bin",
	"ACTIVE_GAME_UDP.bin":         "quic_initial_4pda_to.bin",
	"tls_clienthello_max_ru.bin":  "tls_clienthello_www_onetrust_com.bin",
}

// ParseFlowsealBat converts one general*.bat into a strategy block — a line-for-line port of the
// router's grep/sed pipeline, so both produce the same strategies.
func ParseFlowsealBat(name, bat string) []string {
	var match []string
	for _, l := range strings.Split(strings.ReplaceAll(bat, "\r", ""), "\n") {
		if fsMatchRx.MatchString(l) {
			match = append(match, l)
		}
	}
	if len(match) == 0 {
		return nil
	}
	out := []string{"#" + name}
	for _, l := range match {
		for _, part := range strings.Split(strings.ReplaceAll(l, "--", "\n--"), "\n") {
			part = strings.TrimRight(part, " \t")
			if part == "" {
				continue
			}
			out = append(out, part)
		}
	}
	var res []string
	for _, l := range out {
		drop := false
		for _, d := range fsDropRx {
			if strings.Contains(l, d) {
				drop = true
				break
			}
		}
		if drop {
			continue
		}
		l = strings.ReplaceAll(l, `"%LISTS%list-exclude.txt"`, ExcludeList)
		l = strings.ReplaceAll(l, `"%LISTS%list-google.txt"`, GoogleList)
		l = fsNewRx.ReplaceAllString(l, "--new")
		l = fsBinRx.ReplaceAllStringFunc(l, func(m string) string {
			f := fsBinRx.FindStringSubmatch(m)[1]
			if a, ok := fsAlias[f]; ok {
				f = a
			}
			return FK + f
		})
		l = fsListRx.ReplaceAllString(l, LS+"$1")
		l = strings.ReplaceAll(l, "^!", FK+"tls_clienthello_www_google_com.bin")
		l = strings.ReplaceAll(l, "%GameFilterTCP%", PortsGameTCP)
		l = strings.ReplaceAll(l, "%GameFilterUDP%", PortsGameUDP)
		l = strings.TrimRight(l, " \t")
		if strings.TrimSpace(l) == "" {
			continue
		}
		res = append(res, l)
	}
	return res
}

// flowsealOrder sorts "general", "general (ALT)", "general (ALT2)" … naturally.
func flowsealOrder(a, b string) bool {
	na, nb := fsNumRx.FindString(a), fsNumRx.FindString(b)
	ba, bb := fsNumRx.ReplaceAllString(a, ""), fsNumRx.ReplaceAllString(b, "")
	if ba != bb {
		return ba < bb
	}
	ia, _ := strconv.Atoi(na)
	ib, _ := strconv.Atoi(nb)
	return ia < ib
}

// DownloadFlowseal fetches the Flowseal repo zip, parses its .bat strategies and installs every
// payload it ships into files/fake (do_flowseal_download + do_add_fake_flow).
func DownloadFlowseal(j *app.Job) error {
	ctx := j.Ctx()
	j.Say("==> Скачиваем список стратегий Flowseal")
	zp := filepath.Join(app.TmpDir, "flowseal.zip")
	if err := app.Download(ctx, FlowsealZip, zp, 5, j); err != nil {
		return fmt.Errorf("не удалось скачать архив — проверьте соединение с GitHub")
	}
	defer app.Remove(zp)
	zr, err := zip.OpenReader(zp)
	if err != nil {
		return fmt.Errorf("архив повреждён: %v", err)
	}
	defer zr.Close()
	j.Say("==> Разбираем .bat-стратегии")
	type bat struct{ name, text string }
	var bats []bat
	fakes := 0
	for _, f := range zr.File {
		base := filepath.Base(f.Name)
		dir := filepath.Base(filepath.Dir(f.Name))
		rc, err := f.Open()
		if err != nil {
			continue
		}
		data, _ := io.ReadAll(io.LimitReader(rc, 4<<20))
		rc.Close()
		switch {
		case dir != "bin" && strings.HasPrefix(base, "general") && strings.HasSuffix(base, ".bat") && base != "general (ALT5).bat":
			bats = append(bats, bat{strings.TrimSuffix(base, ".bat"), string(data)})
		case dir == "bin" && strings.HasSuffix(base, ".bin"):
			dst := filepath.Join(app.FakeDir, base)
			if !app.Exists(dst) {
				_ = app.WriteFileAtomic(dst, data)
				fakes++
			}
			if a, ok := fsAlias[base]; ok && !strings.HasPrefix(base, "ACTIVE_") && !app.Exists(filepath.Join(app.FakeDir, a)) {
				_ = app.WriteFileAtomic(filepath.Join(app.FakeDir, a), data)
				fakes++
			}
		}
	}
	sort.Slice(bats, func(a, b int) bool { return flowsealOrder(bats[a].name, bats[b].name) })
	var out []string
	for _, b := range bats {
		if blk := ParseFlowsealBat(b.name, b.text); blk != nil {
			out = append(out, blk...)
		}
	}
	if len(out) == 0 {
		return fmt.Errorf("в архиве не найдено ни одной стратегии")
	}
	if err := app.WriteText(flowsealFile(), Join(out)+"\n"); err != nil {
		return err
	}
	if fakes > 0 {
		j.Say("   ✓ Добавлено fake-файлов: %d", fakes)
	}
	j.Say("==> Готово, стратегий: %d", len(FlowsealList()))
	return nil
}
