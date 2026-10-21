package service

import (
	"errors"

	autherrors "github.com/lasagna67/lasagna.git/internal/auth/errors"
	"golang.org/x/crypto/bcrypt"
)

const (
	// bcryptCost — сотни миллисекунд на хеш: дорого для перебора, терпимо для входа.
	bcryptCost = 12
	// maxPasswordLen — предел bcrypt, в байтах.
	maxPasswordLen = 72
)

type BcryptHasher struct {
	cost int
}

func NewBcryptHasher() *BcryptHasher {
	return &BcryptHasher{cost: bcryptCost}
}

func (h *BcryptHasher) Hash(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), h.cost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

func (h *BcryptHasher) Compare(hash, password string) error {
	// bcrypt учитывает только первые 72 байта: без этой проверки подошёл бы
	// любой пароль с правильным началом.
	if len(password) > maxPasswordLen {
		return autherrors.ErrInvalidCredentials
	}

	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
		return autherrors.ErrInvalidCredentials
	}
	return err
}
