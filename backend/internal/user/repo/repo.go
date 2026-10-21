package userrepo

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	userdomain "github.com/lasagna67/lasagna.git/internal/user/domain"
	usererrors "github.com/lasagna67/lasagna.git/internal/user/errors"
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
func (r *UserRepo) Create(ctx context.Context, user userdomain.User) (userdomain.User, error) {
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
				return userdomain.User{}, usererrors.ErrEmailTaken
			case "users_phone_key":
				return userdomain.User{}, usererrors.ErrPhoneTaken
			}
		}
		return userdomain.User{}, fmt.Errorf("insert user: %w", err)
	}
	return created, nil
}

func (r *UserRepo) SetStatus(ctx context.Context, id uuid.UUID, status userdomain.Status) error {
	tag, err := r.pool.Exec(ctx, `UPDATE users SET status = $2 WHERE id = $1`, id, status)
	if err != nil {
		return fmt.Errorf("update user status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return usererrors.ErrUserNotFound
	}
	return nil
}

func scanUser(row pgx.Row) (userdomain.User, error) {
	var u userdomain.User
	err := row.Scan(
		&u.ID, &u.Role, &u.Status, &u.Email, &u.Phone, &u.PasswordHash,
		&u.FirstName, &u.LastName, &u.MiddleName, &u.CreatedAt, &u.UpdatedAt,
	)
	return u, err
}
