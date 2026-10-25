// Package documentrepo — файлы заявок: объекты в S3 и метаданные в
// application_documents.
package documentrepo

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	applicationrepo "github.com/lasagna67/lasagna.git/internal/application/repo"
	documentdomain "github.com/lasagna67/lasagna.git/internal/document/domain"
	documenterrors "github.com/lasagna67/lasagna.git/internal/document/errors"
)

type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo {
	return &Repo{pool: pool}
}

// Get — метаданные файла; нет — ErrDocumentNotFound.
func (r *Repo) Get(ctx context.Context, id uuid.UUID) (documentdomain.Document, error) {
	var d documentdomain.Document
	err := r.pool.QueryRow(ctx, `
		SELECT id, application_id, type, file_name, content_type, size_bytes, storage_key, sha256, created_at
		FROM application_documents WHERE id = $1`, id,
	).Scan(&d.ID, &d.ApplicationID, &d.Type, &d.FileName, &d.ContentType, &d.SizeBytes, &d.StorageKey, &d.SHA256, &d.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return documentdomain.Document{}, documenterrors.ErrDocumentNotFound
	}
	if err != nil {
		return documentdomain.Document{}, fmt.Errorf("select document: %w", err)
	}
	return d, nil
}

// GetDetails — заявка документа: по ней проверяется доступ клиента. Нет заявки —
// applicationerrors.ErrNotFound.
func (r *Repo) GetDetails(ctx context.Context, id uuid.UUID) (applicationdomain.ApplicationDetails, error) {
	return applicationrepo.GetDetails(ctx, r.pool, id, false)
}

// InsertDocument пишет метаданные файла. Файл к этому моменту уже в хранилище;
// пишет модуль, которому документ принадлежит, в своей транзакции.
func InsertDocument(ctx context.Context, q applicationrepo.Querier, d documentdomain.Document) error {
	_, err := q.Exec(ctx, `
		INSERT INTO application_documents (id, application_id, type, file_name, content_type, size_bytes, storage_key, sha256)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		d.ID, d.ApplicationID, d.Type, d.FileName, d.ContentType, d.SizeBytes, d.StorageKey, d.SHA256,
	)
	if err != nil {
		return fmt.Errorf("insert document: %w", err)
	}
	return nil
}
