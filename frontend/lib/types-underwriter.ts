import type {
  Application,
  CashPlan,
  Collateral,
  Contract,
  InfoRequest,
  Offer,
  PlanReview,
} from "./types-application";

export type FieldErrorResponse = { field: string; code: string; message: string };

export type QueueItem = Application & {
  assignee: { id: string; display_name: string } | null;
};

export type QueuePage = { items: QueueItem[]; next_cursor: string | null };

export type Checks = {
  registry: {
    status: "not_requested" | "ok" | "not_found" | "unavailable" | "invalid";
    checked_at: string | null;
    inn: string | null;
    company_state: "active" | "reorganizing" | "terminated" | null;
    snapshot: unknown;
  };
  credit_bureau: {
    status: "not_requested" | "ok" | "not_found" | "unavailable" | "invalid";
    checked_at: string | null;
    has_credit_history: boolean | null;
    snapshot: unknown;
  };
  financials: {
    data_status: "missing" | "ready" | "no_history";
    source_type: "automatic" | "csv" | null;
    coverage: "complete" | "partial" | null;
  };
};

export type Scoring = {
  status: "completed" | "insufficient_data" | "not_configured" | "failed" | "skipped";
  kind: "cashflow" | "project" | "overdraft" | null;
  score: number | null;
  risk_grade: "low" | "medium" | "high" | "critical" | null;
  model_version: string | null;
  recommendation: "manual_review" | "auto_approved" | "auto_rejected" | null;
  can_publish: boolean;
  positive_blockers: string[];
  calculated_at: string | null;
};

export type StaffCollateralReview = {
  id: string;
  version: number;
  result: "accepted" | "needs_info" | "rejected";
  accepted_value_kopecks: number | null;
  public_comment: string | null;
  reviewed_at: string;
  actual: boolean;
  comment: string;
};

export type StaffPlanReview = {
  id: string;
  version: number;
  result: "accepted" | "needs_info" | "rejected";
  validated_reserve_kopecks: number | null;
  public_comment: string | null;
  reviewed_at: string;
  actual: boolean;
  terms: {
    product_code: string;
    amount_kopecks: number;
    term_months: number | null;
    collateral_mode: string;
  };
  data_revision: number;
  comment: string;
};

export type DecisionCard = {
  id: string;
  version: number;
  outcome: "approved" | "counter_offer" | "rejected";
  offer: Offer | null;
  contract: Contract | null;
  reason_code: string | null;
  reasons: string[];
  comment: string | null;
  decided_by: string | null;
  created_at: string;
};

export type HistoryEvent = {
  id: number;
  type: string;
  from_status: string | null;
  to_status: string;
  message: string | null;
  actor_id: string | null;
  created_at: string;
};

export type CashPlanAssessment = {
  required: boolean | null;
  signals: string[];
  terms: {
    product_code: string;
    amount_kopecks: number;
    term_months: number | null;
    collateral_mode: string;
  } | null;
  review_current: boolean;
  blockers: FieldErrorResponse[];
};

export type ReviewCard = QueueItem & {
  cash_plan: CashPlan | null;
  plan_review: PlanReview | null;
  collateral: Collateral | null;
  offer: Offer | null;
  contract: Contract | null;
  info_requests: InfoRequest[];
  collateral_reviews: StaffCollateralReview[];
  cash_plan_reviews: StaffPlanReview[];
  cash_plan_assessment: CashPlanAssessment;
  decisions: DecisionCard[];
  history: HistoryEvent[];
  decision_blockers: FieldErrorResponse[];
  checks: Checks;
  scoring: Scoring;
};
