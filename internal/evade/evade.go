// Package evade is the adaptive control plane of Zapret Manager (layers L1/L3 of
// docs/adaptive-core.md). It answers two questions the fixed strategy catalogue cannot:
//
//	1. почему хост недоступен — SNI-фильтрация, сброс соединения, шейпинг по объёму
//	   или мобильный белый список;
//	2. что с этим делать — direct, desync (winws), app-level split или туннель.
//
// Wrong answers here are expensive in both directions: desync against a shaping engine
// wastes the user's time, and a tunnel where desync would do costs throughput.
package evade

import (
	"context"
	"sync"
)

// Class is the blocking mechanism that was actually observed.
type Class string

const (
	ClassOK        Class = "ok"        // не блокируется
	ClassSNI       Class = "sni"       // режет по SNI в ClientHello — desync применим
	ClassReset     Class = "reset"     // RST/чёрная дыра на любом TLS — desync почти бесполезен
	ClassThrottle  Class = "throttle"  // шейпинг по объёму — манипуляции пакетами не помогают
	ClassWhitelist Class = "whitelist" // мобильный белый список CIDR/SNI — нужен вход из РФ
	ClassUnknown   Class = "unknown"
)

// Plan is the cheapest treatment that can actually work for a Class.
type Plan string

const (
	PlanDirect Plan = "direct" // ничего не делать
	PlanDesync Plan = "desync" // winws: fake/split/ttl-фоллинг
	PlanSplit  Plan = "split"  // app-level split ClientHello (локальный прокси, без WinDivert)
	PlanTunnel Plan = "tunnel" // mihomo/WARP/relay: пакетные трюки структурно бесполезны
)

// Evidence is what the probes measured for one host. All durations are milliseconds.
type Evidence struct {
	Host  string `json:"host"`
	Port  string `json:"port"`
	Class Class  `json:"class,omitempty"` // заполняется Classify

	TCPOK  bool `json:"tcp_ok"`
	TCPms  int  `json:"tcp_ms,omitempty"`
	RSTms  int  `json:"rst_ms,omitempty"` // задержка до RST/обрыва, если была
	DirectTLSOk bool `json:"direct_tls_ok"`
	SplitTLSOk  bool `json:"split_tls_ok"`
	DirectErr   string `json:"direct_err,omitempty"`
	SplitErr    string `json:"split_err,omitempty"`

	// Скорость: маленькая выборка против 256 КБ в одном соединении. Время в микросекундах:
	// на быстрых каналах миллисекунды обнуляются и скорость становится невычислимой.
	SmallBytes int     `json:"small_bytes,omitempty"`
	SmallUs    float64 `json:"small_us,omitempty"`
	BulkBytes  int     `json:"bulk_bytes,omitempty"`
	BulkUs     float64 `json:"bulk_us,omitempty"`

	// Детект белых списков: доступен ли родной хост и доступен ли зарубежный.
	RUOK      *bool `json:"ru_ok,omitempty"`
	ForeignOK *bool `json:"foreign_ok,omitempty"`

	Note string `json:"note,omitempty"`
}

// SmallMbps и BulkMbps — скорость чтения тела, Мбит/с.
func (e Evidence) SmallMbps() float64 { return mbps(e.SmallBytes, e.SmallUs) }
func (e Evidence) BulkMbps() float64  { return mbps(e.BulkBytes, e.BulkUs) }

// mbps: байты*8 бит за us микросекунд → Мбит/с. us дробный: на быстрых каналах
// чтение 4 КБ занимает меньше микросекунды, и целочисленное время обнулялось бы.
func mbps(bytes int, us float64) float64 {
	if bytes <= 0 || us <= 0 {
		return 0
	}
	return float64(bytes) * 8 / us
}

// Verdict is the decision for one host. Evidence is kept so the caller (panel, model)
// can see *why* the verdict was made and feed the same numbers to the surrogate model.
type Verdict struct {
	Host       string   `json:"host"`
	Class      Class    `json:"class"`
	Plan       Plan     `json:"plan"`
	Fallback   Plan     `json:"fallback,omitempty"`
	Reason     string   `json:"reason"`
	Confidence float64  `json:"confidence"` // 0..1, насколько класс однозначен
	Evidence   Evidence `json:"evidence"`
}

