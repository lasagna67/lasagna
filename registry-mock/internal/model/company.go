package model

import "errors"

var ErrCompanyNotFound = errors.New("company not found")

type Company struct {
	INN            string          `json:"inn"`
	OGRN           string          `json:"ogrn"`
	KPP            string          `json:"kpp,omitempty"`
	EntityType     string          `json:"entity_type"`
	Source         string          `json:"source"`
	Name           Name            `json:"name"`
	Registration   Registration    `json:"registration"`
	Status         Status          `json:"status"`
	Capital        *Capital        `json:"capital,omitempty"`
	Activities     Activities      `json:"activities"`
	Reorganization *Reorganization `json:"reorganization,omitempty"`
	Metadata       Metadata        `json:"metadata"`
}

type Name struct {
	Full  string `json:"full"`
	Short string `json:"short"`
}

type Registration struct {
	Date    string  `json:"date"`
	Region  string  `json:"region"`
	Address *string `json:"address"`
}

type Status struct {
	Code        string `json:"code"`
	Description string `json:"description"`
	Since       string `json:"since"`
}

type Capital struct {
	AmountKopecks int64  `json:"amount_kopecks"`
	Type          string `json:"type"`
}

type Activities struct {
	PrimaryOKVED     string   `json:"primary_okved"`
	AdditionalOKVEDs []string `json:"additional_okveds"`
}

type Reorganization struct {
	Type        string `json:"type"`
	StartDate   string `json:"start_date"`
	Description string `json:"description"`
}

type Metadata struct {
	AsOf         string `json:"as_of"`
	IsSynthetic  bool   `json:"is_synthetic"`
	DemoScenario string `json:"demo_scenario"`
}
