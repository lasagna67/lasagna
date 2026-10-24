package companyrepo

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

type accountExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

// EnsureSettlementAccount используется demo-seed, в том числе для ранее подключённых
// компаний. Повторный вызов сохраняет номер, баланс и статус существующего счёта.
func (r *CompanyRepo) EnsureSettlementAccount(ctx context.Context, companyID uuid.UUID) error {
	return ensureSettlementAccount(ctx, r.pool, companyID)
}

// SeedSettlementBalance вызывается только demo-seed. Однократная инициализация:
// повторный seed, в том числе после обнуления счёта, не пополняет его заново.
func (r *CompanyRepo) SeedSettlementBalance(ctx context.Context, companyID uuid.UUID, balance int64) error {
	if balance < 0 {
		return fmt.Errorf("demo balance must be nonnegative")
	}
	_, err := r.pool.Exec(ctx, `UPDATE company_accounts
		SET balance_kopecks=$2, demo_balance_seeded=true
		WHERE company_id=$1 AND NOT demo_balance_seeded
			AND balance_kopecks=0 AND status='active'`, companyID, balance)
	if err != nil {
		return fmt.Errorf("seed company account balance: %w", err)
	}
	return nil
}

func ensureSettlementAccount(ctx context.Context, db accountExecutor, companyID uuid.UUID) error {
	_, err := db.Exec(ctx, `
		INSERT INTO company_accounts (company_id)
		SELECT id FROM companies
		WHERE id = $1 AND registry_status IN ('active', 'reorganizing')
		ON CONFLICT (company_id) DO NOTHING`, companyID)
	if err != nil {
		return fmt.Errorf("ensure company settlement account: %w", err)
	}
	return nil
}
