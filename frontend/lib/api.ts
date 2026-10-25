import { request, requestBlob, newIdemKey } from "./http";
import type {
  AppPage,
  Application,
  Contract,
  FinancialState,
  InfoRequest,
  Offer,
  ProductSuggestion,
  Signature,
  SigningContext,
} from "./types-application";
import type {
  AuthResponse,
  Certificate,
  Challenge,
  Company,
  Overview,
  RegistryCard,
} from "./types";
import type { QueuePage, ReviewCard } from "./types-underwriter";

export { ApiError, isRevisionConflict } from "./http";
export { newIdemKey } from "./http";
export type {
  AuthResponse,
  Certificate,
  Challenge,
  Company,
  Overview,
  RegistryCard,
  SourceStatus,
} from "./types";
export type { Application, Offer, Contract, InfoRequest } from "./types-application";

type Token = string;
type Revision = { expected_revision: number };

const esign = { base: "/esign" };

export const api = {
  login: (email: string, password: string) =>
    request<AuthResponse>("/auth/login", { method: "POST", body: { email, password } }),
  createUser: (email: string, password: string) =>
    request<{ id: string }>("/users", { method: "POST", body: { email, password } }),
  logout: (token: Token) =>
    request<void>("/auth/logout", { method: "POST", token }).catch(() => undefined),
  me: (token: Token) => request<{ id: string; role: string }>("/auth/me", { token }),

  companies: (token: Token) => request<Company[]>("/companies", { token }),
  company: (id: string, token: Token) => request<Company>(`/companies/${id}`, { token }),
  lookup: (inn: string, token: Token) =>
    request<RegistryCard>(`/companies/lookup?inn=${inn}`, { token }),
  createChallenge: (inn: string, token: Token) =>
    request<Challenge>("/companies/challenges", { method: "POST", body: { inn }, token }),
  connect: (challengeId: string, signature: string, token: Token) =>
    request<Company>("/companies", {
      method: "POST",
      body: { challenge_id: challengeId, signature },
      token,
    }),
  overview: (companyId: string, token: Token) =>
    request<Overview>(`/companies/${companyId}/overview`, { token }),
  uploadCompanyCsv: (companyId: string, file: File, token: Token) => {
    const form = new FormData();
    form.append("file", file);
    return request<unknown>(`/companies/${companyId}/financials`, { method: "POST", form, token });
  },

  products: (query: Record<string, string | number | undefined>, token: Token) =>
    request<ProductSuggestion[]>("/credit-products", { query, token }),

  createApp: (companyId: string, token: Token, idem = newIdemKey()) =>
    request<Application>("/applications", {
      method: "POST",
      body: { company_id: companyId },
      token,
      idem,
    }),
  listApps: (companyId: string, token: Token, status?: string) =>
    request<AppPage>("/applications", { query: { company_id: companyId, status }, token }),
  getApp: (id: string, token: Token) => request<Application>(`/applications/${id}`, { token }),
  patchApp: (id: string, body: Record<string, unknown>, token: Token) =>
    request<Application>(`/applications/${id}`, { method: "PATCH", body, token }),
  submitApp: (id: string, revision: number, token: Token, idem = newIdemKey()) =>
    request<Application>(`/applications/${id}/submit`, {
      method: "POST",
      body: { expected_revision: revision },
      token,
      idem,
    }),
  resubmitApp: (id: string, revision: number, token: Token, idem = newIdemKey()) =>
    request<Application>(`/applications/${id}/resubmit`, {
      method: "POST",
      body: { expected_revision: revision },
      token,
      idem,
    }),
  infoRequests: (id: string, token: Token) =>
    request<InfoRequest[]>(`/applications/${id}/info-requests`, { token }),
  answerInfo: (id: string, rid: string, body: Revision & { answer: string }, token: Token) =>
    request<Application>(`/applications/${id}/info-requests/${rid}/answer`, {
      method: "POST",
      body,
      token,
    }),
  offer: (id: string, token: Token) => request<Offer | null>(`/applications/${id}/offer`, { token }),
  contract: (id: string, token: Token) =>
    request<Contract | null>(`/applications/${id}/contract`, { token }),
  document: (docId: string, token: Token) =>
    requestBlob(`/documents/${docId}/content`, { token }),

  appFinancials: (id: string, token: Token) =>
    request<FinancialState>(`/applications/${id}/financials`, { token }),
  fetchFinancials: (id: string, body: Revision & { demo_mode?: string }, token: Token, idem = newIdemKey()) =>
    request<FinancialState>(`/applications/${id}/financials/fetch`, {
      method: "POST",
      body,
      token,
      idem,
    }),
  uploadAppCsv: (id: string, file: File, expectedRevision: number, token: Token) => {
    const form = new FormData();
    form.append("file", file);
    form.append("expected_revision", String(expectedRevision));
    return request<FinancialState>(`/applications/${id}/financials/csv`, {
      method: "POST",
      form,
      token,
      idem: newIdemKey(),
    });
  },
  declareNoHistory: (
    id: string,
    body: Revision & { confirmed: boolean },
    token: Token,
    idem = newIdemKey(),
  ) =>
    request<FinancialState>(`/applications/${id}/financials/no-history`, {
      method: "POST",
      body,
      token,
      idem,
    }),

  signingContext: (
    id: string,
    body: Revision & { offer_id: string; document_id: string },
    token: Token,
  ) => request<SigningContext>(`/applications/${id}/signing-context`, { method: "POST", body, token }),
  signApp: (
    id: string,
    body: { context_id: string; signature: string; terms_accepted: boolean },
    token: Token,
    idem = newIdemKey(),
  ) =>
    request<Signature>(`/applications/${id}/sign`, {
      method: "POST",
      body,
      token,
      idem,
    }),
  signature: (id: string, token: Token) => request<Signature>(`/applications/${id}/signature`, { token }),

  uwQueue: (query: Record<string, string | number | undefined>, token: Token) =>
    request<QueuePage>("/underwriter/applications", { query, token }),
  uwGet: (id: string, token: Token) =>
    request<ReviewCard>(`/underwriter/applications/${id}`, { token }),
  uwPost: (id: string, action: string, body: Record<string, unknown>, token: Token) =>
    request<ReviewCard>(`/underwriter/applications/${id}/${action}`, { method: "POST", body, token }),

  certificates: () => request<{ certificates: Certificate[] }>("/v1/certificates", esign),
  esignSign: (certificateSerial: string, pin: string, data: string) =>
    request<{ signature: string }>("/v1/sign", {
      method: "POST",
      body: { certificate_serial: certificateSerial, pin, data },
      ...esign,
    }),
};
