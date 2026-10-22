package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// profile связывает сценарий кабинета с существующим ИНН в моках.
type profile struct{ Kind, Scenario, INN string }

var profiles = []profile{
	{"ooo", "good", "7705005410"}, {"ooo", "thin", "7810969607"}, {"ooo", "overdue", "2310569351"},
	{"ooo", "no-history", "1655501828"}, {"ooo", "no-data", "6671986865"}, {"ooo", "terminated", "5260947681"},
	{"ip", "good", "770761836401"}, {"ip", "thin", "781497718575"}, {"ip", "overdue", "770815348700"},
	{"ip", "no-history", "165979491871"}, {"ip", "no-data", "667036260874"}, {"ip", "terminated", "526200583660"},
}
var scenarioLabels = map[string]string{
	"good": "Хорошая кредитная история и достаточные финансы", "thin": "Небольшой запас денег", "overdue": "Текущая просрочка",
	"no-history": "Нет кредитной истории, есть финансовые данные", "no-data": "Нет кредитной истории и финансовых данных", "terminated": "Деятельность прекращена",
}

type certificate struct {
	Serial, Type, Status string
	Subject              struct{ INN string }
	Organization         struct{ INN string }
}
type company struct {
	ID, INN, Name string
	EntityType    string `json:"entity_type"`
}

// account — запись для итогового JSON: логин, ID компании и адреса её API.
// Токен сессии сюда не попадает: frontend получает свой токен через login.
type account struct {
	Key         string `json:"key"`
	Scenario    string `json:"scenario"`
	EntityType  string `json:"entity_type"`
	Name        string `json:"name"`
	INN         string `json:"inn"`
	Email       string `json:"email"`
	UserID      string `json:"user_id"`
	CompanyID   string `json:"company_id"`
	CompanyURL  string `json:"company_url"`
	OverviewURL string `json:"overview_url"`
}

// seeder заполняет кабинет через HTTP API; подключения к БД у него нет.
// baseURL используется внутри Docker, publicURL — в ссылках для frontend.
type seeder struct {
	client                                                   *http.Client
	baseURL, registryURL, esignURL, publicURL, password, pin string
}
type statusError struct {
	method, path string
	status       int
}

// Error описывает неуспешный HTTP-ответ без вывода тела запроса и токена.
func (e *statusError) Error() string {
	return fmt.Sprintf("%s %s: HTTP %d", e.method, e.path, e.status)
}

// request отправляет JSON-запрос и декодирует успешный ответ в out.
// body == nil означает запрос без JSON-тела, out == nil — ответ не нужен.
// Непустой token передаётся как Bearer; любой статус вне 2xx возвращает statusError.
func (s *seeder) request(ctx context.Context, method, url, token string, body, out any) error {
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, req.URL.Path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &statusError{method, req.URL.Path, resp.StatusCode}
	}
	if out == nil {
		_, err = io.Copy(io.Discard, resp.Body)
		return err
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out); err != nil {
		return fmt.Errorf("decode %s: %w", req.URL.Path, err)
	}
	return nil
}

// selectCertificate выбирает сертификат со статусом valid для нужного бизнеса.
// Для ООО должны совпасть ИНН организации и ИНН руководителя из реестра;
// для ИП — ИНН владельца сертификата и самого предпринимателя.
// Срок действия и подпись дополнительно проверяются при обращении к API подписи.
func selectCertificate(p profile, headINN string, certs []certificate) (certificate, error) {
	for _, c := range certs {
		if c.Status != "valid" {
			continue
		}
		if p.Kind == "ooo" && c.Type == "legal_entity_head" && c.Organization.INN == p.INN && c.Subject.INN == headINN {
			return c, nil
		}
		if p.Kind == "ip" && c.Type == "individual_entrepreneur" && c.Subject.INN == p.INN {
			return c, nil
		}
	}
	return certificate{}, fmt.Errorf("no head certificate for %s", p.INN)
}

