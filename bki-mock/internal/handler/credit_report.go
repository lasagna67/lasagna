package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"bki-mock/internal/model"
	"bki-mock/internal/service"
)

type CreditReportService interface {
	GetCreditReport(ctx context.Context, inn string) (model.CreditReport, error)
}

type BKIHandler struct {
	service CreditReportService
	logger  *slog.Logger
}

// NewBKIHandler получает бизнес-логику и общий логгер через конструктор.
func NewBKIHandler(service CreditReportService, logger *slog.Logger) *BKIHandler {
	return &BKIHandler{service: service, logger: logger}
}

// Routes регистрирует API кредитных отчётов и единый JSON для неизвестных маршрутов.
func (h *BKIHandler) Routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", h.Health)
	mux.HandleFunc("/v1/credit-reports/{inn}", h.GetCreditReport)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		h.writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Route not found")
	})
	return mux
}

// Health сообщает о доступности HTTP-сервиса.
func (h *BKIHandler) Health(w http.ResponseWriter, r *http.Request) {
	if !h.allowMethod(w, r, http.MethodGet) {
		return
	}
	h.writeJSON(w, r, http.StatusOK, struct {
		Status string `json:"status"`
	}{Status: "ok"})
}

// GetCreditReport получает ИНН из пути и преобразует результат Service в HTTP-ответ.
func (h *BKIHandler) GetCreditReport(w http.ResponseWriter, r *http.Request) {
	if !h.allowMethod(w, r, http.MethodGet) {
		return
	}
	inn := r.PathValue("inn")
	report, err := h.service.GetCreditReport(r.Context(), inn)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidINN):
			h.writeError(w, r, http.StatusBadRequest, "INVALID_INN", "Invalid INN")
		case errors.Is(err, model.ErrCreditHistoryNotFound):
			h.writeError(w, r, http.StatusNotFound, "CREDIT_HISTORY_NOT_FOUND", "Credit history not found")
		default:
			h.logger.ErrorContext(r.Context(), "credit report lookup failed", "error", err, "inn", inn)
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
func (h *BKIHandler) writeError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	h.writeJSON(w, r, status, errorResponse{Error: errorDetail{Code: code, Message: message}})
}

// writeJSON отправляет JSON и записывает ошибку соединения в общий логгер.
func (h *BKIHandler) writeJSON(w http.ResponseWriter, r *http.Request, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		h.logger.ErrorContext(r.Context(), "write JSON response", "error", err, "path", r.URL.Path)
	}
}

// allowMethod ограничивает HTTP-метод и возвращает Allow при отказе.
func (h *BKIHandler) allowMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method != method {
		w.Header().Set("Allow", method)
		h.writeError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
		return false
	}
	return true
}
