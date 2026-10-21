package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"time"

	"esign-mock/internal/model"
)

var (
	personINNPattern  = regexp.MustCompile(`^[0-9]{12}$`)
	companyINNPattern = regexp.MustCompile(`^[0-9]{10}$`)
)

type JSONRepository struct {
	certificates map[string]model.StoredCertificate
	serials      []string
}

// NewJSONRepository один раз загружает сертификаты из JSON-файла и проверяет их.
func NewJSONRepository(path string) (*JSONRepository, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read certificates %q: %w", path, err)
	}
	var records []model.StoredCertificate
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, fmt.Errorf("decode certificates %q: %w", path, err)
	}
	if records == nil {
		return nil, fmt.Errorf("decode certificates %q: expected an array, got null", path)
	}
	certificates := make(map[string]model.StoredCertificate, len(records))
	serials := make([]string, 0, len(records))
	for i, record := range records {
		if err := validate(record); err != nil {
			return nil, fmt.Errorf("load certificates %q record %d: %w", path, i+1, err)
		}
		if _, exists := certificates[record.Serial]; exists {
			return nil, fmt.Errorf("load certificates %q: duplicate serial %q", path, record.Serial)
		}
		certificates[record.Serial] = record
		serials = append(serials, record.Serial)
	}
	sort.Strings(serials)
	return &JSONRepository{certificates: certificates, serials: serials}, nil
}

func (r *JSONRepository) List(ctx context.Context) ([]model.StoredCertificate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	list := make([]model.StoredCertificate, 0, len(r.serials))
	for _, serial := range r.serials {
		list = append(list, r.certificates[serial])
	}
	return list, nil
}

func (r *JSONRepository) Get(ctx context.Context, serial string) (model.StoredCertificate, error) {
	if err := ctx.Err(); err != nil {
		return model.StoredCertificate{}, err
	}
	certificate, ok := r.certificates[serial]
	if !ok {
		return model.StoredCertificate{}, fmt.Errorf("find certificate %q: %w", serial, model.ErrCertificateNotFound)
	}
	return certificate, nil
}

// validate отсекает данные, с которыми сервис вёл бы себя непредсказуемо.
func validate(c model.StoredCertificate) error {
	if c.Serial == "" || c.Issuer == "" || c.PIN == "" || c.Subject.FullName == "" {
		return fmt.Errorf("serial, issuer, pin and subject.full_name are required")
	}
	if !personINNPattern.MatchString(c.Subject.INN) {
		return fmt.Errorf("subject.inn must contain 12 digits")
	}
	switch c.Type {
	case model.TypeLegalEntityHead:
		if c.Organization == nil || c.Organization.Name == "" || !companyINNPattern.MatchString(c.Organization.INN) {
			return fmt.Errorf("legal_entity_head requires organization with name and 10-digit inn")
		}
	case model.TypeIndividualEntrepreneur, model.TypeIndividual:
		if c.Organization != nil {
			return fmt.Errorf("%s must not have organization", c.Type)
		}
	default:
		return fmt.Errorf("unknown type %q", c.Type)
	}
	if c.Status != model.StatusValid && c.Status != model.StatusRevoked {
		return fmt.Errorf("unknown status %q", c.Status)
	}
	from, err := time.Parse(time.DateOnly, c.ValidFrom)
	if err != nil {
		return fmt.Errorf("valid_from: %w", err)
	}
	to, err := time.Parse(time.DateOnly, c.ValidTo)
	if err != nil {
		return fmt.Errorf("valid_to: %w", err)
	}
	if to.Before(from) {
		return fmt.Errorf("valid_to is before valid_from")
	}
	return nil
}
