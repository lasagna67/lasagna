package config

import "time"

type ReportsConfig struct {
	URL     string        `default:"http://bki-mock:8082"`
	Timeout time.Duration `default:"5s"`
}
type FinancialsConfig struct {
	URL     string        `default:"http://financials-mock:8083"`
	Timeout time.Duration `default:"5s"`
}
