package zapret

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/zapretmanager/zmwin/internal/app"
	"github.com/zapretmanager/zmwin/internal/osx"
)

// Strategy tester — port of do_test_run / do_autobest: every candidate is applied in turn, winws
// restarts and a set of blocked sites is fetched; results are ranked by sites opened.

const (
	suiteURL     = "https://raw.githubusercontent.com/hyperion-cs/dpi-checkers/refs/heads/main/ru/tcp-16-20/suite.v2.json"
	testParallel = 8
)

var staticURLs = [][2]string{
	{"gosuslugi.ru", "https://www.gosuslugi.ru"}, {"esia.gosuslugi.ru", "https://esia.gosuslugi.ru"},
	{"nalog.ru", "https://nalog.ru"}, {"lkfl2.nalog.ru", "https://lkfl2.nalog.ru"}, {"rutube.ru", "https://rutube.ru"},
	{"ntc.party", "https://ntc.party/"}, {"instagram.com", "https://instagram.com"}, {"facebook.com", "https://facebook.com"},
	{"rutracker.org", "https://rutracker.org"}, {"nnmclub.to", "https://nnmclub.to"}, {"rutor.info", "https://rutor.info"},
	{"openwrt.org", "https://openwrt.org"}, {"discord.com", "https://discord.com"}, {"x.com", "https://x.com"},
	{"forum.ru-board.com", "https://forum.ru-board.com"}, {"play.google.com", "https://play.google.com"},
	{"downloads.openwrt.org", "https://downloads.openwrt.org"},
	{"githubusercontent.com", "https://raw.githubusercontent.com/StressOzz/Zapret-Manager/refs/heads/main/Zapret-Manager.sh"},
}

var ytDomains = strings.Fields(`youtu.be youtube.com i.ytimg.com i9.ytimg.com yt3.ggpht.com yt4.ggpht.com googleapis.com
jnn-pa.googleapis.com googleusercontent.com signaler-pa.youtube.com youtubei.googleapis.com manifest.googlevideo.com
yt3.googleusercontent.com rr4---sn-4g5e6nze.googlevideo.com rr4---sn-5go7yner.googlevideo.com rr4---sn-q4flrnsl.googlevideo.com
rr5---sn-n8v7knez.googlevideo.com rr2---sn-q4fl6ndl.googlevideo.com rr1---sn-q4fl6n6y.googlevideo.com rr1---sn-aj5go5-53.googlevideo.com
rr1---sn-4axm-n8vs.googlevideo.com rr14---sn-n8v7kn7r.googlevideo.com rr16---sn-axq7sn76.googlevideo.com rr4---sn-jvhnu5g-c35d.googlevideo.com
rr1---sn-8ph2xajvh-5xge.googlevideo.com rr1---sn-xguxaxjvh-gufl.googlevideo.com rr1---sn-gvnuxaxjvh-jx3z.googlevideo.com
rr1---sn-gvnuxaxjvh-jx3l.googlevideo.com rr1---sn-gvnuxaxjvh-o8ge.googlevideo.com rr5---sn-gvnuxaxjvh-n8vk.googlevideo.com
rr10---sn-gvnuxaxjvh-304z.googlevideo.com rr12---sn-gvnuxaxjvh-bvwz.googlevideo.com rr3---sn-ug5onuxaxjvh-n8v6.googlevideo.com
rr1---sn-ug5onuxaxjvh-p5ge.googlevideo.com rr1---sn-ug5onuxaxjvh-p3ul.googlevideo.com rr1---sn-ug5onuxaxjvh-n8v6.googlevideo.com
rr1---sn-u5uuxaxjvhg0-ocje.googlevideo.com`)

var TestModes = []string{"v", "flowseal", "v_flowseal", "youtube", "current", "domain", "custom", "custom_yt"}

func TestDir() string        { return app.P("state", "test") }
func customTestFile() string { return app.P("state", "custom_test.txt") }

type Target struct{ Name, URL string }

type RunResult struct {
	Name  string   `json:"name"`
	OK    int      `json:"ok"`
	Total int      `json:"total"`
	Fails []string `json:"fails"`
	Ctrl  bool     `json:"control,omitempty"`
}

