// E2E полного пути взятия кредита: проверка залога и денежного плана,
// повторные проверки, все решения и выдача (issued) — включая «Старт» и овердрафт.
import test from "node:test";
import assert from "node:assert/strict";
import {
  clientLogin,
  months,
  req,
  reqOk,
  signLoan,
  startApplication,
  staffCard,
  staffLogin,
  staffPost,
  until,
  waitForIssued,
} from "./helpers.mjs";

const takeWait = (id, staff, label) =>
  until(
    async () => {
      const c = await staffCard(id, staff);
      return c.available_actions.includes("take") ? c : null;
    },
    { timeout: 90_000, label },
  );

test(
  "demo.ooo.good: «Старт» — залог, денежный план, одобрение, подпись, выдача",
  { timeout: 180_000 },
  async (t) => {
    const client = await clientLogin("ooo.good");
    const staff = await staffLogin();
    const forecast = months("2026-11", 24).map((month) => ({
      month,
      income_kopecks: month < "2027-01" ? 0 : 6_000_000,
      expenses_kopecks: 2_000_000,
    }));
    const { id } = await startApplication(client, {
      request: {
        purpose: "business_start",
        purpose_description: "Запуск цеха",
        amount_kopecks: 10_000_000,
        need_period: { unit: "months", value: 24 },
        product_code: "start",
        term_months: 24,
        collateral_mode: "pledged",
      },
      extras: [
        {
          collateral: {
            type: "equipment",
            ownership: "existing",
            description: "Токарный станок",
            declared_value_kopecks: 20_000_000,
          },
        },
        {
          product_details: {
            project_description: "Запуск цеха",
            cost_items: [
              { name: "Оборудование", amount_kopecks: 10_000_000 },
              { name: "Ремонт", amount_kopecks: 5_000_000 },
            ],
            own_funds_kopecks: 5_000_000,
            sales_start_month: "2027-01",
          },
        },
        {
          cash_plan: {
            kind: "project",
            first_month: "2026-11",
            own_reserve_kopecks: 10_000_000,
            forecast,
          },
        },
        { cash_plan: { confirmed: true } },
      ],
    });
    t.diagnostic(`заявка ${id}: «Старт» со залогом и планом`);

    let card = await takeWait(id, staff, "take (start)");
    card = await staffPost(id, staff, "take", { expected_revision: card.revision });
    await staffPost(id, staff, "collateral-review", {
      expected_revision: card.revision,
      result: "accepted",
      accepted_value_kopecks: 20_000_000,
      comment: "Оценка соответствует рынку",
    });
    card = await staffCard(id, staff);
    await staffPost(id, staff, "cash-plan-review", {
      expected_revision: card.revision,
      result: "accepted",
      validated_reserve_kopecks: 10_000_000,
      comment: "План реалистичен, резерв подтверждён",
      public_comment: "Денежный план принят",
    });
    card = await staffCard(id, staff);

    // заключения изменили данные заявки — пересчитываем скоринг
    await staffPost(id, staff, "retry-scoring", { expected_revision: card.revision });
    card = await until(
      async () => {
        const c = await staffCard(id, staff);
        return c.status === "manual_review" && c.decision_blockers.length === 0 ? c : null;
      },
      { timeout: 90_000, label: "после пересчёта скоринга блокеров нет" },
    );

    card = await staffPost(id, staff, "decision", {
      expected_revision: card.revision,
      outcome: "approved",
      terms: {
        product_code: "start",
        amount_kopecks: 10_000_000,
        term_months: 24,
        annual_rate_bps: 2400,
      },
    });
    assert.equal(card.status, "approved");

    const [offer, contract, app] = await Promise.all([
      reqOk(`/applications/${id}/offer`, { token: client }),
      reqOk(`/applications/${id}/contract`, { token: client }),
      reqOk(`/applications/${id}`, { token: client }),
    ]);
    assert.equal(offer.annual_rate_bps, 2400);
    assert.ok(contract.id, "договор сформирован");

    const signature = await signLoan(client, id, {
      revision: app.revision,
      offerId: offer.id,
      documentId: contract.id,
    });
    assert.ok(signature.signed_document_id, "контракт подписан");
    const issued = await waitForIssued(client, id);
    assert.equal(issued.status, "issued", "кредит по «Старту» выдан");
    t.diagnostic("«Старт» подписан и выдан");
  },
);


