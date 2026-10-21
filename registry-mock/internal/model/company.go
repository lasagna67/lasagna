package model

import "errors"

var ErrCompanyNotFound = errors.New("company not found")

type Company struct {
	INN    string `json:"inn"`
	Status string `json:"status"`
	Report Report `json:"report"`
}

type Report struct {
	BusinessType     string     `json:"business_type"`
	Name             string     `json:"name"`
	RegistrationDate string     `json:"registration_date"`
	BusinessStatus   string     `json:"business_status"`
	TerminationDate  *string    `json:"termination_date,omitempty"`
	Head             *Head      `json:"head,omitempty"`
	OKVED            []Activity `json:"okved"`
}

// Head — лицо, имеющее право без доверенности действовать от имени ЮЛ.
// Есть только в ЕГРЮЛ: у ИП руководитель — сам предприниматель.
type Head struct {
	FullName string `json:"full_name"`
	Position string `json:"position"`
	INN      string `json:"inn"`
}

type Activity struct {
	Code string `json:"code"`
	Name string `json:"name"`
}
