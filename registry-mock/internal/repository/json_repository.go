package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"registry-mock/internal/model"
)

type JSONRepository struct {
	egripCompanies map[string]model.Company
	egrulCompanies map[string]model.Company
}

func NewJSONRepository(egripPath, egrulPath string) (*JSONRepository, error) {
	egripCompanies, err := loadCompanies(egripPath)
	if err != nil {
		return nil, err
	}
	egrulCompanies, err := loadCompanies(egrulPath)
	if err != nil {
		return nil, err
	}
	for inn := range egrulCompanies {
		if _, exists := egripCompanies[inn]; exists {
			return nil, fmt.Errorf("load registry data: duplicate INN %q across %q and %q", inn, egripPath, egrulPath)
		}
	}
	return &JSONRepository{egripCompanies: egripCompanies, egrulCompanies: egrulCompanies}, nil
}

func loadCompanies(path string) (map[string]model.Company, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read registry data %q: %w", path, err)
	}
	var companies []model.Company
	if err := json.Unmarshal(data, &companies); err != nil {
		return nil, fmt.Errorf("decode registry data %q: %w", path, err)
	}
	if companies == nil {
		return nil, fmt.Errorf("decode registry data %q: expected an array of reports, got null", path)
	}
	index := make(map[string]model.Company, len(companies))
	for _, company := range companies {
		if _, exists := index[company.INN]; exists {
			return nil, fmt.Errorf("load registry data %q: duplicate INN %q", path, company.INN)
		}
		index[company.INN] = company
	}
	return index, nil
}

func (r *JSONRepository) GetEGRIP(ctx context.Context, inn string) (model.Company, error) {
	return findCompany(ctx, r.egripCompanies, inn)
}

func (r *JSONRepository) GetEGRUL(ctx context.Context, inn string) (model.Company, error) {
	return findCompany(ctx, r.egrulCompanies, inn)
}

func findCompany(ctx context.Context, companies map[string]model.Company, inn string) (model.Company, error) {
	if err := ctx.Err(); err != nil {
		return model.Company{}, err
	}
	company, ok := companies[inn]
	if !ok {
		return model.Company{}, fmt.Errorf("find company with INN %q: %w", inn, model.ErrCompanyNotFound)
	}
	return company, nil
}