// Classify turns measurements into a class + treatment. The order of the checks is the
// whole point: shaping and whitelists must be recognised *before* any desync attempt,
// because no packet trick can beat either of them.
func Classify(e Evidence) Verdict {
	v := Verdict{Host: e.Host, Evidence: e}

	ruBlocked := e.RUOK != nil && !*e.RUOK
	foreignBlocked := e.ForeignOK != nil && !*e.ForeignOK

	switch {
	case ruBlocked && foreignBlocked:
		v.Class, v.Plan, v.Confidence = ClassUnknown, PlanTunnel, 0.6
		v.Reason = "не отвечает ни родной, ни зарубежный адрес — похоже на отсутствие связи, а не на блокировку"

	case e.RUOK != nil && *e.RUOK && foreignBlocked:
		v.Class, v.Plan, v.Confidence = ClassWhitelist, PlanTunnel, 0.8
		v.Reason = "родные адреса доступны, зарубежные нет — белый список (мобильные операторы); нужен вход через IP из РФ"

	case !e.TCPOK:
		v.Class, v.Plan, v.Confidence = ClassUnknown, PlanTunnel, 0.5
		v.Reason = "TCP-соединение не устанавливается: адрес заблокирован или хост недоступен"
		if e.DirectErr != "" {
			v.Reason += " (" + e.DirectErr + ")"
		}

	case e.DirectTLSOk && throttled(e):
		v.Class, v.Plan, v.Fallback, v.Confidence = ClassThrottle, PlanTunnel, PlanDirect, 0.75
		v.Reason = "handshake проходит, но объём режется — шейпинг по размеру, desync здесь не поможет"

	case e.DirectTLSOk:
		v.Class, v.Plan, v.Confidence = ClassOK, PlanDirect, 0.85
		v.Reason = "хост открывается напрямую"

	case !e.DirectTLSOk && e.SplitTLSOk:
		v.Class, v.Plan, v.Fallback, v.Confidence = ClassSNI, PlanDesync, PlanSplit, 0.9
		v.Reason = "handshake рвётся целиком, но проходит по частям — фильтрация по SNI; помогает split/desync"

	case !e.DirectTLSOk && !e.SplitTLSOk:
		v.Class, v.Plan, v.Fallback, v.Confidence = ClassReset, PlanDesync, PlanTunnel, 0.6
		v.Reason = "handshake рвётся и целиком, и по частям — обрыв не по SNI; desync маловероятно поможет"
	}

	return v
}

// throttled reports shaping: the bulk read is much slower than the small one.
// Thresholds are deliberately loose — a transient stall must not look like shaping, and a
// false "throttle" only costs a tunnel attempt.
func throttled(e Evidence) bool {
	small, bulk := e.SmallMbps(), e.BulkMbps()
	if e.SmallBytes <= 0 || e.BulkBytes <= 0 || small <= 0 || bulk <= 0 {
		return false
	}
	if small < 0.2 {
		return false // слишком медленно, чтобы вообще что-то измерять
	}
	return bulk < small*0.35 || bulk < 0.5
}

// Decide maps a class to the plan a caller should execute; kept separate from Classify so
// policy (e.g. "never tunnel by default") can change without touching measurement.
func Decide(v Verdict) Plan {
	switch v.Class {
	case ClassOK, ClassThrottle:
		return v.Plan
	case ClassSNI:
		return PlanDesync
	case ClassReset:
		return PlanDesync
	case ClassWhitelist:
		return PlanTunnel
	}
	return PlanDesync
}

// ProbeFunc is what ProbeHost needs from the network layer; tests inject a fake.
type ProbeFunc func(ctx context.Context, host string, opt Options) Evidence

// Scan probes every host (bounded concurrency) and returns verdicts in input order.
func Scan(ctx context.Context, hosts []string, opt Options, probe ProbeFunc) []Verdict {
	if probe == nil {
		probe = ProbeHost
	}
	if opt.Parallel < 1 {
		opt.Parallel = 1
	}
	out := make([]Verdict, len(hosts))
	sem := make(chan struct{}, opt.Parallel)
	var wg sync.WaitGroup
	for i, h := range hosts {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, h string) {
			defer wg.Done()
			defer func() { <-sem }()
			defer func() {
				if r := recover(); r != nil {
					out[i] = Verdict{Host: h, Class: ClassUnknown, Plan: PlanTunnel, Reason: "проба упала"}
				}
			}()
			out[i] = Classify(probe(ctx, h, opt))
		}(i, h)
	}
	wg.Wait()
	return out
}
