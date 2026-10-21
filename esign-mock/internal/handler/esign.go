package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"slices"

	"esign-mock/internal/model"
	"esign-mock/internal/service"
)

// maxBodySize — предел тела запроса: данные плюс JSON-обёртка.
const maxBodySize = 2 * service.MaxDataSize

type ESignService interface {
	ListCertificates(ctx context.Context) ([]model.Certificate, error)
	Sign(ctx context.Context, serial, pin, data string) (model.Signature, error)
	Verify(ctx context.Context, data, signature string) (model.Verification, error)
}

type ESignHandler struct {
	service        ESignService
	logger         *slog.Logger
	allowedOrigins []string
}

// NewESignHandler: allowedOrigins — источники браузерных запросов для CORS,
// "*" разрешает любой.
func NewESignHandler(service ESignService, logger *slog.Logger, allowedOrigins []string) *ESignHandler {
	return &ESignHandler{service: service, logger: logger, allowedOrigins: allowedOrigins}
}

func (h *ESignHandler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", h.Health)
	mux.HandleFunc("/v1/certificates", h.ListCertificates)
	mux.HandleFunc("/v1/sign", h.Sign)
	mux.HandleFunc("/v1/verify", h.Verify)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		h.writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Route not found")
	})
	return h.cors(mux)
}

func (h *ESignHandler) Health(w http.ResponseWriter, r *http.Request) {
	if !h.allowMethod(w, r, http.MethodGet) {
		return
	}
	h.writeJSON(w, r, http.StatusOK, struct {
		Status string `json:"status"`
	}{Status: "ok"})
}

// ListCertificates — сертификаты, «установленные на компьютере» пользователя.
func (h *ESignHandler) ListCertificates(w http.ResponseWriter, r *http.Request) {
	if !h.allowMethod(w, r, http.MethodGet) {
		return
	}
	certificates, err := h.service.ListCertificates(r.Context())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.writeJSON(w, r, http.StatusOK, struct {
		Certificates []model.Certificate `json:"certificates"`
	}{Certificates: certificates})
}

type signRequest struct {
	CertificateSerial string `json:"certificate_serial"`
	PIN               string `json:"pin"`
	Data              string `json:"data"`
}

func (h *ESignHandler) Sign(w http.ResponseWriter, r *http.Request) {
	if !h.allowMethod(w, r, http.MethodPost) {
		return
	}
	var req signRequest
	if !h.decode(w, r, &req) {
		return
	}
	signature, err := h.service.Sign(r.Context(), req.CertificateSerial, req.PIN, req.Data)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.writeJSON(w, r, http.StatusOK, signature)
}

type verifyRequest struct {
	Data      string `json:"data"`
	Signature string `json:"signature"`
}

func (h *ESignHandler) Verify(w http.ResponseWriter, r *http.Request) {
	if !h.allowMethod(w, r, http.MethodPost) {
		return
	}
	var req verifyRequest
	if !h.decode(w, r, &req) {
		return
	}
	verification, err := h.service.Verify(r.Context(), req.Data, req.Signature)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.writeJSON(w, r, http.StatusOK, verification)
}

// fail переводит ошибку сервиса в HTTP-ответ. Ожидаемые ошибки не логируются.
func (h *ESignHandler) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, service.ErrInvalidRequest):
		h.writeError(w, r, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
	case errors.Is(err, service.ErrMalformedSignature):
		h.writeError(w, r, http.StatusBadRequest, "MALFORMED_SIGNATURE", "Signature is malformed")
	case errors.Is(err, model.ErrCertificateNotFound):
		h.writeError(w, r, http.StatusNotFound, "CERTIFICATE_NOT_FOUND", "Certificate not found")
	case errors.Is(err, service.ErrInvalidPIN):
		h.writeError(w, r, http.StatusForbidden, "INVALID_PIN", "Invalid PIN")
	case errors.Is(err, service.ErrCertificateRevoked):
		h.writeError(w, r, http.StatusUnprocessableEntity, "CERTIFICATE_REVOKED", "Certificate is revoked")
	case errors.Is(err, service.ErrCertificateExpired):
		h.writeError(w, r, http.StatusUnprocessableEntity, "CERTIFICATE_EXPIRED", "Certificate is not valid at signing time")
	default:
		h.logger.ErrorContext(r.Context(), "esign request failed", "error", err, "path", r.URL.Path)
		h.writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Internal server error")
	}
}

func (h *ESignHandler) decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		h.writeError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "Request body must be a JSON object")
		return false
	}
	return true
}

// cors разрешает браузерные запросы фронта: подписывает пользователь у себя,
// PIN не проходит через бекенд банка.
func (h *ESignHandler) cors(next http.Handler) http.Handler {
	anyOrigin := slices.Contains(h.allowedOrigins, "*")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		switch {
		case origin == "":
		case anyOrigin:
			w.Header().Set("Access-Control-Allow-Origin", "*")
		case slices.Contains(h.allowedOrigins, origin):
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Add("Vary", "Origin")
		}
		if r.Method == http.MethodOptions && origin != "" {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.Header().Set("Access-Control-Max-Age", "600")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type errorResponse struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (h *ESignHandler) writeError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	h.writeJSON(w, r, status, errorResponse{Error: errorDetail{Code: code, Message: message}})
}

func (h *ESignHandler) writeJSON(w http.ResponseWriter, r *http.Request, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		h.logger.ErrorContext(r.Context(), "write JSON response", "error", err, "path", r.URL.Path)
	}
}

func (h *ESignHandler) allowMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method != method {
		w.Header().Set("Allow", method)
		h.writeError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
		return false
	}
	return true
}
