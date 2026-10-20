// Command genmodel обучает встроенную модель контура управления и пишет веса.
//
//	go run ./internal/evade/genmodel            # → internal/evade/model_default.json
//
// Разметка берётся из правил Classify (distillation). Это осознанный компромисс:
// собственная нейросеть нужна не вместо правил, а как быстрый однопроходный
// экстрактор решения, который потом переобучается на реальных исходах тестов.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/zapretmanager/zmwin/internal/evade"
)

func main() {
	out := flag.String("o", "internal/evade/model_default.json", "куда писать веса")
	samples := flag.Int("samples", 20000, "сколько наблюдений сгенерировать")
	epochs := flag.Int("epochs", 60, "эпох обучения")
	lr := flag.Float64("lr", 0.08, "скорость обучения")
	seed := flag.Int64("seed", 7, "seed корпуса")
	mseed := flag.Int64("model-seed", 11, "seed инициализации весов")
	flag.Parse()

	m := evade.NewModel(*mseed)
	train := evade.SynthSamples(*samples, *seed)
	acc := m.Train(train, *epochs, float32(*lr))

	holdout := evade.SynthSamples(*samples/4, *seed+1000)
	m.Meta = evade.ModelMeta{
		Version:  1,
		Trained:  len(train),
		Epochs:   *epochs,
		Accuracy: float64(acc),
		Source:   "distillation: Classify() over SynthSamples",
	}
	if err := m.Save(*out); err != nil {
		fmt.Println("не удалось сохранить веса:", err)
		os.Exit(1)
	}
	fmt.Printf("train=%.4f holdout=%.4f samples=%d → %s\n", acc, m.Accuracy(holdout), len(train), *out)
}
