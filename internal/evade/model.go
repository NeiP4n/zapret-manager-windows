package evade

// Своя нейромодель для контура управления (L1/L3 в docs/adaptive-core.md).
//
// Что это: крошечный полносвязный классификатор 24→16→3 (432 умножения на проход),
// без зависимостей, с фиксированными массивами — Forward не аллоцирует память.
// Что он делает: по замерам пробы и параметрам стратегии предсказывает класс блокировки
// (ok / нужен desync / нужен туннель) и уверенность. Один вызов на соединение-хост,
// НЕ на пакет: в пакетном пути нейросеть при 100 тыс. пакетов/с съела бы ядро.
//
// Откуда веса: обучение идёт на размеченных наблюдениях. Сейчас разметка — это
// distillation правил Classify (модель учится правилам и обобщает их без ветвлений);
// когда в state\ появятся реальные результаты тестов стратегий, тот же Trainer
// переобучается на исходах, и модель начинает предсказывать не «класс блокировки»,
// а «сработает ли эта стратегия на этом хосте».

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"strconv"
	"strings"
)

const (
	NumFeatures = 26
	HiddenUnits = 16
	NumClasses  = 3
)

// Классы модели: 0 — можно напрямую, 1 — нужен desync/split, 2 — нужен туннель.
const (
	LabelDirect = 0
	LabelDesync = 1
	LabelTunnel = 2
)

var labelClasses = [NumClasses]Class{ClassOK, ClassSNI, ClassThrottle}

// numFeatureNames документирует порядок вектора: менять его без переобучения нельзя.
var numFeatureNames = [NumFeatures]string{
	"tcp_ok", "tcp_ms", "tls_direct_ok", "tls_split_ok", "rst_ms",
	"small_mbps_log", "bulk_mbps_log", "bulk_over_small", "ru_ok", "foreign_ok",
	"has_ttl", "ttl", "split_pos", "fooling_ts", "fooling_badseq",
	"fooling_badsum", "repeats", "seqovl", "desync_fake", "desync_multisplit",
	"desync_split2", "desync_disorder", "has_desync", "autottl",
	"small_measured", "bulk_measured",
}

// ClassOf переводит выход модели в привычный Class.
func ClassOf(label int) Class {
	if label < 0 || label >= NumClasses {
		return ClassUnknown
	}
	return labelClasses[label]
}

// LabelOf — целевая метка для обучения из класса блокировки.
func LabelOf(c Class) int {
	switch c {
	case ClassOK:
		return LabelDirect
	case ClassSNI, ClassReset:
		return LabelDesync
	default: // throttle, whitelist, unknown
		return LabelTunnel
	}
}

// StrategyParams — то, что модель знает о выбранной стратегии (из тела strategy.txt).
type StrategyParams struct {
	Desync   string `json:"desync"`
	SplitPos string `json:"split_pos"`
	Fooling  string `json:"fooling"`
	TTL      int    `json:"ttl"`
	Repeats  int    `json:"repeats"`
	SeqOvl   int    `json:"seqovl"`
	AutoTTL  bool   `json:"autottl"`
}

// ParamsFromBody разбирает сгенерированное/сохранённое тело стратегии в параметры модели.
func ParamsFromBody(lines []string) StrategyParams {
	var p StrategyParams
	for _, raw := range lines {
		l := strings.TrimSpace(raw)
		k, v, ok := strings.Cut(strings.TrimPrefix(l, "--"), "=")
		if !ok {
			continue
		}
		switch k {
		case "dpi-desync":
			p.Desync = v
		case "dpi-desync-split-pos":
			p.SplitPos = v
		case "dpi-desync-fooling":
			p.Fooling = v
		case "dpi-desync-ttl":
			p.TTL, _ = strconv.Atoi(v)
		case "dpi-desync-repeats":
			p.Repeats, _ = strconv.Atoi(v)
		case "dpi-desync-split-seqovl":
			p.SeqOvl, _ = strconv.Atoi(v)
		case "dpi-desync-autottl":
			p.AutoTTL = true
		}
	}
	return p
}

// Features — вектор признаков. Порядок фиксирован и не должен меняться: веса обучены под него.
type Features [NumFeatures]float32

func clamp01(x float32) float32 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}

func boolF(b bool) float32 {
	if b {
		return 1
	}
	return 0
}

func triState(p *bool) float32 {
	switch {
	case p == nil:
		return -1
	case *p:
		return 1
	default:
		return 0
	}
}

func splitPosValue(s string) float32 {
	switch s {
	case "midsld":
		return 0.25
	case "sniext":
		return 0.30
	case "sniext+1":
		return 0.35
	case "host+1":
		return 0.40
	case "host+2":
		return 0.45
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0
	}
	return clamp01(float32(n) / 32)
}

