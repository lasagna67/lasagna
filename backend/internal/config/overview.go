package config

import "time"

type ReportsConfig struct {
	URL     string        `default:"http://bki-mock:8082"`
	Timeout time.Duration `default:"5s"`
}
type FinancialsConfig struct {
	URL     string        `default:"http://financials-mock:8083"`
	Timeout time.Duration `default:"5s"`
	// DemoModes — разрешить demo_mode в financials/fetch: моделировать ответ
	// источника для заявки. Только в изолированном демо-окружении.
	DemoModes bool `default:"false"`
}
