// Общие помощники E2E: HTTP-клиент API, логины, поллинг.
// Базовый адрес: E2E_BASE (по умолчанию http://localhost:3000 — фронтенд).
// Тесты ходят через next-прокси /api и /esign, как это делает браузер.
const BASE = process.env.E2E_BASE ?? "http://localhost:3000";
export const API = process.env.E2E_API ?? `${BASE}/api/v0`;
export const ESIGN =
  process.env.E2E_ESIGN ?? (BASE.includes(":8080") ? "http://localhost:8084" : `${BASE}/esign`);

export const CLIENT_PASSWORD = "DemoCabinet2026!";
export const STAFF_EMAIL = "underwriter@lasagna.demo";
export const STAFF_PASSWORD = "DemoUnderwriter2026!";
export const SIGN_PIN = "12345678";

export const TERMINAL = new Set(["rejected", "cancelled", "expired"]);
export const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
export const idem = () => crypto.randomUUID();

export async function req(path, { method = "GET", token, body, idempotencyKey, base = API } = {}) {
  const res = await fetch(base + path, {
    method,
    headers: {
      ...(body ? { "Content-Type": "application/json" } : {}),
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
      ...(idempotencyKey ? { "Idempotency-Key": idempotencyKey } : {}),
    },
    body: body ? JSON.stringify(body) : undefined,
  });
  const text = await res.text();
  let data;
  try {
    data = JSON.parse(text);
  } catch {
    data = text;
  }
  return { status: res.status, ok: res.ok, data };
}

export async function reqOk(path, opts = {}) {
  const r = await req(path, opts);
  if (!r.ok) {
    const label = `${opts.method ?? "GET"} ${path}`;
    throw new Error(`${label} -> ${r.status}: ${JSON.stringify(r.data).slice(0, 400)}`);
  }
  return r.data;
}

export async function reqBytes(path, { token, base = API } = {}) {
  const res = await fetch(base + path, { headers: token ? { Authorization: `Bearer ${token}` } : {} });
  const buf = Buffer.from(await res.arrayBuffer());
  return { status: res.status, ok: res.ok, bytes: buf.length, contentType: res.headers.get("content-type") ?? "" };
}

export async function login(email, password = CLIENT_PASSWORD) {
  const data = await reqOk("/auth/login", { method: "POST", body: { email, password } });
  return data.access_token;
}

export const clientLogin = (scenario) => login(`demo.${scenario}@lasagna.test`);
export const staffLogin = () => login(STAFF_EMAIL, STAFF_PASSWORD);

export async function until(fn, { timeout = 60_000, interval = 1_500, label = "condition" } = {}) {
  const started = Date.now();
  let last;
  for (;;) {
    last = await fn();
    if (last) return last;
    if (Date.now() - started > timeout) throw new Error(`Timeout after ${timeout}ms: ${label}`);
    await sleep(interval);
  }
}

// Создаёт заявку, заполняет параметры, тянет финансы и отправляет.
// Возвращает { id, rev, status, company }.
export async function startApplication(token, overrides = {}) {
  const request = {
    purpose: "working_capital",
    purpose_description: null,
    amount_kopecks: 300_000_000,
    need_period: { unit: "months", value: 24 },
    product_code: "working_capital",
    term_months: 24,
    collateral_mode: "none",
    ...overrides,
  };
  const companies = await reqOk("/companies", { token });
  const company = companies[0];
  const app = await reqOk("/applications", {
    method: "POST",
    token,
    idempotencyKey: idem(),
    body: { company_id: company.id },
  });
  let rev = app.revision;
  const patched = await reqOk(`/applications/${app.id}`, {
    method: "PATCH",
    token,
    body: { expected_revision: rev, request },
  });
  rev = patched.revision;
  const fin = await reqOk(`/applications/${app.id}/financials/fetch`, {
    method: "POST",
    token,
    idempotencyKey: idem(),
    body: { expected_revision: rev },
  });
  rev = fin.revision ?? rev;
  const submitted = await reqOk(`/applications/${app.id}/submit`, {
    method: "POST",
    token,
    idempotencyKey: idem(),
    body: { expected_revision: rev },
  });
  return { id: app.id, rev: submitted.revision, status: submitted.status, company };
}

export async function staffCard(id, staffToken) {
  return reqOk(`/underwriter/applications/${id}`, { token: staffToken });
}

export async function esignSign(certificateSerial, data) {
  return reqOk("/v1/sign", {
    method: "POST",
    base: ESIGN,
    body: { certificate_serial: certificateSerial, pin: SIGN_PIN, data },
  });
}
