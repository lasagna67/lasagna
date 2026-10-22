package service

import (
	"context"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"esign-mock/internal/model"
)

// MaxDataSize — предел подписываемых данных. Подписывают короткие заявления,
// а не файлы целиком: для файла подписывается его хеш.
const MaxDataSize = 64 << 10

var (
	ErrInvalidRequest     = errors.New("invalid request")
	ErrInvalidPIN         = errors.New("invalid PIN")
	ErrCertificateRevoked = errors.New("certificate revoked")
	ErrCertificateExpired = errors.New("certificate expired")
	ErrMalformedSignature = errors.New("malformed signature")
)

type Repository interface {
	List(ctx context.Context) ([]model.StoredCertificate, error)
	Get(ctx context.Context, serial string) (model.StoredCertificate, error)
}

// ESignService имитирует УЦ и криптопровайдер: закрытый ключ сертификата
// «лежит на токене» и доступен только по PIN-коду.
type ESignService struct {
	repository Repository
	keySecret  []byte
	now        func() time.Time
}

func NewESignService(repository Repository, keySecret string) *ESignService {
	return &ESignService{repository: repository, keySecret: []byte(keySecret), now: time.Now}
}

// envelope — подпись вместе с тем, что нужно для её проверки: серийный номер
// сертификата и время подписания. Аналог detached CMS, упрощённый для мока.
type envelope struct {
	Serial   string `json:"serial"`
	SignedAt string `json:"signed_at"`
	Value    []byte `json:"value"`
}

func (s *ESignService) ListCertificates(ctx context.Context) ([]model.Certificate, error) {
	stored, err := s.repository.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list certificates: %w", err)
	}
	certificates := make([]model.Certificate, 0, len(stored))
	for _, c := range stored {
		certificates = append(certificates, c.Certificate)
	}
	return certificates, nil
}

func (s *ESignService) Sign(ctx context.Context, serial, pin, data string) (model.Signature, error) {
	if serial == "" || pin == "" || data == "" {
		return model.Signature{}, fmt.Errorf("certificate_serial, pin and data are required: %w", ErrInvalidRequest)
	}
	if len(data) > MaxDataSize {
		return model.Signature{}, fmt.Errorf("data exceeds %d bytes: %w", MaxDataSize, ErrInvalidRequest)
	}

	certificate, err := s.repository.Get(ctx, serial)
	if err != nil {
		return model.Signature{}, fmt.Errorf("get certificate: %w", err)
	}
	if subtle.ConstantTimeCompare([]byte(certificate.PIN), []byte(pin)) != 1 {
		return model.Signature{}, ErrInvalidPIN
	}

	signedAt := s.now().UTC().Truncate(time.Second)
	if reason := usability(certificate.Certificate, signedAt); reason != "" {
		if reason == model.ReasonCertificateRevoked {
			return model.Signature{}, ErrCertificateRevoked
		}
		return model.Signature{}, ErrCertificateExpired
	}

	env := envelope{Serial: serial, SignedAt: signedAt.Format(time.RFC3339)}
	env.Value = ed25519.Sign(s.privateKey(serial), message(env.SignedAt, data))
	raw, err := json.Marshal(env)
	if err != nil {
		return model.Signature{}, fmt.Errorf("marshal signature: %w", err)
	}

	return model.Signature{
		Signature:   base64.RawURLEncoding.EncodeToString(raw),
		SignedAt:    env.SignedAt,
		Certificate: certificate.Certificate,
	}, nil
}

// Verify проверяет подпись над data. Невалидная подпись — это не ошибка,
// а результат с Valid == false и причиной.
func (s *ESignService) Verify(ctx context.Context, data, signature string) (model.Verification, error) {
	if data == "" || signature == "" {
		return model.Verification{}, fmt.Errorf("data and signature are required: %w", ErrInvalidRequest)
	}
	env, err := decodeEnvelope(signature)
	if err != nil {
		return model.Verification{}, err
	}
	signedAt, err := time.Parse(time.RFC3339, env.SignedAt)
	if err != nil {
		return model.Verification{}, fmt.Errorf("signed_at: %w", ErrMalformedSignature)
	}

	stored, err := s.repository.Get(ctx, env.Serial)
	if errors.Is(err, model.ErrCertificateNotFound) {
		return model.Verification{Valid: false, Reason: model.ReasonUnknownCertificate, SignedAt: env.SignedAt}, nil
	}
	if err != nil {
		return model.Verification{}, fmt.Errorf("get certificate: %w", err)
	}

	result := model.Verification{SignedAt: env.SignedAt, Certificate: &stored.Certificate}
	publicKey := s.privateKey(env.Serial).Public().(ed25519.PublicKey)
	switch {
	case !ed25519.Verify(publicKey, message(env.SignedAt, data), env.Value):
		result.Reason = model.ReasonSignatureMismatch
	default:
		result.Reason = usability(stored.Certificate, signedAt)
	}
	result.Valid = result.Reason == ""
	return result, nil
}

// privateKey выводит ключ сертификата из секрета мока, чтобы подписи
// оставались проверяемыми после перезапуска.
func (s *ESignService) privateKey(serial string) ed25519.PrivateKey {
	mac := hmac.New(sha256.New, s.keySecret)
	mac.Write([]byte(serial))
	return ed25519.NewKeyFromSeed(mac.Sum(nil))
}

// usability — причина, по которой сертификатом нельзя подписывать в момент at,
// или пустая строка. Даты в сертификате — включительно, по UTC.
func usability(c model.Certificate, at time.Time) string {
	if c.Status == model.StatusRevoked {
		return model.ReasonCertificateRevoked
	}
	day := at.UTC().Format(time.DateOnly)
	if day < c.ValidFrom || day > c.ValidTo {
		return model.ReasonCertificateExpired
	}
	return ""
}

// message — подписываемые байты: время подписания связано с данными,
// чтобы его нельзя было подменить в конверте.
func message(signedAt, data string) []byte {
	return []byte(signedAt + "\n" + data)
}

func decodeEnvelope(signature string) (envelope, error) {
	raw, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil {
		return envelope{}, fmt.Errorf("decode base64: %w", ErrMalformedSignature)
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return envelope{}, fmt.Errorf("decode envelope: %w", ErrMalformedSignature)
	}
	if env.Serial == "" || len(env.Value) != ed25519.SignatureSize {
		return envelope{}, fmt.Errorf("incomplete envelope: %w", ErrMalformedSignature)
	}
	return env, nil
}
