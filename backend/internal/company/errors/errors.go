package companyerrors

import "errors"

var (
	ErrNotInRegistry = errors.New("company not found in registry")
	// ErrCompanyNotFound — компании нет или пользователь не её участник.
	// Не различаются, чтобы не раскрывать, подключена ли чужая компания.
	ErrCompanyNotFound    = errors.New("company not found")
	ErrChallengeNotFound  = errors.New("challenge not found or expired")
	ErrNotCompanyHead     = errors.New("certificate does not belong to company head")
	ErrNotSigner          = errors.New("only company head can manage members")
	ErrUserNotFound       = errors.New("client not found")
	ErrAlreadyMember      = errors.New("user is already a member")
	ErrMemberNotFound     = errors.New("member not found")
	ErrCannotRemoveSigner = errors.New("company head cannot be removed")

	ErrRegistryUnavailable = errors.New("registry unavailable")
	ErrESignUnavailable    = errors.New("esign unavailable")
)

// ValidationError — некорректное значение поля во входных данных.
// Message предназначен для показа пользователю.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Field + ": " + e.Message
}

// SignatureError — подпись не прошла проверку в УЦ. Reason — причина от
// esign-mock: signature_mismatch, unknown_certificate, certificate_revoked,
// certificate_expired или malformed.
type SignatureError struct {
	Reason string
}

func (e *SignatureError) Error() string {
	return "signature rejected: " + e.Reason
}
