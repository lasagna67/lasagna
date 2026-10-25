// E2E для разных демо-аккаунтов: сценарии расходятся ещё до решения сотрудника.
import test from "node:test";
import assert from "node:assert/strict";
import {
  TERMINAL,
  clientLogin,
  idem,
  lastCompletedMonth,
  req,
  reqOk,
  startApplication,
  staffCard,
  staffLogin,
  until,
  uploadCsv,
} from "./helpers.mjs";

test(
  "demo.ip.overdue: пайплайн отклоняет заявку без участия сотрудника",
  { timeout: 120_000 },
  async (t) => {
    const client = await clientLogin("ip.overdue");
    const { id } = await startApplication(client);
    t.diagnostic(`заявка ${id} отправлена, ждём отказа пайплайна`);

    const app = await until(
      async () => {
        const a = await reqOk(`/applications/${id}`, { token: client });
        return TERMINAL.has(a.status) ? a : null;
      },
      { timeout: 90_000, label: "terminal status for overdue" },
    );
    assert.equal(app.status, "rejected");

    const offer = await req(`/applications/${id}/offer`, { token: client });
    assert.ok(offer.status === 404 || offer.data == null, "отклонённой заявке не выдаётся предложение");

    const staff = await staffLogin();
    const card = await staffCard(id, staff);
    assert.equal(card.status, "rejected");
    assert.deepEqual(card.available_actions, [], "после отказа действий у сотрудника нет");
    t.diagnostic("отклонено пайплайном, сотрудник действий не получает");
  },
);

test(
  "demo.ooo.thin: контрпредложение сотрудника видно клиенту с изменёнными условиями",
  { timeout: 120_000 },
  async (t) => {
    const client = await clientLogin("ooo.thin");
    const staff = await staffLogin();
    const { id } = await startApplication(client);
    t.diagnostic(`заявка ${id} отправлена, ждём manual_review`);

    let card = await until(
      async () => {
        const c = await staffCard(id, staff);
        return c.available_actions.includes("take") ? c : null;
      },
      { timeout: 90_000, label: "take available for thin" },
    );
    assert.deepEqual(card.decision_blockers, [], "у тонкого профиля нет блокеров решения");

    const me = await reqOk("/auth/me", { token: staff });
    card = await reqOk(`/underwriter/applications/${id}/take`, {
      method: "POST",
      token: staff,
      body: { expected_revision: card.revision },
    });
    assert.equal(card.assignee?.id, me.id, "заявка закреплена за сотрудником");

    card = await reqOk(`/underwriter/applications/${id}/decision`, {
      method: "POST",
      token: staff,
      body: {
        expected_revision: card.revision,
        outcome: "counter_offer",
        terms: {
          product_code: "working_capital",
          amount_kopecks: 250_000_000,
          term_months: 24,
          annual_rate_bps: 2100,
        },
      },
    });
    assert.equal(card.status, "counter_offer");

    const [app, offer, contract] = await Promise.all([
      reqOk(`/applications/${id}`, { token: client }),
      reqOk(`/applications/${id}/offer`, { token: client }),
      reqOk(`/applications/${id}/contract`, { token: client }),
    ]);
    assert.equal(app.status, "counter_offer");
    assert.equal(offer.outcome, "counter_offer");
    assert.equal(offer.amount_kopecks, 250_000_000, "клиент видит уменьшенную сумму");
    assert.equal(offer.annual_rate_bps, 2100);
    assert.deepEqual(offer.changed_fields, ["amount_kopecks"]);
    assert.ok(contract.id, "контрпредложению создан договор");
    t.diagnostic("клиент видит контрпредложение и договор");
  },
);

// Общий сценарий «нет финансовых данных → CSV → submit» для ООО и ИП:
// источник не дал данных, клиент сам загружает выгрузку и подаёт заявку.
async function csvReserveFlow(scenario, t) {
  const client = await clientLogin(scenario);
  const companies = await reqOk("/companies", { token: client });
  const inn = companies[0].inn;
  const app = await reqOk("/applications", {
    method: "POST",
    token: client,
    idempotencyKey: idem(),
    body: { company_id: companies[0].id },
  });
  const patched = await reqOk(`/applications/${app.id}`, {
    method: "PATCH",
    token: client,
    body: {
      expected_revision: app.revision,
      request: {
        purpose: "working_capital",
        purpose_description: null,
        amount_kopecks: 300_000_000,
        need_period: { unit: "months", value: 24 },
        product_code: "working_capital",
        term_months: 24,
        collateral_mode: "none",
      },
    },
  });
  const fin = await reqOk(`/applications/${app.id}/financials/fetch`, {
    method: "POST",
    token: client,
    idempotencyKey: idem(),
    body: { expected_revision: patched.revision },
  });
  assert.equal(fin.data_status, "missing", "источник не дал данных");
  assert.equal(fin.allow_csv_upload, true, "после неудачи открыта загрузка CSV");
  t.diagnostic(`fetch: ${fin.source_status}, CSV открыт`);

  const csv = [
    "inn,month,income,expenses,taxes,owner_withdrawals",
    `${inn},${lastCompletedMonth()},500000000,310000000,30000000,40000000`,
  ].join("\n");
  const uploaded = await uploadCsv(app.id, fin.revision, client, csv);
  assert.equal(uploaded.data_status, "ready", "CSV принят");
  assert.equal(uploaded.snapshot.rows.length, 1, "в снимке один месяц");
  assert.equal(uploaded.allow_csv_upload, false, "снимок принят, резерв закрыт");
  t.diagnostic("CSV загружен, снимок готов");

  const submitted = await reqOk(`/applications/${app.id}/submit`, {
    method: "POST",
    token: client,
    idempotencyKey: idem(),
    body: { expected_revision: uploaded.revision },
  });
  assert.equal(submitted.status, "submitted", "заявка уходит с CSV-финансами");
  t.diagnostic("заявка отправлена");
}

test(
  "demo.ooo.no-data: ООО принимает CSV с оборотами на шаге отправки",
  { timeout: 120_000 },
  async (t) => {
    await csvReserveFlow("ooo.no-data", t);
  },
);

test(
  "demo.ip.no-data: ИП принимает CSV с оборотами на шаге отправки",
  { timeout: 120_000 },
  async (t) => {
    await csvReserveFlow("ip.no-data", t);
  },
);
