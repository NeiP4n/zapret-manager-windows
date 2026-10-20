package evade

import (
	"strings"
	"testing"
)

func TestFeaturesOfKnown(t *testing.T) {
	ev := Evidence{
		TCPOK: true, TCPms: 1000,
		DirectTLSOk: false, SplitTLSOk: true,
		SmallBytes: 8192, SmallUs: 16384, // 8192*8/16384 = 4 Мбит/с
		BulkBytes: 262144, BulkUs: 262144, // 8 Мбит/с
		RUOK: bp(true), ForeignOK: bp(false),
	}
	sp := StrategyParams{
		Desync: "fake,multisplit", SplitPos: "sniext+1", Fooling: "badseq,ts",
		TTL: 6, Repeats: 8, SeqOvl: 681,
	}
	f := FeaturesOf(ev, sp)

	if f[0] != 1 || f[2] != 0 || f[3] != 1 {
		t.Fatalf("флаги TCP/TLS неверны: %v %v %v", f[0], f[2], f[3])
	}
	if f[1] != 0.5 { // 1000 мс из 2000
		t.Fatalf("tcp_ms нормирован как %v", f[1])
	}
	if f[8] != 1 || f[9] != 0 {
		t.Fatalf("белый список: ru=%v foreign=%v", f[8], f[9])
	}
	if f[10] != 1 || f[11] != 6.0/32 {
		t.Fatalf("ttl признаки: %v %v", f[10], f[11])
	}
	if f[13] != 1 || f[14] != 1 || f[15] != 0 {
		t.Fatalf("fooling признаки: %v %v %v", f[13], f[14], f[15])
	}
	if f[18] != 1 || f[19] != 1 {
		t.Fatalf("desync признаки: %v %v", f[18], f[19])
	}
	if f[7] <= 0 { // bulk/small = 2 → 1.0
		t.Fatalf("отношение скоростей %v", f[7])
	}
}

func TestFeaturesUndefinedAreNegative(t *testing.T) {
	// Отсутствие детекта белых списков должно отличаться от «адрес недоступен».
	f := FeaturesOf(Evidence{}, StrategyParams{})
	if f[8] != -1 || f[9] != -1 {
		t.Fatalf("неизвестное состояние должно быть -1, получено %v %v", f[8], f[9])
	}
}

func TestParamsFromBody(t *testing.T) {
	body := []string{
		"#zm0a0b0c",
		"--filter-tcp=443",
		"--dpi-desync=fake,multisplit",
		"--dpi-desync-split-pos=2",
		"--dpi-desync-fooling=ts,badseq",
		"--dpi-desync-ttl=7",
		"--dpi-desync-repeats=9",
		"--dpi-desync-split-seqovl=654",
	}
	p := ParamsFromBody(body)
	if p.Desync != "fake,multisplit" || p.SplitPos != "2" || p.TTL != 7 ||
		p.Repeats != 9 || p.SeqOvl != 654 || !strings.Contains(p.Fooling, "ts") {
		t.Fatalf("разбор тела: %+v", p)
	}
}

func TestModelTrainsRules(t *testing.T) {
	m := NewModel(3)
	train := SynthSamples(8000, 21)
	acc := m.Train(train, 60, 0.08)
	if acc < 0.9 {
		t.Fatalf("точность на обучении %.3f < 0.9", acc)
	}
	holdout := SynthSamples(1000, 22)
	if a := m.Accuracy(holdout); a < 0.9 {
		t.Fatalf("точность на отложенной выборке %.3f < 0.9", a)
	}
}

func TestDefaultModelAgreesWithRules(t *testing.T) {
	m := DefaultModel()
	if m == nil {
		t.Fatal("встроенная модель не загрузилась")
	}
	if m.Meta.Trained == 0 || m.Meta.Source == "" {
		t.Fatalf("нет метаданных обучения: %+v", m.Meta)
	}
	holdout := SynthSamples(2000, 4242)
	acc := m.Accuracy(holdout)
	if acc < 0.9 {
		t.Fatalf("встроенная модель согласна с правилами только на %.3f", acc)
	}
	t.Logf("модель: %d примеров, точность train=%.4f, holdout=%.4f", m.Meta.Trained, m.Meta.Accuracy, acc)
}

func TestModelProbabilitiesAreValid(t *testing.T) {
	m := DefaultModel()
	for i := 0; i < 500; i++ {
		f := SynthSamples(1, int64(i))[0].F
		p := m.Probabilities(f)
		var sum float32
		for _, v := range p {
			if v < 0 || v > 1 {
				t.Fatalf("вероятность вне [0,1]: %v", v)
			}
			sum += v
		}
		if sum < 0.999 || sum > 1.001 {
			t.Fatalf("сумма вероятностей %v", sum)
		}
	}
}

func TestModelPredictNoAllocs(t *testing.T) {
	m := DefaultModel()
	f := SynthSamples(1, 9)[0].F
	if allocs := testing.AllocsPerRun(200, func() { m.Predict(f) }); allocs != 0 {
		t.Fatalf("Predict аллоцирует: %.1f аллокаций на вызов", allocs)
	}
}

func BenchmarkModelForward(b *testing.B) {
	m := DefaultModel()
	f := SynthSamples(1, 9)[0].F
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.Forward(f)
	}
}

func BenchmarkModelPredict(b *testing.B) {
	m := DefaultModel()
	f := SynthSamples(1, 9)[0].F
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = m.Predict(f)
	}
}
