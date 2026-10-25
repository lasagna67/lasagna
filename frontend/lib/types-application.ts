export type AppStatus =
  | "draft"
  | "submitted"
  | "validation"
  | "enrichment"
  | "scoring"
  | "manual_review"
  | "info_requested"
  | "approved"
  | "counter_offer"
  | "signing"
  | "issued"
  | "rejected"
  | "cancelled"
  | "expired";

export type NeedPeriod = { unit: "months" | "days" | "unknown"; value: number | null };

export type RequestFields = {
  purpose: string | null;
  purpose_description: string | null;
  amount_kopecks: number | null;
  need_period: NeedPeriod;
  product_code: string | null;
  term_months: number | null;
  collateral_mode: "none" | "pledged";
};

export type FinancialRow = {
  month: string;
  income_kopecks: number;
  expenses_kopecks: number;
  taxes_kopecks: number;
  owner_withdrawals_kopecks: number;
  free_cash_kopecks: number;
};

export type FinancialState = {
  revision: number;
  data_revision: number;
  financial_cycle_id: number;
  source_status: "not_requested" | "ok" | "not_found" | "unavailable" | "invalid";
  data_status: "missing" | "ready" | "no_history";
  source_type: "automatic" | "csv" | null;
  checked_at: string | null;
  snapshot: {
    period_from: string;
    period_to: string;
    coverage: "complete" | "partial";
    provenance: { source_type: string; file_name: string | null };
    rows: FinancialRow[];
  } | null;
  quality: {
    as_of_date: string;
    window_from: string;
    window_to: string;
    n_months: number;
    coverage: string;
    stale: boolean;
  } | null;
  allow_csv_upload: boolean;
  allow_no_history: boolean;
  required_refresh: boolean;
  manual_required: boolean;
  issues: { code: string; field: string; message: string }[];
};

export type Project = {
  project_description: string | null;
  cost_items: { name: string; amount_kopecks: number }[];
  project_cost_kopecks: number | null;
  own_funds_kopecks: number | null;
  sales_start_month: string | null;
};

export type CashPlan = {
  kind: "project" | "liquidity";
  first_month: string;
  own_reserve_kopecks: number;
  forecast: { month: string; income_kopecks: number; expenses_kopecks: number }[];
  confirmed: boolean;
  confirmed_at: string | null;
};

export type CollateralReview = {
  result: "accepted" | "needs_info" | "rejected";
  accepted_value_kopecks: number | null;
  public_comment: string | null;
  reviewed_by: string;
  reviewed_at: string;
  actual: boolean;
};

export type Collateral = {
  revision: number;
  type: "real_estate" | "equipment" | "vehicle";
  ownership: "existing" | "acquired";
  description: string;
  declared_value_kopecks: number;
  review?: CollateralReview | null;
};

export type PlanReview = {
  result: "accepted" | "needs_info" | "rejected";
  validated_reserve_kopecks: number | null;
  public_comment: string | null;
  reviewed_at: string;
  actual: boolean;
};

export type Offer = {
  id: string;
  version: number;
  outcome: "approved" | "counter_offer";
  product_code: string;
  product_name: string;
  amount_kopecks: number;
  term_months: number | null;
  annual_rate_bps: number;
  monthly_payment_kopecks: number | null;
  agreement_term_months: number | null;
  max_tranche_days: number | null;
  collateral_required: boolean;
  changed_fields: string[];
  reasons: string[];
  valid_until: string | null;
  created_at: string;
};

export type Contract = {
  id: string;
  version: number;
  offer_id: string;
  file_name: string;
  signed_at: string | null;
  signature_document_id: string | null;
  created_at: string;
};

export type Application = {
  id: string;
  company_id: string;
  applicant_id: string;
  company: { id: string; name: string; inn: string };
  applicant: { id: string; display_name: string };
  number: number;
  revision: number;
  data_revision: number;
  status: AppStatus;
  request: RequestFields;
  product_details: Project | null;
  financials: FinancialState;
  collateral: Collateral | null;
  cash_plan: CashPlan | null;
  plan_review: PlanReview | null;
  editable_fields: string[];
  available_actions: string[];
  created_at: string;
  updated_at: string;
  submitted_at: string | null;
};

export type AppPage = { items: Application[]; next_cursor: string | null };

export type InfoRequest = {
  id: string;
  message: string;
  editable_fields: string[];
  status: "open" | "answered" | "cancelled";
  answer: string | null;
  requested_at: string;
  answered_at: string | null;
};

export type Product = {
  id: string;
  code: string;
  name: string;
  description: string;
  purposes: string[];
  min_amount_kopecks: number;
  max_amount_kopecks: number;
  min_term_months: number;
  max_term_months: number;
  base_rate: number | null;
  collateral_mode: "not_supported" | "optional" | "required";
  repayment_type: "annuity" | "overdraft";
  max_drawdown_days: number | null;
  min_business_age_months: number;
  is_active: boolean;
};

export type ProductSuggestion = {
  product: Product;
  product_code: string;
  eligibility: "eligible" | "needs_details" | "ineligible";
  reasons: { code: string; message: string }[];
};

export type SigningContext = {
  context_id: string;
  expected_revision: number;
  offer_id: string;
  document_id: string;
  document_sha256: string;
  certificate_serial: string;
  data: string;
  expires_at: string;
};

export type Signature = {
  id: string;
  signed_document_id: string | null;
  signed_at: string;
  signature: string;
  certificate: {
    serial: string;
    owner_full_name: string;
    owner_position: string;
    organization_name: string;
    valid_to: string;
  };
  modeled_effective_date: string;
  terms_accepted: boolean;
};