type TestResults struct {
	Mode    string      `json:"mode"`
	At      int64       `json:"at"`
	Domains []string    `json:"domains"`
	Items   []RunResult `json:"items"`
	Stopped bool        `json:"stopped"`
	targets []Target
}

func resultsPath(mode string) string { return filepath.Join(TestDir(), "results_"+mode+".json") }

func LoadResults(mode string) *TestResults {
	var r TestResults
	b, err := os.ReadFile(resultsPath(mode))
	if err != nil || json.Unmarshal(b, &r) != nil {
		return nil
	}
	return &r
}

func saveResults(r *TestResults) {
	_ = os.MkdirAll(TestDir(), 0o755)
	b, _ := json.MarshalIndent(r, "", " ")
	_ = app.WriteFileAtomic(resultsPath(r.Mode), b)
}

func ClearResults(mode string) {
	if mode == "" || mode == "all" {
		app.Remove(TestDir())
		return
	}
	app.Remove(resultsPath(mode))
}

func BlockedTargets(ctx context.Context) []Target {
	var out []Target
	for _, u := range staticURLs {
		out = append(out, Target{u[0], u[1]})
	}
	if b, err := app.Fetch(ctx, suiteURL, 15*time.Second); err == nil {
		var suite []struct {
			ID   string `json:"id"`
			Host string `json:"host"`
		}
		if json.Unmarshal(b, &suite) != nil {
			var wrap struct {
				Items []struct {
					ID   string `json:"id"`
					Host string `json:"host"`
				} `json:"items"`
			}
			_ = json.Unmarshal(b, &wrap)
			suite = wrap.Items
		}
		for _, s := range suite {
			if s.Host == "" {
				continue
			}
			h := s.Host
			if !strings.HasPrefix(h, "http") {
				h = "https://" + h
			}
			out = append(out, Target{s.ID, h})
		}
	}
	return out
}

func YoutubeTargets() []Target {
	var out []Target
	for _, d := range ytDomains {
		out = append(out, Target{d, "https://" + d + "/"})
	}
	return out
}

var domainRx = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)

// ParseDomains validates a user list for the "domain" test mode (max 30).
func ParseDomains(s string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, d := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ';' || r == ' ' || r == '\n' || r == '\t' }) {
		h := strings.ToLower(d)
		h = regexp.MustCompile(`^[a-zA-Z]+://`).ReplaceAllString(h, "")
		if i := strings.IndexAny(h, "/?#"); i >= 0 {
			h = h[:i]
		}
		if !domainRx.MatchString(h) {
			return nil, fmt.Errorf("не похоже на домен: %s", d)
		}
		if !seen[h] {
			seen[h] = true
			out = append(out, h)
		}
		if len(out) >= 30 {
			break
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("введите хотя бы один домен")
	}
	return out, nil
}

// checkURL mirrors `curl -sL --connect-timeout 4 --max-time 6 --range 0-65535`: any completed
// HTTP response counts as reachable; DPI resets / handshake timeouts count as blocked.
func checkURL(ctx context.Context, u string) bool {
	tr := &http.Transport{
		DialContext:         (&net.Dialer{Timeout: 4 * time.Second}).DialContext,
		TLSHandshakeTimeout: 5 * time.Second,
		DisableKeepAlives:   true,
		ForceAttemptHTTP2:   true,
		TLSClientConfig:     &tls.Config{},
		Proxy:               nil,
	}
	defer tr.CloseIdleConnections()
	cl := &http.Client{Transport: tr, Timeout: 6 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) curl/8.0")
	req.Header.Set("Range", "bytes=0-65535")
	resp, err := cl.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, err = io.Copy(io.Discard, io.LimitReader(resp.Body, 65536))
	return err == nil || err == io.EOF
}

