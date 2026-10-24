package scoring

import (
	"slices"
	"sort"
	"time"

	d "github.com/lasagna67/lasagna.git/internal/company/domain"
)

func calculateDecisionMetrics(in ScoringInput, validation ValidationResult, now time.Time) (DecisionMetrics, error) {
	m := DecisionMetrics{Status: "not_completed", F: []MonthlyFlow{}, S: []MonthlyFlow{}, Schedule: []Payment{}}
	var months []d.MonthlyData
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	if report := in.Overview.Financials.Data; report != nil && in.Overview.Financials.Status == "ok" && report.Status == "ok" {
		for _, month := range report.MonthlyData {
			if month.Month < monthStart.Format("2006-01") {
				months = append(months, month)
			}
		}
		sort.Slice(months, func(i, j int) bool { return months[i].Month < months[j].Month })
		if len(months) > 0 {
			c, err := AvailableCashFlow(months)
			if err != nil {
				return m, err
			}
			m.C = &c
			cs, err := AvailableStressedCashFlow(months)
			if err != nil {
				return m, err
			}
			m.Cs = &cs
			for i, month := range months {
				f, err := MonthlyFreeCashFlow(month)
				if err != nil {
					return m, err
				}
				s, err := MonthlyStressedCashFlow(month)
				if err != nil {
					return m, err
				}
				m.F = append(m.F, MonthlyFlow{Month: month.Month, Value: f})
				m.S = append(m.S, MonthlyFlow{Month: month.Month, Value: s})
				if f > 0 {
					m.PositiveMonths++
					if i >= len(months)-6 {
						m.PositiveLastSix++
					}
					if i >= len(months)-3 {
						m.PositiveLastThree++
					}
				}
			}
			if len(months) >= 6 {
				recent := months[len(months)-6:]
				m.CompleteLastSix = true
				var oldIncome, recentIncome float64
				for i, month := range recent {
					if month.Month != monthStart.AddDate(0, i-6, 0).Format("2006-01") {
						m.CompleteLastSix = false
					}
					if i < 3 {
						oldIncome += float64(month.Income)
					} else {
						recentIncome += float64(month.Income)
					}
				}
				if oldIncome > 0 {
					g := recentIncome / oldIncome
					m.G = &g
				}
			}
		}
	}
	credit := in.Overview.CreditHistory.Data
	if credit != nil && in.Overview.CreditHistory.Status == "ok" && credit.Status == "ok" && credit.Report.Loans != nil {
		debt, err := ActiveMonthlyDebt(credit.Report.Loans)
		if err != nil {
			return m, err
		}
		m.D = &debt
	}
	if p := in.Product; p != nil && p.AnnualRateBasisPoints != nil && p.RepaymentType == "annuity" && in.Amount > 0 && in.TermMonths > 0 {
		schedule, err := BuildAnnuitySchedule(in.Amount, *p.AnnualRateBasisPoints, in.TermMonths)
		if err != nil {
			return m, err
		}
		m.Schedule = schedule
		pmax, err := MaximumPayment(schedule)
		if err != nil {
			return m, err
		}
		m.Pmax = &pmax
	}
	if m.C != nil && m.Cs != nil && m.D != nil && m.Pmax != nil {
		k, ks, err := CoverageRatios(*m.C, *m.Cs, *m.D, *m.Pmax)
		if err != nil {
			return m, err
		}
		m.K = &k
		m.Ks = &ks
		if validation.Status != StatusCannotScore {
			score, err := CalculateScore(k, credit.Report, months)
			if err != nil {
				return m, err
			}
			m.Score = &score
			m.Status = "completed"
		}
	}
	return m, nil
}

func checkProduct(in ScoringInput, now time.Time) (failed, pending []string) {
	p := in.Product
	if p == nil {
		return nil, []string{"product_missing"}
	}
	if !p.Active {
		failed = append(failed, "product_inactive")
	}
	if in.Purpose == "" || len(p.Purposes) == 0 {
		pending = append(pending, "product_purpose_missing")
	} else if !slices.Contains(p.Purposes, in.Purpose) {
		failed = append(failed, "product_purpose_mismatch")
	}
	if in.Amount <= 0 || p.MinAmount <= 0 || p.MaxAmount < p.MinAmount {
		pending = append(pending, "product_amount_missing")
	} else if in.Amount < p.MinAmount || in.Amount > p.MaxAmount {
		failed = append(failed, "product_amount_out_of_range")
	}
	if in.TermMonths <= 0 || p.MinTerm <= 0 || p.MaxTerm < p.MinTerm {
		pending = append(pending, "product_term_missing")
	} else if in.TermMonths < p.MinTerm || in.TermMonths > p.MaxTerm {
		failed = append(failed, "product_term_out_of_range")
	}
	card := in.Overview.Registry.Data
	if card == nil || card.RegistrationDate.IsZero() {
		pending = append(pending, "business_age_missing")
	} else if card.RegistrationDate.AddDate(0, p.MinBusinessAgeMonths, 0).After(now) {
		failed = append(failed, "business_too_young")
	}
	switch p.CollateralMode {
	case "none", "not_supported":
		if in.HasCollateral {
			failed = append(failed, "product_collateral_not_supported")
		}
	case "optional", "required":
	default:
		pending = append(pending, "product_collateral_mode_unknown")
	}
	if p.AnnualRateBasisPoints == nil {
		pending = append(pending, "product_rate_missing")
	}
	if p.RepaymentType != "annuity" {
		pending = append(pending, "repayment_calculation_unavailable")
	}
	return failed, pending
}
