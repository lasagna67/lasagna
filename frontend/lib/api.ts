export class ApiError extends Error {
  status: number;

  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

type RequestOptions = { method?: "GET" | "POST" | "DELETE"; body?: unknown; token?: string };

async function request<T>(path: string, opts: RequestOptions = {}, base = "/api/v0"): Promise<T> {
  let res: Response;
  try {
    res = await fetch(`${base}${path}`, {
      method: opts.method ?? "GET",
      headers: {
        ...(opts.body ? { "Content-Type": "application/json" } : {}),
        ...(opts.token ? { Authorization: `Bearer ${opts.token}` } : {}),
      },
      body: opts.body ? JSON.stringify(opts.body) : undefined,
    });
  } catch {
    throw new ApiError(0, "Сервис недоступен, попробуйте позже");
  }
  if (!res.ok) {
    const data = (await res.json().catch(() => null)) as
      | { message?: string; error?: { message?: string } }
      | null;
    throw new ApiError(res.status, data?.message ?? data?.error?.message ?? "Ошибка сервиса");
  }
  return res.json() as Promise<T>;
}

export type SourceStatus = "ok" | "not_found" | "unavailable";
export type EntityType = "legal_entity" | "individual_entrepreneur";

export type AuthResponse = {
  access_token: string;
  expires_at: string;
  user: { id: string; role: string };
};

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

export type Company = RegistryCard & {
  id: string;
  primary_okved?: string;
  additional_okveds: string[];
  membership: { position: string; can_sign: boolean };
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
      monthly_data: {
        month: string;
        income: number;
        expenses: number;
        free_cash: number;
      }[];
    } | null;
  };
};

export const api = {
  login: (email: string, password: string) =>
    request<AuthResponse>("/auth/login", { method: "POST", body: { email, password } }),
  createUser: (email: string, password: string) =>
    request<{ id: string }>("/users", { method: "POST", body: { email, password } }),
  logout: (token: string) =>
    request<void>("/auth/logout", { method: "POST", token }).catch(() => undefined),
  me: (token: string) => request<{ id: string; role: string }>("/auth/me", { token }),
  companies: (token: string) => request<Company[]>("/companies", { token }),
  lookup: (inn: string, token: string) =>
    request<RegistryCard>(`/companies/lookup?inn=${inn}`, { token }),
  createChallenge: (inn: string, token: string) =>
    request<Challenge>("/companies/challenges", { method: "POST", body: { inn }, token }),
  connect: (challengeId: string, signature: string, token: string) =>
    request<Company>("/companies", {
      method: "POST",
      body: { challenge_id: challengeId, signature },
      token,
    }),
  overview: (companyId: string, token: string) =>
    request<Overview>(`/companies/${companyId}/overview`, { token }),
  certificates: () => request<{ certificates: Certificate[] }>("/v1/certificates", {}, "/esign"),
  sign: (certificateSerial: string, pin: string, data: string) =>
    request<{ signature: string }>(
      "/v1/sign",
      { method: "POST", body: { certificate_serial: certificateSerial, pin, data } },
      "/esign",
    ),
};
