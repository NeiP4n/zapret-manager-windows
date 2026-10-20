package evade

// Bootstrap-корпус для обучения модели: синтетические замеры, размеченные правилами
// Classify (distillation). Нужен ровно для того, чтобы модель работала «из коробки»,
// пока нет реальных исходов. Когда тестер стратегий накопит результаты, обучение
// переключается на них (state\test\results_*.json) — тот же Sample и тот же Train.

import (
	"math"
	"math/rand"
)

// SynthSamples генерирует n наблюдений по шести типовым сценариям блокировки с шумом.
func SynthSamples(n int, seed int64) []Sample {
	r := rand.New(rand.NewSource(seed))
	out := make([]Sample, 0, n)
	for i := 0; i < n; i++ {
		ev := synthEvidence(r)
		ev.Host = "synthetic"
		sp := synthParams(r)
		out = append(out, Sample{
			F:     FeaturesOf(ev, sp),
			Label: LabelOf(Classify(ev).Class),
		})
	}
	return out
}

func synthParams(r *rand.Rand) StrategyParams {
	desyncs := []string{"fake,multisplit", "multisplit", "fake,split2", "fake,disorder", ""}
	fooldings := []string{"ts", "badseq", "badsum", "badseq,badsum", ""}
	poses := []string{"1", "2", "midsld", "sniext+1", ""}
	p := StrategyParams{
		Desync:   desyncs[r.Intn(len(desyncs))],
		Fooling:  fooldings[r.Intn(len(fooldings))],
		SplitPos: poses[r.Intn(len(poses))],
	}
	if r.Intn(3) > 0 {
		p.TTL = 3 + r.Intn(10)
	}
	if r.Intn(4) > 0 {
		p.Repeats = 4 + r.Intn(9)
	}
	if r.Intn(2) == 0 {
		p.SeqOvl = []int{568, 582, 654, 681, 726}[r.Intn(5)]
	}
	if r.Intn(8) == 0 {
		p.AutoTTL = true
	}
	return p
}

func synthEvidence(r *rand.Rand) Evidence {
	var ev Evidence
	// 1/6 сценариев: ok, sni, reset, throttle, whitelist, нет сети — плюс шум внутри.
	switch r.Intn(6) {
	case 0: // открывается: полоса соизмерима на обоих окнах
		ev.TCPOK = true
		ev.TCPms = 5 + r.Intn(120)
		ev.DirectTLSOk = true
		ev.SplitTLSOk = true
		base := logRate(r, 1, 500)
		fillRates(&ev, base, base*(0.6+0.4*r.Float64()))
	case 1: // SNI: целиком рвётся, по частям проходит
		ev.TCPOK = true
		ev.TCPms = 5 + r.Intn(150)
		ev.DirectTLSOk = r.Intn(10) == 0 // 10% шума
		ev.SplitTLSOk = true
		ev.RSTms = 20 + r.Intn(500)
	case 2: // обрыв всегда
		ev.TCPOK = true
		ev.TCPms = 5 + r.Intn(150)
		ev.DirectTLSOk = false
		ev.SplitTLSOk = r.Intn(12) == 0
		ev.RSTms = 10 + r.Intn(900)
	case 3: // шейпинг: мелкое быстро, крупное медленно
		ev.TCPOK = true
		ev.TCPms = 5 + r.Intn(150)
		ev.DirectTLSOk = true
		ev.SplitTLSOk = true
		base := logRate(r, 1, 200)
		fillRates(&ev, base, base*(0.02+0.28*r.Float64()))
	case 4: // белый список
		ev.TCPOK = r.Intn(2) == 0
		ev.RUOK = boolPtr(true)
		ev.ForeignOK = boolPtr(false)
	case 5: // связи нет
		ev.TCPOK = false
		ev.DirectErr = "таймаут"
		ev.RUOK = boolPtr(r.Intn(2) == 0)
		ev.ForeignOK = boolPtr(false)
	}
	// В реальных пробах скорость регулярно неизмерима: на быстрых каналах чтение
	// укладывается в разрешение таймера, и rate остаётся нулевым. Без этих случаев
	// модель уверенно выдумывает шейпинг там, где его никто не мерил.
	if r.Intn(4) == 0 {
		switch r.Intn(3) {
		case 0:
			ev.BulkBytes, ev.BulkUs = 0, 0
		case 1:
			ev.SmallBytes, ev.SmallUs = 0, 0
		default:
			ev.BulkBytes, ev.BulkUs, ev.SmallBytes, ev.SmallUs = 0, 0, 0, 0
		}
	}
	return ev
}

func boolPtr(b bool) *bool { return &b }

// logRate — лог-равномерная скорость, Мбит/с: разные каналы от 3G до гигабита.
func logRate(r *rand.Rand, minMbps, maxMbps float64) float64 {
	l := math.Log(minMbps) + r.Float64()*(math.Log(maxMbps)-math.Log(minMbps))
	return math.Exp(l)
}

// fillRates переводит скорости в байты и микросекунды (us = байты*8/Мбит/с).
func fillRates(ev *Evidence, smallMbps, bulkMbps float64) {
	ev.SmallBytes, ev.BulkBytes = 8192, 262144
	ev.SmallUs = float64(ev.SmallBytes) * 8 / smallMbps
	ev.BulkUs = float64(ev.BulkBytes) * 8 / bulkMbps
}
