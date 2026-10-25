package companyservice

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
	companyerrors "github.com/lasagna67/lasagna.git/internal/company/errors"
	"github.com/lasagna67/lasagna.git/internal/validation"
)

// challengeTTL — сколько живёт заявление о подключении: хватает, чтобы
// выбрать сертификат и ввести PIN.
const challengeTTL = 5 * time.Minute

type CompanyRepo interface {
	SaveOverview(ctx context.Context, overview companydomain.Overview) error
	GetOverview(ctx context.Context, companyID uuid.UUID) (companydomain.Overview, error)

	// SaveConnection в одной транзакции создаёт или обновляет компанию по ИНН
	// и связь с пользователем.
	SaveConnection(ctx context.Context, company companydomain.Company, member companydomain.Member) (companydomain.Membership, error)
	ListByUser(ctx context.Context, userID uuid.UUID) ([]companydomain.Membership, error)

	// GetMembership возвращает companyerrors.ErrCompanyNotFound, если пользователь не участник.
	GetMembership(ctx context.Context, companyID, userID uuid.UUID) (companydomain.Membership, error)
	ListMembers(ctx context.Context, companyID uuid.UUID) ([]companydomain.MemberProfile, error)

	// FindClientByEmail ищет активного клиента, иначе companyerrors.ErrUserNotFound.
	FindClientByEmail(ctx context.Context, email string) (uuid.UUID, error)

	// AddMember возвращает companyerrors.ErrAlreadyMember, если связь уже есть.
	AddMember(ctx context.Context, member companydomain.Member) (companydomain.MemberProfile, error)
	RemoveMember(ctx context.Context, companyID, userID uuid.UUID) error
}

type ChallengeRepo interface {
	Save(ctx context.Context, challenge companydomain.Challenge, ttl time.Duration) error

	// Take возвращает и удаляет заявление: подписанное используется один раз.
	// Нет или истекло — companyerrors.ErrChallengeNotFound.
	Take(ctx context.Context, id string) (companydomain.Challenge, error)
}

// External — внешние системы модуля: реестр и УЦ. Реализация — companyhttpjson.
type External interface {
	GetCreditReport(ctx context.Context, inn string) (companydomain.CreditReport, error)
	GetFinancialReport(ctx context.Context, inn string) (companydomain.FinancialReport, error)
	// GetCard — карточка из ЕГРЮЛ/ЕГРИП. Нет в реестре — companyerrors.ErrNotInRegistry,
	// реестр недоступен — companyerrors.ErrRegistryUnavailable.
	GetCard(ctx context.Context, inn string) (companydomain.RegistryCard, error)

	// VerifySignature проверяет КЭП над data и возвращает сертификат подписанта.
	// Подпись не принята — *companyerrors.SignatureError,
	// УЦ недоступен — companyerrors.ErrESignUnavailable.
	VerifySignature(ctx context.Context, data, signature string) (companydomain.Certificate, error)
}

type CompanyService struct {
	companies  CompanyRepo
	challenges ChallengeRepo
	external   External
	now        func() time.Time
}

func New(companies CompanyRepo, challenges ChallengeRepo, external External) *CompanyService {
	return &CompanyService{
		companies:  companies,
		challenges: challenges,
		external:   external,
		now:        time.Now,
	}
}

// Lookup — карточка компании из ЕГРЮЛ/ЕГРИП. Ничего не сохраняет:
// данные реестра публичные, а полномочия ещё не подтверждены.
func (s *CompanyService) Lookup(ctx context.Context, inn string) (companydomain.RegistryCard, error) {
	inn, err := normalizeINN(inn)
	if err != nil {
		return companydomain.RegistryCard{}, err
	}
	return s.lookup(ctx, inn)
}

