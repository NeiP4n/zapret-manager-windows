package evade

import _ "embed"

// Веса обучены командой `go run ./internal/evade/genmodel`; переобучение на реальных
// исходах тестов пишет файл туда же и меняет только этот JSON.
//
//go:embed model_default.json
var modelDefaultJSON []byte

func init() { embedWeights = modelDefaultJSON }
