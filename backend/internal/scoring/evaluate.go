package scoring

import "time"

// EvaluateApplication выбирает модель; финансовые правила остаются внутри неё.
func EvaluateApplication(in EvaluationInput, now time.Time) ScoringDecision {
	if in.Product == nil {
		return modelUnavailable("product_missing")
	}
	switch in.Product.Code {
	case "express", "working_capital", "equipment":
		return EvaluateAnnuity(in.ScoringInput, now)
	case "start":
		input := ProjectInput{ScoringInput: in.ScoringInput}
		if in.Project != nil {
			input = *in.Project
			input.ScoringInput = in.ScoringInput
		}
		result := EvaluateProject(input, now)
		return ScoringDecision{State: result.State, Project: &result, CanPublish: result.CanPublish, PositiveBlockers: result.PositiveBlockers,
			Metrics: DecisionMetrics{Status: result.Status}, Reasons: result.PositiveBlockers,
			Validation: ValidationResult{Status: StatusNeedsReview, Reasons: result.PositiveBlockers}}
	case "overdraft":
		input := OverdraftInput{ScoringInput: in.ScoringInput}
		if in.Overdraft != nil {
			input = *in.Overdraft
			input.ScoringInput = in.ScoringInput
		}
		result := EvaluateOverdraft(input, now)
		return ScoringDecision{State: result.State, Overdraft: &result, CanPublish: result.CanPublish, PositiveBlockers: result.PositiveBlockers,
			Metrics: DecisionMetrics{Status: result.Status, Score: result.Score}, Reasons: result.PositiveBlockers}
	default:
		return modelUnavailable("scoring_model_not_supported")
	}
}

func modelUnavailable(reason string) ScoringDecision {
	return ScoringDecision{State: DecisionManualReview, Metrics: DecisionMetrics{Status: "not_configured"},
		Reasons: []string{reason}, PositiveBlockers: []string{reason}, Validation: ValidationResult{Status: StatusNeedsReview, Reasons: []string{reason}}}
}
