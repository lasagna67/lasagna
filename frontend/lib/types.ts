export type SourceStatus = "ok" | "not_found" | "unavailable";
export type EntityType = "legal_entity" | "individual_entrepreneur";

export type RegistryCard = {
  inn: string;
  entity_type: EntityType;
  name: string;
  registration_date: string;
  business_status: string;
  termination_date?: string;
  okved?: { code: string; name: string }[];
  head?: { full_name: string; position: string } | null;
};

export type Membership = { position: string; can_sign: boolean };

export type Company = RegistryCard & {
  id: string;
  primary_okved?: string;
  additional_okveds: string[];
  membership: Membership;
};

export type Challenge = {
  challenge_id: string;
  data: string;
  expires_at: string;
  company: RegistryCard;
};

export type Certificate = {
  serial: string;
  subject: { full_name: string; inn: string; position?: string };
  status: string;
};

export type Loan = {
  loan_type: string;
  status: string;
  opened_date: string;
  outstanding_amount: number;
  monthly_payment: number;
  overdue_amount: number;
  overdue_days: number;
};

export type Overview = {
  company_id: string;
  registry: { status: SourceStatus; data: RegistryCard | null };
  credit_history: {
    status: SourceStatus;
    data: {
      report_date: string;
      report: { has_credit_history: boolean; history_months: number; loans: Loan[] };
    } | null;
  };
  financials: {
    status: SourceStatus;
    data: {
      period?: { from: string; to: string };
      monthly_data: { month: string; income: number; expenses: number; free_cash: number }[];
    } | null;
  };
};

export type AuthResponse = {
  access_token: string;
  expires_at: string;
  user: { id: string; role: string };
};