// CheckAll fetches targets with limited parallelism.
func CheckAll(ctx context.Context, ts []Target) (ok int, fails []string) {
	var mu sync.Mutex
	sem := make(chan struct{}, testParallel)
	var wg sync.WaitGroup
	for _, t := range ts {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(t Target) {
			defer wg.Done()
			defer func() { <-sem }()
			good := checkURL(ctx, t.URL)
			mu.Lock()
			if good {
				ok++
			} else {
				fails = append(fails, t.Name)
			}
			mu.Unlock()
		}(t)
	}
	wg.Wait()
	sort.Strings(fails)
	return ok, fails
}

func candidates(ctx context.Context, mode string, j *app.Job) ([]Block, error) {
	var text string
	addV := func() {
		for n := 1; n <= 10; n++ {
			text += Join(StrategyV(n)) + "\n"
		}
	}
	addFS := func() error {
		if !app.Exists(flowsealFile()) {
			if err := DownloadFlowseal(j); err != nil {
				return err
			}
		}
		text += app.ReadText(flowsealFile()) + "\n"
		return nil
	}
	switch mode {
	case "v":
		addV()
	case "flowseal":
		if err := addFS(); err != nil {
			return nil, err
		}
	case "v_flowseal", "domain":
		if err := addFS(); err != nil {
			j.Say("!! Flowseal не скачался — проверяем только v1–v10")
		}
		addV()
	case "youtube":
		if !app.Exists(youtubeFile()) {
			if err := DownloadYoutube(ctx, j); err != nil {
				return nil, err
			}
		}
		text += app.ReadText(youtubeFile())
	case "custom", "custom_yt":
		text += NormalizePaths(app.ReadText(customTestFile()))
	}
	blocks := SplitBlocks(text, reHash)
	if mode != "youtube" && mode != "custom" && mode != "custom_yt" {
		var keep []Block
		for _, b := range blocks {
			if !strings.HasPrefix(b.Name, "Y") {
				keep = append(keep, b)
			}
		}
		blocks = keep
	}
	return blocks, nil
}

var testMu sync.Mutex