test(
  "demo.ooo.no-history: повторные проверки, пересчёт скоринга и ручной отказ",
  { timeout: 180_000 },
  async (t) => {
    const client = await clientLogin("ooo.no-history");
    const staff = await staffLogin();
    const { id } = await startApplication(client);
    t.diagnostic(`заявка ${id} отправлена`);

    let card = await takeWait(id, staff, "take (no-history)");
    card = await staffPost(id, staff, "take", { expected_revision: card.revision });
    assert.ok(card.available_actions.includes("retry_checks"), "повторные проверки доступны");
    assert.ok(card.available_actions.includes("retry_scoring"), "пересчёт скоринга доступен");

    await staffPost(id, staff, "retry-checks", {
      expected_revision: card.revision,
      sources: ["registry", "credit_bureau"],
    });
    const afterChecks = await until(
      async () => {
        const c = await staffCard(id, staff);
        return c.revision > card.revision ? c : null;
      },
      { timeout: 60_000, label: "retry-checks обновил заявку" },
    );
    assert.equal(afterChecks.checks.registry.status, "ok");
    assert.equal(afterChecks.checks.credit_bureau.status, "ok");
    t.diagnostic(`проверки повторены, v${afterChecks.revision}`);

    await staffPost(id, staff, "retry-scoring", { expected_revision: afterChecks.revision });
    const scored = await until(
      async () => {
        const c = await staffCard(id, staff);
        return c.revision > afterChecks.revision &&
          c.status === "manual_review" &&
          c.available_actions.includes("decide")
          ? c
          : null;
      },
      { timeout: 90_000, label: "пересчёт скоринга завершён" },
    );
    assert.equal(scored.scoring.status, "completed");
    t.diagnostic(`скоринг пересчитан: ${scored.scoring.score}, v${scored.revision}`);

    const decided = await staffPost(id, staff, "decision", {
      expected_revision: scored.revision,
      outcome: "rejected",
      reason_code: "LOW_SCORE",
      reasons: ["Скоринг ниже порога банка", "Недостаточный запас финансовых средств"],
    });
    assert.equal(decided.status, "rejected");

    const [app, offer] = await Promise.all([
      reqOk(`/applications/${id}`, { token: client }),
      req(`/applications/${id}/offer`, { token: client }),
    ]);
    assert.equal(app.status, "rejected", "клиент видит отказ");
    assert.ok(offer.status === 404 || offer.data == null, "отказанный заявке нет предложения");
    const finalCard = await staffCard(id, staff);
    assert.deepEqual(finalCard.available_actions, [], "после отказа действий нет");
    t.diagnostic("проверки повторены, скоринг пересчитан, отказ показан клиенту");
  },
);

test(
  "demo.ooo.good: овердрафт — одобрение, подпись и открытие лимита",
  { timeout: 180_000 },
  async (t) => {
    const client = await clientLogin("ooo.good");
    const staff = await staffLogin();
    const { id } = await startApplication(client, {
      request: {
        purpose: "cash_gap",
        purpose_description: null,
        amount_kopecks: 10_000_000,
        need_period: { unit: "days", value: 60 },
        product_code: "overdraft",
        term_months: null,
        collateral_mode: "none",
      },
    });
    t.diagnostic(`заявка ${id}: овердрафт 100 000 ₽, транш до 60 дней`);

    let card = await takeWait(id, staff, "take (overdraft)");
    card = await staffPost(id, staff, "take", { expected_revision: card.revision });
    assert.deepEqual(card.decision_blockers, [], "блокеров решения нет");

    card = await staffPost(id, staff, "decision", {
      expected_revision: card.revision,
      outcome: "approved",
      terms: {
        product_code: "overdraft",
        amount_kopecks: 10_000_000,
        annual_rate_bps: 2150,
        agreement_term_months: 12,
        max_tranche_days: 60,
      },
    });
    assert.equal(card.status, "approved");

    const [offer, contract, app] = await Promise.all([
      reqOk(`/applications/${id}/offer`, { token: client }),
      reqOk(`/applications/${id}/contract`, { token: client }),
      reqOk(`/applications/${id}`, { token: client }),
    ]);
    assert.equal(offer.outcome, "approved");
    assert.equal(offer.term_months, null, "у овердрафта нет срока кредита");
    assert.equal(offer.agreement_term_months, 12);
    assert.equal(offer.max_tranche_days, 60);
    assert.ok(contract.id, "договор сформирован");

    const signature = await signLoan(client, id, {
      revision: app.revision,
      offerId: offer.id,
      documentId: contract.id,
    });
    assert.ok(signature.signed_document_id, "лимит подписан");
    const issued = await waitForIssued(client, id);
    assert.equal(issued.status, "issued", "лимит открыт");
    t.diagnostic("овердрафт подписан, лимит открыт");
  },
);