// CreateChallenge — первый шаг подключения: заявление, которое руководитель
// подпишет своей КЭП в esign-mock.
func (s *CompanyService) CreateChallenge(ctx context.Context, cmd companydomain.CreateChallengeCommand) (companydomain.Challenge, companydomain.RegistryCard, error) {
	inn, err := normalizeINN(cmd.INN)
	if err != nil {
		return companydomain.Challenge{}, companydomain.RegistryCard{}, err
	}
	card, err := s.lookup(ctx, inn)
	if err != nil {
		return companydomain.Challenge{}, companydomain.RegistryCard{}, err
	}

	challenge := companydomain.Challenge{
		ID:        uuid.NewString(),
		UserID:    cmd.UserID,
		INN:       inn,
		ExpiresAt: s.now().UTC().Add(challengeTTL).Truncate(time.Second),
	}
	challenge.Data = statement(card, challenge)

	if err := s.challenges.Save(ctx, challenge, challengeTTL); err != nil {
		return companydomain.Challenge{}, companydomain.RegistryCard{}, fmt.Errorf("save challenge: %w", err)
	}
	return challenge, card, nil
}

// Connect — второй шаг: проверяет подпись заявления и сверяет владельца
// сертификата с руководителем по реестру. Успех — пользователь становится
// owner (ИП) или ceo (ЮЛ) с правом подписи. Заявление тратится при любом исходе.
func (s *CompanyService) Connect(ctx context.Context, cmd companydomain.ConnectCommand) (companydomain.Membership, error) {
	if _, err := uuid.Parse(cmd.ChallengeID); err != nil {
		return companydomain.Membership{}, invalid("challenge_id", "Некорректный challenge_id")
	}
	if cmd.Signature == "" {
		return companydomain.Membership{}, invalid("signature", "Подпись обязательна")
	}

	challenge, err := s.challenges.Take(ctx, cmd.ChallengeID)
	if err != nil {
		return companydomain.Membership{}, fmt.Errorf("take challenge: %w", err)
	}
	if challenge.UserID != cmd.UserID || !s.now().Before(challenge.ExpiresAt) {
		return companydomain.Membership{}, companyerrors.ErrChallengeNotFound
	}

	cert, err := s.external.VerifySignature(ctx, challenge.Data, cmd.Signature)
	if err != nil {
		return companydomain.Membership{}, fmt.Errorf("verify signature: %w", err)
	}

	// Реестр запрашивается заново: руководитель мог смениться, пока подписывали.
	card, err := s.lookup(ctx, challenge.INN)
	if err != nil {
		return companydomain.Membership{}, err
	}
	position, ok := validation.CertificateHeadPosition(card, cert)
	if !ok {
		return companydomain.Membership{}, companyerrors.ErrNotCompanyHead
	}

	serial := cert.Serial
	membership, err := s.companies.SaveConnection(ctx, companyFromCard(card), companydomain.Member{
		UserID:            cmd.UserID,
		Position:          position,
		CanSign:           true,
		CertificateSerial: &serial,
	})
	if err != nil {
		return companydomain.Membership{}, fmt.Errorf("save connection: %w", err)
	}
	return membership, nil
}

// List — компании пользователя с его ролью в каждой.
func (s *CompanyService) List(ctx context.Context, userID uuid.UUID) ([]companydomain.Membership, error) {
	memberships, err := s.companies.ListByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list companies: %w", err)
	}
	return memberships, nil
}

// Membership — компания и роль пользователя в ней. Проверка доступа для
// других модулей: companyerrors.ErrCompanyNotFound — пользователь не участник.
func (s *CompanyService) Membership(ctx context.Context, companyID, userID uuid.UUID) (companydomain.Membership, error) {
	membership, err := s.companies.GetMembership(ctx, companyID, userID)
	if err != nil {
		return companydomain.Membership{}, fmt.Errorf("get membership: %w", err)
	}
	return membership, nil
}