// seedAccount подготавливает один сценарий: регистрация → вход → подключение
// компании. При повторном запуске использует существующий
// аккаунт и уже подключённую компанию, не сбрасывая пароль и не подписывая заново.
// Успешно открытая сессия закрывается через logout даже при последующей ошибке.
func (s *seeder) seedAccount(ctx context.Context, p profile, cert certificate) (result account, err error) {
	email := "demo." + p.Kind + "." + p.Scenario + "@lasagna.test"
	base := strings.TrimRight(s.baseURL, "/") + "/api/v0"
	err = s.request(ctx, "POST", base+"/users", "", map[string]string{"email": email, "password": s.password, "first_name": "Демо", "last_name": scenarioLabels[p.Scenario]}, nil)
	var status *statusError
	// Конфликт регистрации допускается: дальше login проверит пароль аккаунта.
	if err != nil && !(errors.As(err, &status) && status.status == http.StatusConflict) {
		return result, err
	}
	var login struct {
		Token string `json:"access_token"`
		User  struct{ ID, Role string }
	}
	if err = s.request(ctx, "POST", base+"/auth/login", "", map[string]string{"email": email, "password": s.password}, &login); err != nil {
		return result, err
	}
	if login.Token == "" {
		return result, errors.New("login returned empty token")
	}
	defer func() {
		// Logout получает отдельные 5 секунд, даже если основной контекст отменён.
		// Именованный err позволяет вернуть и исходную ошибку, и ошибку logout.
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		err = errors.Join(err, s.request(cleanup, "POST", base+"/auth/logout", login.Token, nil, nil))
	}()
	if login.User.Role != "client" {
		return result, fmt.Errorf("%s is not a client", email)
	}
	var companies []company
	if err = s.request(ctx, "GET", base+"/companies", login.Token, nil, &companies); err != nil {
		return result, err
	}
	var business company
	for _, c := range companies {
		if c.INN == p.INN {
			business = c
			break
		}
	}
	if business.ID == "" {
		// Подключение проходит тот же путь, что и на frontend:
		// получить заявление, подписать исходный текст, передать подпись backend.
		var challenge struct {
			ID   string `json:"challenge_id"`
			Data string
		}
		if err = s.request(ctx, "POST", base+"/companies/challenges", login.Token, map[string]string{"inn": p.INN}, &challenge); err != nil {
			return result, err
		}
		var signed struct{ Signature string }
		if err = s.request(ctx, "POST", strings.TrimRight(s.esignURL, "/")+"/v1/sign", "", map[string]string{"certificate_serial": cert.Serial, "pin": s.pin, "data": challenge.Data}, &signed); err != nil {
			return result, err
		}
		if err = s.request(ctx, "POST", base+"/companies", login.Token, map[string]string{"challenge_id": challenge.ID, "signature": signed.Signature}, &business); err != nil {
			return result, err
		}
	}
	if business.ID == "" || business.INN != p.INN {
		return result, errors.New("company response does not match profile")
	}
	public := strings.TrimRight(s.publicURL, "/") + "/api/v0/companies/" + business.ID
	return account{Key: p.Kind + "-" + p.Scenario, Scenario: scenarioLabels[p.Scenario], EntityType: business.EntityType, Name: business.Name, INN: p.INN, Email: email, UserID: login.User.ID, CompanyID: business.ID, CompanyURL: public, OverviewURL: public + "/overview"}, nil
}

// run обрабатывает все 12 профилей: получает сертификаты, сверяет руководителя
// с реестром и вызывает seedAccount для каждого бизнеса. При первой ошибке
// останавливается; уже созданные записи сохраняются для повторного запуска.
// Итоговый JSON записывается после успешного создания или переиспользования всех аккаунтов.
func (s *seeder) run(ctx context.Context, manifestPath string) error {
	var certs struct{ Certificates []certificate }
	if err := s.request(ctx, "GET", strings.TrimRight(s.esignURL, "/")+"/v1/certificates", "", nil, &certs); err != nil {
		return err
	}
	accounts := make([]account, 0, len(profiles))
	for _, p := range profiles {
		registry := "egrul"
		if p.Kind == "ip" {
			registry = "egrip"
		}
		var card struct {
			INN    string
			Report struct{ Head *struct{ INN string } }
		}
		if err := s.request(ctx, "GET", strings.TrimRight(s.registryURL, "/")+"/v1/"+registry+"/"+p.INN, "", nil, &card); err != nil {
			return err
		}
		if card.INN != p.INN {
			return fmt.Errorf("registry returned wrong INN for %s", p.INN)
		}
		head := ""
		if card.Report.Head != nil {
			head = card.Report.Head.INN
		}
		cert, err := selectCertificate(p, head, certs.Certificates)
		if err != nil {
			return err
		}
		a, err := s.seedAccount(ctx, p, cert)
		if err != nil {
			return fmt.Errorf("%s/%s: %w", p.Kind, p.Scenario, err)
		}
		accounts = append(accounts, a)
		slog.Info("demo account prepared", "email", a.Email, "company_id", a.CompanyID, "scenario", a.Key)
	}
	manifest := struct {
		BaseURL  string    `json:"base_url"`
		LoginURL string    `json:"login_url"`
		Password string    `json:"demo_password"`
		Accounts []account `json:"accounts"`
	}{s.publicURL, strings.TrimRight(s.publicURL, "/") + "/api/v0/auth/login", s.password, accounts}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(manifestPath), 0755); err != nil {
		return err
	}
	temp := manifestPath + ".tmp"
	// Временный файл и rename не дают читателю увидеть наполовину записанный JSON.
	if err = os.WriteFile(temp, append(data, '\n'), 0600); err != nil {
		return err
	}
	if err = os.Rename(temp, manifestPath); err != nil {
		return err
	}
	slog.Info("demo seed completed", "accounts", len(accounts), "manifest", manifestPath)
	return nil
}