// FeaturesOf собирает признаки из замера и параметров стратегии.
func FeaturesOf(ev Evidence, sp StrategyParams) Features {
	var f Features
	f[0] = boolF(ev.TCPOK)
	f[1] = clamp01(float32(ev.TCPms) / 2000)
	f[2] = boolF(ev.DirectTLSOk)
	f[3] = boolF(ev.SplitTLSOk)
	f[4] = clamp01(float32(ev.RSTms) / 3000)

	small, bulk := ev.SmallMbps(), ev.BulkMbps()
	// Скорости — по логарифму: линейная шкала схлопывает всё быстрее 200 Мбит/с в 1.0,
	// а решение о шейпинге — это отношение, а не абсолютная полоса.
	f[5] = logScale(small)
	f[6] = logScale(bulk)
	if small > 0 && bulk > 0 {
		f[7] = clamp01(float32(bulk/small) / 2)
	}
	f[8] = triState(ev.RUOK)
	f[9] = triState(ev.ForeignOK)

	f[10] = boolF(sp.TTL > 0 || sp.AutoTTL)
	f[11] = clamp01(float32(sp.TTL) / 32)
	f[12] = splitPosValue(sp.SplitPos)
	f[13] = boolF(strings.Contains(sp.Fooling, "ts"))
	f[14] = boolF(strings.Contains(sp.Fooling, "badseq"))
	f[15] = boolF(strings.Contains(sp.Fooling, "badsum"))
	f[16] = clamp01(float32(sp.Repeats) / 32)
	f[17] = clamp01(float32(sp.SeqOvl) / 2000)
	f[18] = boolF(strings.Contains(sp.Desync, "fake") && !strings.Contains(sp.Desync, "faked"))
	f[19] = boolF(strings.Contains(sp.Desync, "multisplit"))
	f[20] = boolF(strings.Contains(sp.Desync, "split2"))
	f[21] = boolF(strings.Contains(sp.Desync, "disorder"))
	f[22] = boolF(sp.Desync != "")
	f[23] = boolF(sp.AutoTTL)
	// Замер считался, только если есть и байты, и время: иначе скорость неизвестна,
	// и модель не должна домысливать шейпинг (это ровно та ошибка, что поймал тест API).
	f[24] = boolF(ev.SmallBytes > 0 && ev.SmallUs > 0)
	f[25] = boolF(ev.BulkBytes > 0 && ev.BulkUs > 0)
	return f
}

// logScale переводит Мбит/с в 0..1 по порядку величины (0 Мбит/с → 0, 1000 → 1).
func logScale(mbps float64) float32 {
	if mbps <= 0 {
		return 0
	}
	return clamp01(float32(math.Log10(1+mbps) / 3))
}

// Model — веса. Фиксированные массивы: сериализуются в JSON как есть и не требуют
// аллокаций при выводе.
type Model struct {
	W1 [HiddenUnits][NumFeatures]float32 `json:"w1"`
	B1 [HiddenUnits]float32              `json:"b1"`
	W2 [NumClasses][HiddenUnits]float32  `json:"w2"`
	B2 [NumClasses]float32               `json:"b2"`
	// Meta — как модель получена: воспроизводимость важнее красоты.
	Meta ModelMeta `json:"meta"`
}

// ModelMeta описывает происхождение весов.
type ModelMeta struct {
	Version  int     `json:"version"`
	Trained  int     `json:"trained_samples"`
	Epochs   int     `json:"epochs"`
	Accuracy float64 `json:"train_accuracy"`
	Source   string  `json:"source"`
}

// NewModel создаёт модель со случайными весами (детерминированно по seed).
func NewModel(seed int64) *Model {
	r := rand.New(rand.NewSource(seed))
	m := &Model{}
	scale := func(fan int) float32 { return float32(1.0 / math.Sqrt(float64(fan))) }
	s1, s2 := scale(NumFeatures), scale(HiddenUnits)
	for j := 0; j < HiddenUnits; j++ {
		for i := 0; i < NumFeatures; i++ {
			m.W1[j][i] = (r.Float32()*2 - 1) * s1
		}
	}
	for c := 0; c < NumClasses; c++ {
		for j := 0; j < HiddenUnits; j++ {
			m.W2[c][j] = (r.Float32()*2 - 1) * s2
		}
	}
	return m
}

// Forward возвращает логиты. Без аллокаций: всё на стеке.
func (m *Model) Forward(f Features) [NumClasses]float32 {
	var h [HiddenUnits]float32
	for j := 0; j < HiddenUnits; j++ {
		var s float32
		for i := 0; i < NumFeatures; i++ {
			s += m.W1[j][i] * f[i]
		}
		s += m.B1[j]
		if s < 0 {
			s = 0
		}
		h[j] = s
	}
	var out [NumClasses]float32
	for c := 0; c < NumClasses; c++ {
		var s float32
		for j := 0; j < HiddenUnits; j++ {
			s += m.W2[c][j] * h[j]
		}
		out[c] = s + m.B2[c]
	}
	return out
}

