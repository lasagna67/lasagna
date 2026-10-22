package repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lasagna67/lasagna.git/internal/auth/domain"
	autherrors "github.com/lasagna67/lasagna.git/internal/auth/errors"
)

const userColumns = `id, role, status, email, phone, password_hash,
	first_name, last_name, middle_name, created_at, updated_at`

type UserRepo struct {
	pool *pgxpool.Pool
}

func NewUserRepo(pool *pgxpool.Pool) *UserRepo {
	return &UserRepo{pool: pool}
}

func (r *UserRepo) GetByEmail(ctx context.Context, email string) (domain.User, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+userColumns+` FROM users WHERE lower(email) = lower($1)`,
		email,
	)

	user, err := scanUser(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, autherrors.ErrUserNotFound
	}
	if err != nil {
		return domain.User{}, fmt.Errorf("select user by email: %w", err)
	}
	return user, nil
}

func scanUser(row pgx.Row) (domain.User, error) {
	var u domain.User
	err := row.Scan(
		&u.ID, &u.Role, &u.Status, &u.Email, &u.Phone, &u.PasswordHash,
		&u.FirstName, &u.LastName, &u.MiddleName, &u.CreatedAt, &u.UpdatedAt,
	)
	return u, err
}
