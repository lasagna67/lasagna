package repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lasagna67/lasagna.git/internal/auth/domain"
	autherrors "github.com/lasagna67/lasagna.git/internal/auth/errors"
)

const pgUniqueViolation = "23505"

const userColumns = `id, role, status, email, phone, password_hash,
	first_name, last_name, middle_name, created_at, updated_at`

type UserRepo struct {
	pool *pgxpool.Pool
}

func NewUserRepo(pool *pgxpool.Pool) *UserRepo {
	return &UserRepo{pool: pool}
}

// Create сохраняет пользователя и возвращает его с заполненными id, статусом и датами.
func (r *UserRepo) Create(ctx context.Context, user domain.User) (domain.User, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO users (role, email, phone, password_hash, first_name, last_name, middle_name)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING `+userColumns,
		user.Role, user.Email, user.Phone, user.PasswordHash,
		user.FirstName, user.LastName, user.MiddleName,
	)

	created, err := scanUser(row)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			switch pgErr.ConstraintName {
			case "users_email_uidx":
				return domain.User{}, autherrors.ErrEmailTaken
			case "users_phone_key":
				return domain.User{}, autherrors.ErrPhoneTaken
			}
		}
		return domain.User{}, fmt.Errorf("insert user: %w", err)
	}
	return created, nil
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