// RunTest executes a test job; mode as in the router panel. Returns results (also saved).
func RunTest(j *app.Job, mode string, domains []string) (*TestResults, error) {
	testMu.Lock()
	defer testMu.Unlock()
	ctx := j.Ctx()
	if !Installed() {
		return nil, ErrNotInstalled
	}
	res := &TestResults{Mode: mode, At: time.Now().Unix()}
	defer saveResults(res)

	var targets []Target
	switch mode {
	case "youtube", "custom_yt":
		targets = YoutubeTargets()
	case "domain":
		for _, d := range domains {
			targets = append(targets, Target{d, "https://" + d + "/"})
		}
	default:
		j.Say("==> Собираем список сайтов")
		targets = BlockedTargets(ctx)
	}
	for _, t := range targets {
		res.Domains = append(res.Domains, t.Name)
	}
	res.targets = targets

	if mode == "current" {
		AddGPDomains()
		_ = RefreshExclude(ctx)
		j.Say("==> Проверяем текущую стратегию, ничего не меняя")
		if err := Restart(); err != nil {
			return res, err
		}
		time.Sleep(time.Second)
		j.Say("==> Заблокированные сайты: %d", len(targets))
		ok, fails := CheckAll(ctx, targets)
		res.Items = append(res.Items, RunResult{Name: "Заблокированные сайты", OK: ok, Total: len(targets), Fails: fails})
		j.Say("   результат: %d из %d", ok, len(targets))
		if j.Stopped() {
			res.Stopped = true
			return res, nil
		}
		yt := YoutubeTargets()
		j.Say("==> YouTube: %d адресов", len(yt))
		ok, fails = CheckAll(ctx, yt)
		res.Items = append(res.Items, RunResult{Name: "YouTube", OK: ok, Total: len(yt), Fails: fails})
		j.Say("   результат: %d из %d", ok, len(yt))
		j.Say("==> Готово")
		return res, nil
	}

	backupBody := app.ReadText(bodyPath())
	backupSet := app.S()
	_ = app.WriteText(app.P("state", "strategy_test.backup"), backupBody)
	restore := func() {
		_ = app.WriteText(bodyPath(), backupBody)
		app.Update(func(s *app.Settings) {
			s.PortsTCP, s.PortsUDP, s.ZapretEnabled = backupSet.PortsTCP, backupSet.PortsUDP, backupSet.ZapretEnabled
		})
		_ = Restart()
		app.Remove(app.P("state", "strategy_test.backup"))
	}
	defer restore()
	AddGPDomains()
	_ = RefreshExclude(ctx)

	j.Say("==> Собираем стратегии")
	cmode := mode
	blocks, err := candidates(ctx, cmode, j)
	if err != nil {
		return res, err
	}
	if len(blocks) == 0 {
		return res, fmt.Errorf("не удалось собрать ни одной стратегии")
	}
	j.Say("==> Стратегий: %d, адресов: %d", len(blocks), len(targets))

	j.Say("==> Контрольный тест: Zapret выключен")
	osx.ProcStop(ProcName)
	time.Sleep(500 * time.Millisecond)
	ok, fails := CheckAll(ctx, targets)
	res.Items = append(res.Items, RunResult{Name: "Контрольный тест (Zapret выключен)", OK: ok, Total: len(targets), Fails: fails, Ctrl: true})
	j.Say("   результат: %d из %d", ok, len(targets))

	app.Update(func(s *app.Settings) { s.ZapretEnabled = true })
	for i, b := range blocks {
		if j.Stopped() {
			break
		}
		j.Say("==> [%d/%d] %s", i+1, len(blocks), b.Name)
		_ = app.WriteText(bodyPath(), Join(b.Lines)+"\n")
		if err := Restart(); err != nil {
			j.Say("   !! winws не запустился: %v", err)
			res.Items = append(res.Items, RunResult{Name: b.Name, OK: 0, Total: len(targets), Fails: []string{"winws не запустился"}})
			continue
		}
		time.Sleep(600 * time.Millisecond)
		ok, fails := CheckAll(ctx, targets)
		res.Items = append(res.Items, RunResult{Name: b.Name, OK: ok, Total: len(targets), Fails: fails})
		j.Say("   результат: %d из %d", ok, len(targets))
		saveResults(res)
	}
	if j.Stopped() {
		res.Stopped = true
		j.Say("==> Тест остановлен, возвращаем настройки")
	} else {
		j.Say("==> Тест завершён, возвращаем настройки")
	}
	sort.SliceStable(res.Items, func(a, b int) bool {
		if res.Items[a].Ctrl != res.Items[b].Ctrl {
			return res.Items[a].Ctrl
		}
		return res.Items[a].OK > res.Items[b].OK
	})
	if best := BestOf(res); best != nil {
		j.Say("==> Лучшая: %s → %d/%d", best.Name, best.OK, best.Total)
	}
	j.Say("==> Готово: настройки возвращены")
	return res, nil
}

func BestOf(r *TestResults) *RunResult {
	var best *RunResult
	for i := range r.Items {
		it := &r.Items[i]
		if it.Ctrl {
			continue
		}
		if best == nil || it.OK > best.OK {
			best = it
		}
	}
	return best
}

// RecoverTest restores the strategy if the manager died mid-test (_test_recover).
func RecoverTest() {
	p := app.P("state", "strategy_test.backup")
	if !app.Exists(p) || app.GetJob("strategy_test") != nil {
		return
	}
	_ = app.CopyFile(p, bodyPath())
	app.Remove(p)
	_ = Restart()
}

// ---------------- custom strategies for the test (/root/custom_test.txt) ----------------

func CustomTest() string { return app.ReadText(customTestFile()) }

func SetCustomTest(text string) (int, error) {
	ls := Lines(text)
	if len(ls) == 0 {
		app.Remove(customTestFile())
		return 0, nil
	}
	if !strings.HasPrefix(ls[0], "#") {
		return 0, fmt.Errorf("каждая стратегия начинается со строки #Название — первая строка должна быть такой")
	}
	for _, l := range ls {
		l = strings.TrimSpace(l)
		if !strings.HasPrefix(l, "#") && !strings.HasPrefix(l, "--") {
			return 0, fmt.Errorf("строка не похожа на параметр winws (должна начинаться с --): %s", l)
		}
		if strings.Contains(l, "'") {
			return 0, fmt.Errorf("одинарные кавычки ' в стратегиях нельзя")
		}
	}
	blocks := SplitBlocks(Join(ls), reHash)
	for _, b := range blocks {
		if len(b.Lines) < 2 {
			return 0, fmt.Errorf("у стратегии #%s нет ни одного параметра", b.Name)
		}
	}
	return len(blocks), app.WriteText(customTestFile(), NormalizePaths(Join(ls))+"\n")
}

