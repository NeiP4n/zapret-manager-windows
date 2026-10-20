package zapret

import (
	"strings"
	"testing"
)

func TestRandomBodyDeterministic(t *testing.T) {
	a := strings.Join(RandomBody(42), "\n")
	b := strings.Join(RandomBody(42), "\n")
	if a != b {
		t.Fatalf("один seed дал разные стратегии:\n%s\n---\n%s", a, b)
	}
}

func TestRandomBodyValidates(t *testing.T) {
	for seed := int64(0); seed < 500; seed++ {
		body := RandomBody(seed)
		if err := ValidateBody(body); err != nil {
			t.Fatalf("seed %d: невалидная стратегия: %v\n%s", seed, err, strings.Join(body, "\n"))
		}
	}
}

func TestRandomBodyVariety(t *testing.T) {
	seen := map[string]bool{}
	for seed := int64(0); seed < 200; seed++ {
		seen[strings.Join(RandomBody(seed), "\n")] = true
	}
	// Главное свойство анти-фингерпринта: стратегии не должны повторяться.
	if len(seen) < 150 {
		t.Fatalf("слишком мало разнообразия: %d уникальных из 200", len(seen))
	}
}

func TestRandomBodyEnvelopeRespected(t *testing.T) {
	env := Envelope{
		Desync:    []string{"fake,multisplit"},
		SplitPos:  []string{"2"},
		Fooling:   []string{"ts"},
		SeqOvl:    []int{681},
		TTLMin:    5,
		TTLMax:    5,
		RepeatMin: 8,
		RepeatMax: 8,
		FakeTLS:   FK + "stun.bin",
		FakeMod:   "none",
	}
	for seed := int64(0); seed < 50; seed++ {
		body := RandomBodyEnvelope(seed, env)
		if err := ValidateBody(body); err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
		got := strings.Join(body, "\n")
		for _, want := range []string{
			"--dpi-desync=fake,multisplit",
			"--dpi-desync-split-pos=2",
			"--dpi-desync-fooling=ts",
			"--dpi-desync-ttl=5",
			"--dpi-desync-repeats=8",
			"--dpi-desync-split-seqovl=681",
		} {
			if !strings.Contains(got, want) {
				t.Fatalf("seed %d: нет %q в стратегии:\n%s", seed, want, got)
			}
		}
		if strings.Contains(got, "autottl") {
			t.Fatalf("seed %d: autottl выключен в конверте, но попал в вывод", seed)
		}
	}
}

func TestRandomBodyAutoTTL(t *testing.T) {
	env := DefaultEnvelope()
	env.AutoTTL = true
	body := RandomBodyEnvelope(7, env)
	got := strings.Join(body, "\n")
	if !strings.Contains(got, "--dpi-desync-autottl=") {
		t.Fatalf("ожидался autottl:\n%s", got)
	}
	if strings.Contains(got, "--dpi-desync-ttl=") {
		t.Fatalf("ttl и autottl одновременно:\n%s", got)
	}
	if err := ValidateBody(body); err != nil {
		t.Fatalf("autottl-стратегия не проходит валидацию: %v", err)
	}
}

func TestValidateBodyRejects(t *testing.T) {
	base := []string{"#zm000001", "--filter-tcp=443", "--hostlist-exclude=" + ExcludeList}
	cases := map[string][]string{
		"пусто":              nil,
		"нет маркера":        {"--filter-tcp=443"},
		"неизвестный флаг":   append(append([]string{}, base...), "--dpi-desync-fake-trash=1"),
		"нет значения":       append(append([]string{}, base...), "--dpi-desync"),
		"плохой режим":       append(append([]string{}, base...), "--dpi-desync=teleport"),
		"плохой fooling":     append(append([]string{}, base...), "--dpi-desync-fooling=magic"),
		"ttl вне диапазона":  append(append([]string{}, base...), "--dpi-desync-ttl=99"),
		"repeats вне":        append(append([]string{}, base...), "--dpi-desync-repeats=100"),
		"split-pos вне":      append(append([]string{}, base...), "--dpi-desync-split-pos=999"),
		"seqovl вне":         append(append([]string{}, base...), "--dpi-desync-split-seqovl=99999"),
		"нет фильтра":        {"#zm000001", "--dpi-desync=fake"},
		"мусорная строка":    append(append([]string{}, base...), "dpi-desync=fake"),
		"autottl вне":        append(append([]string{}, base...), "--dpi-desync-autottl=50"),
	}
	for name, lines := range cases {
		if err := ValidateBody(lines); err == nil {
			t.Errorf("%s: ожидалась ошибка, получено nil", name)
		}
	}
}

func TestValidateBodyAcceptsRouterPreset(t *testing.T) {
	// Пресеты роутера должны проходить ту же проверку — иначе она бесполезна.
	for n := 1; n <= 10; n++ {
		if err := ValidateBody(StrategyV(n)); err != nil {
			t.Errorf("v%d не проходит валидацию: %v", n, err)
		}
	}
}

func TestRandomBodyPortablePaths(t *testing.T) {
	body := RandomBodyEnvelope(3, DefaultEnvelope())
	for _, l := range body {
		if strings.Contains(l, "/opt/zapret") || strings.Contains(l, "C:\\") {
			t.Fatalf("путь не портируемый: %q", l)
		}
	}
}
