package companyrepo

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
	companyerrors "github.com/lasagna67/lasagna.git/internal/company/errors"
)

const companyColumns = `c.id, c.inn, c.entity_type, c.short_name, c.registration_date,
	c.registry_status, c.termination_date, c.primary_okved, c.additional_okveds,
	c.head_full_name, c.head_position, c.head_inn,
	c.registry_synced_at, c.created_at, c.updated_at`

const memberColumns = `m.company_id, m.user_id, m.position, m.can_sign,
	m.certificate_serial, m.invited_by, m.created_at`

const profileColumns = memberColumns + `, u.email, u.first_name, u.last_name, u.middle_name`

type CompanyRepo struct {
	pool *pgxpool.Pool
}

func NewCompanyRepo(pool *pgxpool.Pool) *CompanyRepo {
	return &CompanyRepo{pool: pool}
}

func (r *CompanyRepo) SaveConnection(ctx context.Context, company companydomain.Company, member companydomain.Member) (companydomain.Membership, error) {
	var result companydomain.Membership
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		headName, headPosition, headINN := headColumns(company.Head)
		row := tx.QueryRow(ctx, `
			INSERT INTO companies AS c (inn, entity_type, short_name, registration_date, registry_status,
				termination_date, primary_okved, additional_okveds,
				head_full_name, head_position, head_inn, registry_synced_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, now())
			ON CONFLICT (inn) DO UPDATE SET
				short_name         = EXCLUDED.short_name,
				registration_date  = EXCLUDED.registration_date,
				registry_status    = EXCLUDED.registry_status,
				termination_date   = EXCLUDED.termination_date,
				primary_okved      = EXCLUDED.primary_okved,
				additional_okveds  = EXCLUDED.additional_okveds,
				head_full_name     = EXCLUDED.head_full_name,
				head_position      = EXCLUDED.head_position,
				head_inn           = EXCLUDED.head_inn,
				registry_synced_at = EXCLUDED.registry_synced_at
			RETURNING `+companyColumns,
			company.INN, company.EntityType, company.ShortName, company.RegistrationDate, company.RegistryStatus,
			company.TerminationDate, company.PrimaryOKVED, company.AdditionalOKVEDs,
			headName, headPosition, headINN,
		)
		var cr companyRow
		if err := row.Scan(cr.dest()...); err != nil {
			return fmt.Errorf("upsert company: %w", err)
		}
		result.Company = cr.company()

		// Повторное подключение обновляет должность и сертификат: например,
		// приглашённый бухгалтер стал директором.
		row = tx.QueryRow(ctx, `
			INSERT INTO company_members AS m (company_id, user_id, position, can_sign, certificate_serial)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (company_id, user_id) DO UPDATE SET
				position           = EXCLUDED.position,
				can_sign           = EXCLUDED.can_sign,
				certificate_serial = EXCLUDED.certificate_serial,
				invited_by         = NULL
			RETURNING `+memberColumns,
			result.Company.ID, member.UserID, member.Position, member.CanSign, member.CertificateSerial,
		)
		if err := row.Scan(memberDest(&result.Member)...); err != nil {
			return fmt.Errorf("upsert member: %w", err)
		}
		// Счёт появляется только вместе с подтверждённым подключением компании.
		// Баланс нового счёта равен нулю; повторное подключение его не меняет.
		return ensureSettlementAccount(ctx, tx, result.Company.ID)
	})
	if err != nil {
		return companydomain.Membership{}, err
	}
	return result, nil
}

func (r *CompanyRepo) ListByUser(ctx context.Context, userID uuid.UUID) ([]companydomain.Membership, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+companyColumns+`, `+memberColumns+`
		FROM company_members m
		JOIN companies c ON c.id = m.company_id
		WHERE m.user_id = $1
		ORDER BY m.created_at`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("select companies: %w", err)
	}
	memberships, err := pgx.CollectRows(rows, scanMembership)
	if err != nil {
		return nil, fmt.Errorf("scan companies: %w", err)
	}
	return memberships, nil
}

func (r *CompanyRepo) GetMembership(ctx context.Context, companyID, userID uuid.UUID) (companydomain.Membership, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+companyColumns+`, `+memberColumns+`
		FROM company_members m
		JOIN companies c ON c.id = m.company_id
		WHERE m.company_id = $1 AND m.user_id = $2`,
		companyID, userID,
	)
	if err != nil {
		return companydomain.Membership{}, fmt.Errorf("select membership: %w", err)
	}
	membership, err := pgx.CollectExactlyOneRow(rows, scanMembership)
	if errors.Is(err, pgx.ErrNoRows) {
		return companydomain.Membership{}, companyerrors.ErrCompanyNotFound
	}
	if err != nil {
		return companydomain.Membership{}, fmt.Errorf("scan membership: %w", err)
	}
	return membership, nil
}

