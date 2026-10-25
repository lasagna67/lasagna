// E2E полного пути взятия кредита: все действия сотрудника — проверка залога,
// денежного плана, повторные проверки и все три решения — до подписи клиентом.
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
  "demo.ooo.good: залог → проверка залога → одобрение → подпись",
  { timeout: 180_000 },
  async (t) => {
    const client = await clientLogin("ooo.good");
    const staff = await staffLogin();
    const { id } = await startApplication(client, {
      request: {
        purpose: "equipment",
        amount_kopecks: 500_000_000,
        term_months: 36,
        product_code: "equipment",
        collateral_mode: "pledged",
      },
      extras: [
        {
          collateral: {
            type: "equipment",
            ownership: "existing",
            description: "Токарный станок",
            declared_value_kopecks: 750_000_000,
          },
        },
      ],
    });
    t.diagnostic(`заявка ${id}: оборудование под залог`);

    let card = await takeWait(id, staff, "take (equipment)");
    card = await staffPost(id, staff, "take", { expected_revision: card.revision });
    assert.ok(
      card.decision_blockers.some((b) => b.field.startsWith("collateral")),
      "непроверенный залог блокирует положительное решение",
    );

    await staffPost(id, staff, "collateral-review", {
      expected_revision: card.revision,
      result: "accepted",
      accepted_value_kopecks: 750_000_000,
      comment: "Оценка соответствует рынку",
      public_comment: "Залог принят",
    });
    card = await staffCard(id, staff); // collateral-review возвращает не карточку
    assert.equal(card.collateral_reviews[0]?.result, "accepted");
    assert.deepEqual(card.decision_blockers, [], "после проверки залога блокеров нет");

    card = await staffPost(id, staff, "decision", {
      expected_revision: card.revision,
      outcome: "approved",
      terms: {
        product_code: "equipment",
        amount_kopecks: 500_000_000,
        term_months: 36,
        annual_rate_bps: 1850,
      },
    });
    assert.equal(card.status, "approved");

    const [offer, contract, app] = await Promise.all([
      reqOk(`/applications/${id}/offer`, { token: client }),
      reqOk(`/applications/${id}/contract`, { token: client }),
      reqOk(`/applications/${id}`, { token: client }),
    ]);
    assert.equal(offer.outcome, "approved");
    assert.ok(contract.id, "договор сформирован");

    const signature = await signLoan(client, id, {
      revision: app.revision,
      offerId: offer.id,
      documentId: contract.id,
    });
    assert.ok(signature.signed_document_id, "контракт подписан");
    const final = await reqOk(`/applications/${id}`, { token: client });
    assert.equal(final.status, "signing", "после подписи — signing");
    t.diagnostic("залог проверен, кредит подписан");
  },
);

test(
  "demo.ip.good: «Старт» — залог и денежный план → одобрение и договор",
  { timeout: 180_000 },
  async (t) => {
    const client = await clientLogin("ip.good");
    const staff = await staffLogin();
    // до старта продаж (2027-01) у нового бизнеса доход нулевой; после —
    // с запасом: стресс-модель берёт 0.9·доход и требует ненулевого остатка
    const forecast = months("2026-11", 24).map((month) => ({
      month,
      income_kopecks: month < "2027-01" ? 0 : 6_000_000,
      expenses_kopecks: 2_000_000,
    }));
    const { id } = await startApplication(client, {
      request: {
        purpose: "business_start",
        purpose_description: "Запуск мастерской",
        amount_kopecks: 30_000_000,
        term_months: 24,
        product_code: "start",
        collateral_mode: "pledged",
      },
      extras: [
        {
          collateral: {
            type: "equipment",
            ownership: "existing",
            description: "Оборудование мастерской",
            declared_value_kopecks: 50_000_000,
          },
        },
        {
          product_details: {
            project_description: "Запуск мастерской",
            cost_items: [
              { name: "Оборудование", amount_kopecks: 35_000_000 },
              { name: "Ремонт помещения", amount_kopecks: 8_000_000 },
            ],
            // кредит 30 + свои 13 = стоимость проекта 43; свои — 30,2%
            own_funds_kopecks: 13_000_000,
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
    t.diagnostic(`заявка ${id}: «Старт» с залогом и денежным планом`);

    let card = await takeWait(id, staff, "take (start)");
    card = await staffPost(id, staff, "take", { expected_revision: card.revision });
    card = await staffCard(id, staff);
    assert.ok(card.available_actions.includes("review_cash_plan"), "план требует заключения");
    assert.ok(card.available_actions.includes("review_collateral"), "залог требует проверки");

    await staffPost(id, staff, "collateral-review", {
      expected_revision: card.revision,
      result: "accepted",
      accepted_value_kopecks: 50_000_000,
      comment: "Оборудование соответствует сумме",
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
    assert.equal(card.cash_plan_reviews[0]?.result, "accepted");
    assert.deepEqual(card.decision_blockers, [], "после залога и плана блокеров нет");

    card = await staffPost(id, staff, "decision", {
      expected_revision: card.revision,
      outcome: "approved",
      terms: {
        product_code: "start",
        amount_kopecks: 30_000_000,
        term_months: 24,
        annual_rate_bps: 2400,
      },
    });
    assert.equal(card.status, "approved");

    const [offer, contract] = await Promise.all([
      reqOk(`/applications/${id}/offer`, { token: client }),
      reqOk(`/applications/${id}/contract`, { token: client }),
    ]);
    assert.equal(offer.outcome, "approved");
    assert.equal(offer.annual_rate_bps, 2400);
    assert.ok(contract.id, "договор сформирован");
    // Подпись «Старт» бэкенд пока блокирует (validateReview:SOURCE_RECHECK_REQUIRED
    // для start/overdraft — нет календарных проверок P14/P12), поэтому цикл
    // для этого продукта завершается договором.
    t.diagnostic("«Старт» одобрен, договор сформирован (подпись продукта закрыта на бэке)");
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
