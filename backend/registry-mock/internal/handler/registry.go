package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"registry-mock/internal/model"
	"registry-mock/internal/service"
)

type CompanyService interface {
	GetCompany(ctx context.Context, inn string) (model.Company, error)
}

type RegistryHandler struct {
	service CompanyService
}

func NewRegistryHandler(service CompanyService) *RegistryHandler {
	return &RegistryHandler{service: service}
}

func (h *RegistryHandler) Routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", h.Health)
	mux.HandleFunc("/v1/companies/{inn}", h.GetCompany)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Route not found")
	})
	return mux
}

func (h *RegistryHandler) Health(w http.ResponseWriter, r *http.Request) {
	if !allowGET(w, r) {
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Status string `json:"status"`
	}{Status: "ok"})
}

func (h *RegistryHandler) GetCompany(w http.ResponseWriter, r *http.Request) {
	if !allowGET(w, r) {
		return
	}
	company, err := h.service.GetCompany(r.Context(), r.PathValue("inn"))
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidINN):
			writeError(w, http.StatusBadRequest, "INVALID_INN", "INN must contain 10 or 12 digits")
		case errors.Is(err, model.ErrCompanyNotFound):
			writeError(w, http.StatusNotFound, "COMPANY_NOT_FOUND", "Company not found")
		default:
			log.Printf("company lookup failed: %v", err)
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Internal server error")
		}
		return
	}
	writeJSON(w, http.StatusOK, newCompanyResponse(company))
}

type companyResponse struct {
	INN            string                `json:"inn"`
	OGRN           string                `json:"ogrn"`
	KPP            string                `json:"kpp,omitempty"`
	EntityType     string                `json:"entity_type"`
	Source         string                `json:"source"`
	Name           model.Name            `json:"name"`
	Registration   model.Registration    `json:"registration"`
	Status         model.Status          `json:"status"`
	Capital        *model.Capital        `json:"capital,omitempty"`
	Activities     model.Activities      `json:"activities"`
	Reorganization *model.Reorganization `json:"reorganization,omitempty"`
}

func newCompanyResponse(company model.Company) companyResponse {
	return companyResponse{
		INN:            company.INN,
		OGRN:           company.OGRN,
		KPP:            company.KPP,
		EntityType:     company.EntityType,
		Source:         company.Source,
		Name:           company.Name,
		Registration:   company.Registration,
		Status:         company.Status,
		Capital:        company.Capital,
		Activities:     company.Activities,
		Reorganization: company.Reorganization,
	}
}

type errorResponse struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorResponse{Error: errorDetail{Code: code, Message: message}})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.Printf("write JSON response: %v", err)
	}
}

func allowGET(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
		return false
	}
	return true
}