// ---------------- scheduled auto-best ----------------

type AutoLast struct {
	At     int64  `json:"at"`
	Mode   string `json:"mode"`
	Best   string `json:"best"`
	BestOK int    `json:"best_ok"`
	Total  int    `json:"total"`
	CurOK  int    `json:"cur_ok"`
	Cur    string `json:"cur"`
	Result string `json:"result"`
}

var autoKV = app.NewKV("autobest_last")

func AutoLastGet() *AutoLast {
	var a AutoLast
	autoKV.Load(&a)
	if a.At == 0 {
		return nil
	}
	return &a
}

func AutoLastClear() { app.Remove(app.P("state", "autobest_last.json")) }

// AutoBest runs the scheduled selection: test, compare with current, apply only if better.
func AutoBest(j *app.Job) error {
	mode := app.S().AutobestMode
	if mode == "" {
		mode = "v_flowseal"
	}
	last := AutoLast{At: time.Now().Unix(), Mode: mode}
	defer func() { _ = autoKV.Save(&last) }()
	j.Say("==> Автоподбор стратегии: %s", map[string]string{"v": "v1–v10", "flowseal": "Flowseal"}[mode]+map[bool]string{true: "v + Flowseal"}[mode == "v_flowseal"])
	if !Installed() {
		last.Result = "error"
		return ErrNotInstalled
	}
	if Guard() != nil {
		j.Say("==> Включено «Не изменять стратегию» (#nochange) — автоподбор ничего не меняет")
		last.Result = "nochange"
		return nil
	}
	cur := ""
	for _, l := range LoadBody() {
		if strings.HasPrefix(l, "#") {
			n := strings.TrimSpace(strings.TrimPrefix(l, "#"))
			if !regexp.MustCompile(`^(udp443|Yv[0-9]+|Dv[0-9]+|Gv[0-9]+.*)$`).MatchString(n) {
				cur = n
				break
			}
		}
	}
	if cur == "" {
		cur = app.S().FlowsealName
	}
	last.Cur = cur
	res, err := RunTest(j, mode, nil)
	if err != nil {
		last.Result = "error"
		return err
	}
	best := BestOf(res)
	if best == nil || res.Stopped {
		j.Say("==> Лучшую стратегию определить не удалось — оставляем текущую")
		last.Result = "none"
		return nil
	}
	last.Best, last.BestOK, last.Total = best.Name, best.OK, best.Total
	j.Say("==> Проверяем текущую стратегию%s на тех же сайтах", map[bool]string{true: " (" + cur + ")"}[cur != ""])
	ts := res.targets
	cok, _ := CheckAll(j.Ctx(), ts)
	last.CurOK = cok
	j.Say("   текущая: %d/%d, лучшая из теста: %s — %d/%d", cok, len(ts), best.Name, best.OK, best.Total)
	if best.OK <= cok {
		j.Say("==> Текущая стратегия не хуже — оставляем её")
		last.Result = "kept"
		return nil
	}
	if Guard() != nil {
		last.Result = "nochange"
		return nil
	}
	j.Say("==> Применяем %s", best.Name)
	var aerr error
	if m := regexp.MustCompile(`^v([1-9]|10)$`).FindStringSubmatch(best.Name); m != nil {
		aerr = SetV(atoi(m[1]))
	} else {
		aerr = SetFlowseal(best.Name)
	}
	if aerr != nil {
		j.Say("!! Не удалось применить %s — оставлена текущая", best.Name)
		last.Result = "kept"
		return nil
	}
	j.Say("==> Готово: стратегия %s применена", best.Name)
	app.Logf("Автоподбор: применена стратегия %s (%d/%d, было %d)", best.Name, best.OK, best.Total, cok)
	last.Result = "applied"
	return nil
}