func (r *CompanyRepo) ListMembers(ctx context.Context, companyID uuid.UUID) ([]companydomain.MemberProfile, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+profileColumns+`
		FROM company_members m
		JOIN users u ON u.id = m.user_id
		WHERE m.company_id = $1
		ORDER BY m.can_sign DESC, m.created_at`,
		companyID,
	)
	if err != nil {
		return nil, fmt.Errorf("select members: %w", err)
	}
	members, err := pgx.CollectRows(rows, scanProfile)
	if err != nil {
		return nil, fmt.Errorf("scan members: %w", err)
	}
	return members, nil
}

func (r *CompanyRepo) FindClientByEmail(ctx context.Context, email string) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.pool.QueryRow(ctx, `
		SELECT id FROM users
		WHERE lower(email) = lower($1) AND role = 'client' AND status = 'active'`,
		email,
	).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, companyerrors.ErrUserNotFound
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("select client by email: %w", err)
	}
	return id, nil
}

func (r *CompanyRepo) AddMember(ctx context.Context, member companydomain.Member) (companydomain.MemberProfile, error) {
	rows, err := r.pool.Query(ctx, `
		WITH m AS (
			INSERT INTO company_members (company_id, user_id, position, can_sign, invited_by)
			VALUES ($1, $2, $3, false, $4)
			ON CONFLICT (company_id, user_id) DO NOTHING
			RETURNING *
		)
		SELECT `+profileColumns+`
		FROM m
		JOIN users u ON u.id = m.user_id`,
		member.CompanyID, member.UserID, member.Position, member.InvitedBy,
	)
	if err != nil {
		return companydomain.MemberProfile{}, fmt.Errorf("insert member: %w", err)
	}
	profile, err := pgx.CollectExactlyOneRow(rows, scanProfile)
	if errors.Is(err, pgx.ErrNoRows) {
		return companydomain.MemberProfile{}, companyerrors.ErrAlreadyMember
	}
	if err != nil {
		return companydomain.MemberProfile{}, fmt.Errorf("scan member: %w", err)
	}
	return profile, nil
}

// RemoveMember удаляет только связь без права подписи: руководителя,
// подтверждённого КЭП, так не удалить даже при гонке с повторным подключением.
func (r *CompanyRepo) RemoveMember(ctx context.Context, companyID, userID uuid.UUID) error {
	tag, err := r.pool.Exec(ctx,
		`DELETE FROM company_members WHERE company_id = $1 AND user_id = $2 AND NOT can_sign`,
		companyID, userID,
	)
	if err != nil {
		return fmt.Errorf("delete member: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return companyerrors.ErrMemberNotFound
	}
	return nil
}

//============================================================================================

// companyRow — компания плюс nullable-колонки руководителя до сборки Head.
type companyRow struct {
	c                               companydomain.Company
	headName, headPosition, headINN *string
}

func (r *companyRow) dest() []any {
	return []any{
		&r.c.ID, &r.c.INN, &r.c.EntityType, &r.c.ShortName, &r.c.RegistrationDate,
		&r.c.RegistryStatus, &r.c.TerminationDate, &r.c.PrimaryOKVED, &r.c.AdditionalOKVEDs,
		&r.headName, &r.headPosition, &r.headINN,
		&r.c.RegistrySyncedAt, &r.c.CreatedAt, &r.c.UpdatedAt,
	}
}

func (r *companyRow) company() companydomain.Company {
	if r.headName != nil && r.headINN != nil {
		r.c.Head = &companydomain.Head{FullName: *r.headName, INN: *r.headINN}
		if r.headPosition != nil {
			r.c.Head.Position = *r.headPosition
		}
	}
	return r.c
}

func memberDest(m *companydomain.Member) []any {
	return []any{&m.CompanyID, &m.UserID, &m.Position, &m.CanSign, &m.CertificateSerial, &m.InvitedBy, &m.CreatedAt}
}

func scanMembership(row pgx.CollectableRow) (companydomain.Membership, error) {
	var cr companyRow
	var m companydomain.Member
	if err := row.Scan(append(cr.dest(), memberDest(&m)...)...); err != nil {
		return companydomain.Membership{}, err
	}
	return companydomain.Membership{Company: cr.company(), Member: m}, nil
}

func scanProfile(row pgx.CollectableRow) (companydomain.MemberProfile, error) {
	var p companydomain.MemberProfile
	dest := append(memberDest(&p.Member), &p.Email, &p.FirstName, &p.LastName, &p.MiddleName)
	if err := row.Scan(dest...); err != nil {
		return companydomain.MemberProfile{}, err
	}
	return p, nil
}

func headColumns(head *companydomain.Head) (name, position, inn *string) {
	if head == nil {
		return nil, nil, nil
	}
	return &head.FullName, &head.Position, &head.INN
}
