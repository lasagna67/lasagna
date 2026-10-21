package userservice

import (
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"unicode/utf8"

	userdomain "github.com/lasagna67/lasagna.git/internal/user/domain"
	usererrors "github.com/lasagna67/lasagna.git/internal/user/errors"
)

const (
	minPasswordLen = 8
	maxPasswordLen = 72 // предел bcrypt, в байтах
	maxEmailLen    = 255
	maxNameLen     = 100
)

var phonePattern = regexp.MustCompile(`^\+[1-9][0-9]{6,14}$`) // E.164

// normalizeCreateClient приводит поля к каноническому виду и проверяет их.
func normalizeCreateClient(cmd userdomain.CreateClientCommand) (userdomain.CreateClientCommand, error) {
	cmd.Email = strings.ToLower(strings.TrimSpace(cmd.Email))
	addr, err := mail.ParseAddress(cmd.Email)
	if err != nil || addr.Address != cmd.Email || len(cmd.Email) > maxEmailLen {
		return cmd, invalid("email", "Некорректный email")
	}

	if n := len(cmd.Password); n < minPasswordLen || n > maxPasswordLen {
		return cmd, invalid("password", fmt.Sprintf("Пароль должен быть от %d до %d символов", minPasswordLen, maxPasswordLen))
	}

	cmd.Phone = trimOptional(cmd.Phone)
	if cmd.Phone != nil && !phonePattern.MatchString(*cmd.Phone) {
		return cmd, invalid("phone", "Телефон должен быть в формате +79991234567")
	}

	if cmd.FirstName, err = normalizeName("first_name", "Имя", cmd.FirstName); err != nil {
		return cmd, err
	}
	if cmd.LastName, err = normalizeName("last_name", "Фамилия", cmd.LastName); err != nil {
		return cmd, err
	}
	if cmd.MiddleName, err = normalizeName("middle_name", "Отчество", cmd.MiddleName); err != nil {
		return cmd, err
	}

	return cmd, nil
}

// normalizeName — ФИО необязательны, но если переданы, то не длиннее maxNameLen.
func normalizeName(field, label string, value *string) (*string, error) {
	value = trimOptional(value)
	if value != nil && utf8.RuneCountInString(*value) > maxNameLen {
		return nil, invalid(field, fmt.Sprintf("%s — не длиннее %d символов", label, maxNameLen))
	}
	return value, nil
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
	return &usererrors.ValidationError{Field: field, Message: message}
}