// softmax с защитой от переполнения и NaN: расходящиеся веса не должны превращать
// ответ в мусор — в этом случае модель честно говорит «не знаю».
func softmax(logits [NumClasses]float32) [NumClasses]float32 {
	mx := logits[0]
	for _, v := range logits {
		if v > mx {
			mx = v
		}
	}
	var p [NumClasses]float32
	var sum float32
	for i, v := range logits {
		e := math.Exp(float64(v - mx))
		if math.IsNaN(e) || math.IsInf(e, 0) {
			return [NumClasses]float32{1.0 / NumClasses, 1.0 / NumClasses, 1.0 / NumClasses}
		}
		p[i] = float32(e)
		sum += p[i]
	}
	if sum <= 0 || math.IsNaN(float64(sum)) || math.IsInf(float64(sum), 0) {
		return [NumClasses]float32{1.0 / NumClasses, 1.0 / NumClasses, 1.0 / NumClasses}
	}
	for i := range p {
		p[i] /= sum
	}
	return p
}

// Probabilities — softmax от логитов.
func (m *Model) Probabilities(f Features) [NumClasses]float32 {
	return softmax(m.Forward(f))
}

// Predict — метка и уверенность.
func (m *Model) Predict(f Features) (int, float32) {
	p := m.Probabilities(f)
	best, bestP := 0, p[0]
	for i, v := range p {
		if v > bestP {
			best, bestP = i, v
		}
	}
	return best, bestP
}

// Sample — размеченное наблюдение для обучения.
type Sample struct {
	F     Features
	Label int
}

// Train обучает модель SGD с кросс-энтропией; возвращает точность на этой же выборке.
func (m *Model) Train(samples []Sample, epochs int, lr float32) float32 {
	if len(samples) == 0 {
		return 0
	}
	var h [HiddenUnits]float32
	var p [NumClasses]float32
	for e := 0; e < epochs; e++ {
		// Перемешивание на каждой эпохе: без него порядок выборки начинает учить модель.
		r := rand.New(rand.NewSource(int64(e) + 1))
		order := r.Perm(len(samples))
		for _, idx := range order {
			s := samples[idx]
			// forward (вручную, чтобы сохранить h для backprop)
			for j := 0; j < HiddenUnits; j++ {
				var z float32
				for i := 0; i < NumFeatures; i++ {
					z += m.W1[j][i] * s.F[i]
				}
				z += m.B1[j]
				if z < 0 {
					z = 0
				}
				h[j] = z
			}
			var logits [NumClasses]float32
			for c := 0; c < NumClasses; c++ {
				var z float32
				for j := 0; j < HiddenUnits; j++ {
					z += m.W2[c][j] * h[j]
				}
				logits[c] = z + m.B2[c]
			}
			p = softmax(logits)
			// dL/dlogits = p - onehot
			var d [NumClasses]float32
			for c := range d {
				d[c] = p[c]
			}
			d[s.Label] -= 1

			var dh [HiddenUnits]float32
			for c := 0; c < NumClasses; c++ {
				g := clip(d[c]*lr, 0.5)
				for j := 0; j < HiddenUnits; j++ {
					m.W2[c][j] -= g * h[j]
					dh[j] += d[c] * m.W2[c][j]
				}
				m.B2[c] -= g
			}
			for j := 0; j < HiddenUnits; j++ {
				if h[j] <= 0 {
					continue // ReLU не пропускает градиент
				}
				g := clip(dh[j]*lr, 0.5)
				for i := 0; i < NumFeatures; i++ {
					m.W1[j][i] -= g * s.F[i]
				}
				m.B1[j] -= g
			}
		}
	}
	return m.Accuracy(samples)
}

func clip(v, lim float32) float32 {
	if v > lim {
		return lim
	}
	if v < -lim {
		return -lim
	}
	return v
}

// Accuracy — доля верных ответов.
func (m *Model) Accuracy(samples []Sample) float32 {
	if len(samples) == 0 {
		return 0
	}
	var ok int
	for _, s := range samples {
		if label, _ := m.Predict(s.F); label == s.Label {
			ok++
		}
	}
	return float32(ok) / float32(len(samples))
}

// Save и Load — JSON, чтобы веса можно было версионировать и переобучать на месте.
func (m *Model) Save(path string) error {
	b, err := json.MarshalIndent(m, "", " ")
	if err != nil {
		return err
	}
	tmp := path + ".zmtmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func LoadModel(path string) (*Model, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseModel(b)
}

func ParseModel(b []byte) (*Model, error) {
	var m Model
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("веса модели: %v", err)
	}
	return &m, nil
}

// embedWeights заполняется в model_default.go из встроенного JSON. Если файла нет,
// остаётся nil и вызывающий обязан использовать правила (Classify).
var embedWeights []byte

// DefaultModel возвращает встроенную обученную модель или nil, если её нет.
func DefaultModel() *Model {
	if len(embedWeights) == 0 {
		return nil
	}
	m, err := ParseModel(embedWeights)
	if err != nil {
		return nil
	}
	return m
}
