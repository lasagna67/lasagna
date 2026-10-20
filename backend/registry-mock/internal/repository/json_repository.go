package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"registry-mock/internal/model"
)

type JSONRepository struct {
	companies map[string]model.Company
}

func NewJSONRepository(path string) (*JSONRepository, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read registry data %q: %w", path, err)
	}
	var companies []model.Company
	if err := json.Unmarshal(data, &companies); err != nil {
		return nil, fmt.Errorf("decode registry data %q: %w", path, err)
	}
	if companies == nil {
		return nil, fmt.Errorf("decode registry data %q: expected an array of companies, got null", path)
	}
	index := make(map[string]model.Company, len(companies))
	for _, company := range companies {
		if _, exists := index[company.INN]; exists {
			return nil, fmt.Errorf("load registry data %q: duplicate INN %q", path, company.INN)
		}
		index[company.INN] = company
	}
	return &JSONRepository{companies: index}, nil
}

func (r *JSONRepository) GetCompany(ctx context.Context, inn string) (model.Company, error) {
	if err := ctx.Err(); err != nil {
		return model.Company{}, err
	}
	company, ok := r.companies[inn]
	if !ok {
		return model.Company{}, fmt.Errorf("find company with INN %q: %w", inn, model.ErrCompanyNotFound)
	}
	return company, nil
}
