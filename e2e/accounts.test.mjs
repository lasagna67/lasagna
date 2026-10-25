// E2E для разных демо-аккаунтов: сценарии расходятся ещё до решения сотрудника.
import test from "node:test";
import assert from "node:assert/strict";
import {
  TERMINAL,
  clientLogin,
  req,
  reqOk,
  startApplication,
  staffCard,
  staffLogin,
  until,
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
