package service

import (
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/lasagna67/lasagna.git/internal/auth/domain"
	autherrors "github.com/lasagna67/lasagna.git/internal/auth/errors"
)

const (
	minPasswordLen = 8
	maxPasswordLen = 72 // предел bcrypt, в байтах
	maxEmailLen    = 255
	maxNameLen     = 100
)

var phonePattern = regexp.MustCompile(`^\+[1-9][0-9]{6,14}$`) // E.164

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// normalizeRegistration приводит поля к каноническому виду и проверяет их.
func normalizeRegistration(in domain.RegisterInput) (domain.RegisterInput, error) {
	in.Email = normalizeEmail(in.Email)
	addr, err := mail.ParseAddress(in.Email)
	if err != nil || addr.Address != in.Email || len(in.Email) > maxEmailLen {
		return in, invalid("email", "Некорректный email")
	}

	if n := len(in.Password); n < minPasswordLen || n > maxPasswordLen {
		return in, invalid("password", fmt.Sprintf("Пароль должен быть от %d до %d символов", minPasswordLen, maxPasswordLen))
	}

	in.Phone = trimOptional(in.Phone)
	if in.Phone != nil && !phonePattern.MatchString(*in.Phone) {
		return in, invalid("phone", "Телефон должен быть в формате +79991234567")
	}

	in.FirstName = strings.TrimSpace(in.FirstName)
	if err := validateName("first_name", "Имя", in.FirstName); err != nil {
		return in, err
	}
	in.LastName = strings.TrimSpace(in.LastName)
	if err := validateName("last_name", "Фамилия", in.LastName); err != nil {
		return in, err
	}
	in.MiddleName = trimOptional(in.MiddleName)
	if in.MiddleName != nil {
		if err := validateName("middle_name", "Отчество", *in.MiddleName); err != nil {
			return in, err
		}
	}

	return in, nil
}

func validateName(field, label, value string) error {
	if value == "" {
		return invalid(field, label+" — обязательное поле")
	}
	if utf8.RuneCountInString(value) > maxNameLen {
		return invalid(field, fmt.Sprintf("%s — не длиннее %d символов", label, maxNameLen))
	}
	return nil
}

// trimOptional обрезает пробелы; пустая строка считается отсутствующим значением.
func trimOptional(s *string) *string {
	if s == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*s)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func invalid(field, message string) error {
	return &autherrors.ValidationError{Field: field, Message: message}
}
