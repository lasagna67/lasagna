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
	OKVED            []Activity `json:"okved"`
}

type Activity struct {
	Code string `json:"code"`
	Name string `json:"name"`
}
