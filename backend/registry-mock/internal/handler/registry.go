package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"registry-mock/internal/model"
	"registry-mock/internal/service"
)

type CompanyService interface {
	GetCompany(ctx context.Context, inn string) (model.Company, error)
}

type RegistryHandler struct {
	service CompanyService
	logger  *slog.Logger
}

func NewRegistryHandler(service CompanyService, logger *slog.Logger) *RegistryHandler {
	return &RegistryHandler{service: service, logger: logger}
}

func (h *RegistryHandler) Routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", h.Health)
	mux.HandleFunc("/v1/companies/{inn}", h.GetCompany)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		h.writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Route not found")
	})
	return mux
}

func (h *RegistryHandler) Health(w http.ResponseWriter, r *http.Request) {
	if !h.allowGET(w, r) {
		return
	}
	h.writeJSON(w, r, http.StatusOK, struct {
		Status string `json:"status"`
	}{Status: "ok"})
}

func (h *RegistryHandler) GetCompany(w http.ResponseWriter, r *http.Request) {
	if !h.allowGET(w, r) {
		return
	}
	company, err := h.service.GetCompany(r.Context(), r.PathValue("inn"))
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidINN):
			h.writeError(w, r, http.StatusBadRequest, "INVALID_INN", "INN must contain 10 or 12 digits")
		case errors.Is(err, model.ErrCompanyNotFound):
			h.writeError(w, r, http.StatusNotFound, "COMPANY_NOT_FOUND", "Company not found")
		default:
			h.logger.ErrorContext(r.Context(), "company lookup failed", "error", err, "inn", r.PathValue("inn"))
			h.writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Internal server error")
		}
		return
	}
	h.writeJSON(w, r, http.StatusOK, newCompanyResponse(company))
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

func (h *RegistryHandler) writeError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	h.writeJSON(w, r, status, errorResponse{Error: errorDetail{Code: code, Message: message}})
}

func (h *RegistryHandler) writeJSON(w http.ResponseWriter, r *http.Request, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		h.logger.ErrorContext(r.Context(), "write JSON response", "error", err, "path", r.URL.Path)
	}
}

func (h *RegistryHandler) allowGET(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		h.writeError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
		return false
	}
	return true
}