// Members — представители компании. Видит любой участник.
func (s *CompanyService) Members(ctx context.Context, companyID, actorID uuid.UUID) ([]companydomain.MemberProfile, error) {
	if _, err := s.Membership(ctx, companyID, actorID); err != nil {
		return nil, err
	}
	members, err := s.companies.ListMembers(ctx, companyID)
	if err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}
	return members, nil
}

// AddMember — руководитель приглашает зарегистрированного клиента без права подписи.
func (s *CompanyService) AddMember(ctx context.Context, cmd companydomain.AddMemberCommand) (companydomain.MemberProfile, error) {
	actor, err := s.Membership(ctx, cmd.CompanyID, cmd.ActorID)
	if err != nil {
		return companydomain.MemberProfile{}, err
	}
	if !actor.Member.CanSign {
		return companydomain.MemberProfile{}, companyerrors.ErrNotSigner
	}
	if !cmd.Position.Invitable() {
		return companydomain.MemberProfile{}, invalid("position", "Должность: cfo, accountant, finance_officer или authorized_employee")
	}
	email, err := normalizeEmail(cmd.Email)
	if err != nil {
		return companydomain.MemberProfile{}, err
	}

	userID, err := s.companies.FindClientByEmail(ctx, email)
	if err != nil {
		return companydomain.MemberProfile{}, fmt.Errorf("find client: %w", err)
	}
	profile, err := s.companies.AddMember(ctx, companydomain.Member{
		CompanyID: cmd.CompanyID,
		UserID:    userID,
		Position:  cmd.Position,
		InvitedBy: &cmd.ActorID,
	})
	if err != nil {
		return companydomain.MemberProfile{}, fmt.Errorf("add member: %w", err)
	}
	return profile, nil
}

// RemoveMember — руководитель удаляет сотрудника, сотрудник может выйти сам.
// Руководителя так не удалить: его связь подтверждена КЭП.
func (s *CompanyService) RemoveMember(ctx context.Context, cmd companydomain.RemoveMemberCommand) error {
	actor, err := s.Membership(ctx, cmd.CompanyID, cmd.ActorID)
	if err != nil {
		return err
	}
	if cmd.UserID != cmd.ActorID && !actor.Member.CanSign {
		return companyerrors.ErrNotSigner
	}

	target := actor.Member
	if cmd.UserID != cmd.ActorID {
		membership, err := s.companies.GetMembership(ctx, cmd.CompanyID, cmd.UserID)
		if errors.Is(err, companyerrors.ErrCompanyNotFound) {
			return companyerrors.ErrMemberNotFound
		}
		if err != nil {
			return fmt.Errorf("get member: %w", err)
		}
		target = membership.Member
	}
	if target.CanSign {
		return companyerrors.ErrCannotRemoveSigner
	}

	if err := s.companies.RemoveMember(ctx, cmd.CompanyID, cmd.UserID); err != nil {
		return fmt.Errorf("remove member: %w", err)
	}
	return nil
}

//============================================================================================

func (s *CompanyService) lookup(ctx context.Context, inn string) (companydomain.RegistryCard, error) {
	card, err := s.external.GetCard(ctx, inn)
	if err != nil {
		return companydomain.RegistryCard{}, fmt.Errorf("get registry card: %w", err)
	}
	return card, nil
}

// statement — текст заявления. Одноразовый код делает каждое заявление
// уникальным, срок и id пользователя не дают переиспользовать подпись.
func statement(card companydomain.RegistryCard, challenge companydomain.Challenge) string {
	return fmt.Sprintf(
		"Заявление о подключении компании к кредитному кабинету.\n"+
			"Компания: %s, ИНН %s.\n"+
			"Подтверждаю полномочия действовать от имени компании без доверенности.\n"+
			"Пользователь: %s.\n"+
			"Одноразовый код: %s.\n"+
			"Действительно до: %s.",
		card.Name, card.INN, challenge.UserID, rand.Text(), challenge.ExpiresAt.Format(time.RFC3339),
	)
}
