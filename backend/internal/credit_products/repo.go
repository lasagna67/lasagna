package creditproducts

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrProductNotFound = errors.New("credit product not found")

// ENUM-массив читаем как text[], чтобы pgx не требовал регистрации его OID.
const productColumns = `id, code, name, description, purposes::text[],
	min_amount_kopecks, max_amount_kopecks, min_term_months, max_term_months, base_rate,
	collateral_mode::text, repayment_type::text, max_drawdown_days,
	min_business_age_months, is_active, created_at, updated_at`

type ProductRepo struct {
	pool *pgxpool.Pool
}

var _ Repository = (*ProductRepo)(nil)

func NewProductRepo(pool *pgxpool.Pool) *ProductRepo {
	return &ProductRepo{pool: pool}
}

// GetByCode возвращает продукт независимо от активности: её проверяет Service.
// Если код не найден, ошибку можно распознать через errors.Is(err, ErrProductNotFound).
func (r *ProductRepo) GetByCode(ctx context.Context, code string) (CreditProduct, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+productColumns+` FROM credit_products WHERE code = $1`, code)
	product, err := scanProduct(row)
	if err != nil {
		return CreditProduct{}, fmt.Errorf("select credit product by code: %w", err)
	}
	return product, nil
}

// ListActive возвращает только доступные продукты в стабильном порядке по коду.
func (r *ProductRepo) ListActive(ctx context.Context) ([]CreditProduct, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+productColumns+` FROM credit_products WHERE is_active = true ORDER BY code`)
	if err != nil {
		return nil, fmt.Errorf("select active credit products: %w", err)
	}
	// CollectRows закрывает rows и проверяет ошибки чтения результата запроса.
	products, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (CreditProduct, error) {
		return scanProduct(row)
	})
	if err != nil {
		return nil, fmt.Errorf("scan active credit products: %w", err)
	}
	return products, nil
}

// scanProduct сохраняет SQL NULL как nil у BaseRate и MaxDrawdownDays.
func scanProduct(row pgx.Row) (CreditProduct, error) {
	var product CreditProduct
	err := row.Scan(
		&product.ID, &product.Code, &product.Name, &product.Description, &product.Purposes,
		&product.MinAmountKopecks, &product.MaxAmountKopecks,
		&product.MinTermMonths, &product.MaxTermMonths, &product.BaseRate,
		&product.CollateralMode, &product.RepaymentType, &product.MaxDrawdownDays,
		&product.MinBusinessAgeMonths, &product.IsActive, &product.CreatedAt, &product.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return CreditProduct{}, ErrProductNotFound
	}
	if err != nil {
		return CreditProduct{}, err
	}
	return product, nil
}
