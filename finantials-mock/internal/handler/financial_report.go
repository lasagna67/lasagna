package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"financials-mock/internal/model"
	"financials-mock/internal/service"
)

type FinancialReportService interface {
	GetFinancialReport(ctx context.Context, inn string) (model.FinancialReport, error)
}

type FinancialHandler struct {
	service FinancialReportService
	logger  *slog.Logger
}

// NewFinancialHandler получает бизнес-логику и общий логгер через конструктор.
func NewFinancialHandler(service FinancialReportService, logger *slog.Logger) *FinancialHandler {
	return &FinancialHandler{service: service, logger: logger}
}

// Routes регистрирует API финансовых отчётов и единый JSON для неизвестных маршрутов.
func (h *FinancialHandler) Routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", h.Health)
	mux.HandleFunc("/v1/financial-reports/{inn}", h.GetFinancialReport)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		h.writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Route not found")
	})
	return mux
}

// Health сообщает о доступности HTTP-сервиса.
func (h *FinancialHandler) Health(w http.ResponseWriter, r *http.Request) {
	if !h.allowMethod(w, r, http.MethodGet) {
		return
	}
	h.writeJSON(w, r, http.StatusOK, struct {
		Status string `json:"status"`
	}{Status: "ok"})
}

// GetFinancialReport получает ИНН из пути и преобразует результат Service в HTTP-ответ.
func (h *FinancialHandler) GetFinancialReport(w http.ResponseWriter, r *http.Request) {
	if !h.allowMethod(w, r, http.MethodGet) {
		return
	}
	inn := r.PathValue("inn")
	report, err := h.service.GetFinancialReport(r.Context(), inn)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidINN):
			h.writeError(w, r, http.StatusBadRequest, "INVALID_INN", "Invalid INN")
		case errors.Is(err, model.ErrFinancialDataNotFound):
			h.writeError(w, r, http.StatusNotFound, "FINANCIAL_DATA_NOT_FOUND", "Financial data not found")
		default:
			h.logger.ErrorContext(r.Context(), "financial report lookup failed", "error", err, "inn", inn)
			h.writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Internal server error")
		}
		return
	}
	h.writeJSON(w, r, http.StatusOK, report)
}

type errorResponse struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// writeError формирует JSON-ошибку в том же формате, что Registry Mock.
func (h *FinancialHandler) writeError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	h.writeJSON(w, r, status, errorResponse{Error: errorDetail{Code: code, Message: message}})
}

// writeJSON отправляет JSON и записывает ошибку соединения в общий логгер.
func (h *FinancialHandler) writeJSON(w http.ResponseWriter, r *http.Request, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		h.logger.ErrorContext(r.Context(), "write JSON response", "error", err, "path", r.URL.Path)
	}
}

// allowMethod ограничивает HTTP-метод и возвращает Allow при отказе.
func (h *FinancialHandler) allowMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method != method {
		w.Header().Set("Allow", method)
		h.writeError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
		return false
	}
	return true
}
