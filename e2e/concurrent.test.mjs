// Длинный сценарий: клиент и сотрудник ведут одну заявку одновременно —
// параллельные наблюдения, конфликт ревизий, цикл уточнений, решение и подпись.
import test from "node:test";
import assert from "node:assert/strict";
import {
  clientLogin,
  esignSign,
  idem,
  req,
  reqOk,
  reqBytes,
  startApplication,
  staffCard,
  staffLogin,
  until,
} from "./helpers.mjs";

test(
  "длинный сценарий: клиент и сотрудник работают с одной заявкой одновременно",
  { timeout: 240_000 },
  async (t) => {
    const [client, staff] = await Promise.all([
      clientLogin("ooo.good"),
      staffLogin(),
    ]);
    const staffMe = await reqOk("/auth/me", { token: staff });

    // 1. Клиент оформляет и отправляет заявку.
    const { id } = await startApplication(client);
    t.diagnostic(`заявка ${id} отправлена`);

    // 2. Оба наблюдают за пайплайном одновременно: два параллельных поллинга
    //    до статуса, в котором сотруднику доступен приём заявки.
    const [clientView, staffView] = await Promise.all([
      until(
        async () => {
          const a = await reqOk(`/applications/${id}`, { token: client });
          return a.status === "manual_review" ? a : null;
        },
        { timeout: 90_000, label: "клиент видит manual_review" },
      ),
      until(
        async () => {
          const c = await staffCard(id, staff);
          return c.available_actions.includes("take") ? c : null;
        },
        { timeout: 90_000, label: "сотрудник видит take" },
      ),
    ]);
    assert.equal(clientView.status, "manual_review");
    t.diagnostic("оба видят manual_review");

    // 3. Сотрудник принимает заявку и запрашивает уточнение; клиент параллельно
    //    замечает новый статус и открытый запрос.
    let card = await reqOk(`/underwriter/applications/${id}/take`, {
      method: "POST",
      token: staff,
      body: { expected_revision: staffView.revision },
    });
    assert.equal(card.assignee?.id, staffMe.id);

    card = await reqOk(`/underwriter/applications/${id}/request-info`, {
      method: "POST",
      token: staff,
      body: {
        expected_revision: card.revision,
        message: "Приложите счёт поставщика оборудования",
        editable_fields: ["request.purpose_description"],
      },
    });
    assert.equal(card.status, "info_requested");

    const [appAfterRequest, requests] = await Promise.all([
      reqOk(`/applications/${id}`, { token: client }),
      reqOk(`/applications/${id}/info-requests`, { token: client }),
    ]);
    assert.equal(appAfterRequest.status, "info_requested");
    assert.equal(requests.length, 1);
    assert.equal(requests[0].status, "open");
    assert.equal(requests[0].message, "Приложите счёт поставщика оборудования");
    t.diagnostic("клиент видит запрос уточнения");

    // 4. Клиент отвечает с устаревшей ревизией — получает 409 и обновляет данные,
    //    затем отвечает корректно и переподавляет заявку.
    const stale = await req(`/applications/${id}/info-requests/${requests[0].id}/answer`, {
      method: "POST",
      token: client,
      body: { expected_revision: appAfterRequest.revision - 1, answer: "Счёт №123 приложен" },
    });
    assert.equal(stale.status, 409);
    assert.equal(stale.data.error.code, "REVISION_CONFLICT");
    assert.equal(stale.data.error.current_revision, appAfterRequest.revision);

    await reqOk(`/applications/${id}/info-requests/${requests[0].id}/answer`, {
      method: "POST",
      token: client,
      body: { expected_revision: appAfterRequest.revision, answer: "Счёт №123 приложен" },
    });
    const afterAnswer = await reqOk(`/applications/${id}/info-requests`, { token: client });
    assert.equal(afterAnswer[0].status, "answered", "запрос остаётся видимым после ответа");
    assert.equal(afterAnswer[0].answer, "Счёт №123 приложен");
    const appAnswered = await reqOk(`/applications/${id}`, { token: client });
    assert.equal(appAnswered.status, "info_requested", "ответ не сбрасывает статус заявки");
    const resubmitted = await reqOk(`/applications/${id}/resubmit`, {
      method: "POST",
      token: client,
      idempotencyKey: idem(),
      body: { expected_revision: appAnswered.revision },
    });
    assert.equal(resubmitted.status, "submitted");
    t.diagnostic("ответ принят после 409, заявка переподавлена");

    // 5. Снова два параллельных поллинга: клиент ждёт возврата в рассмотрение,
    //    сотрудник — появления кнопки решения.
    const [, decidedCard] = await Promise.all([
      until(
        async () => {
          const a = await reqOk(`/applications/${id}`, { token: client });
          return a.status === "manual_review" ? a : null;
        },
        { timeout: 90_000, label: "клиент снова видит manual_review" },
      ),
      until(
        async () => {
          const c = await staffCard(id, staff);
          return c.available_actions.includes("decide") ? c : null;
        },
        { timeout: 90_000, label: "сотрудник видит decide" },
      ),
    ]);
    assert.deepEqual(decidedCard.decision_blockers, [], "блокеров решения нет");

    card = await reqOk(`/underwriter/applications/${id}/decision`, {
      method: "POST",
      token: staff,
      body: {
        expected_revision: decidedCard.revision,
        outcome: "approved",
        terms: {
          product_code: "working_capital",
          amount_kopecks: 300_000_000,
          term_months: 24,
          annual_rate_bps: 1990,
        },
      },
    });
    assert.equal(card.status, "approved");
    t.diagnostic("решение опубликовано");

    // 6. Одновременно с двух сторон: клиент читает оферту и договор,
    //    сотрудник — карточку с решением и историей.
    const [appWithOffer, offer, contract, staffFinalCard] = await Promise.all([
      reqOk(`/applications/${id}`, { token: client }),
      reqOk(`/applications/${id}/offer`, { token: client }),
      reqOk(`/applications/${id}/contract`, { token: client }),
      staffCard(id, staff),
    ]);
    assert.equal(appWithOffer.status, "approved");
    assert.equal(offer.outcome, "approved");
    assert.equal(offer.amount_kopecks, 300_000_000);
    assert.equal(offer.annual_rate_bps, 1990);
    assert.ok(contract.id, "договор сформирован");
    assert.equal(staffFinalCard.decisions.at(-1)?.outcome, "approved");
    assert.ok(
      staffFinalCard.history.some((e) => e.to_status === "approved"),
      "история содержит переход в approved",
    );

    // 7. Клиент подписывает; esign и чтение карточки сотрудником идут параллельно.
    const ctx = await reqOk(`/applications/${id}/signing-context`, {
      method: "POST",
      token: client,
      body: {
        expected_revision: appWithOffer.revision,
        offer_id: offer.id,
        document_id: contract.id,
      },
    });
    const [signed, observingStaff] = await Promise.all([
      esignSign(ctx.certificate_serial, ctx.data),
      staffCard(id, staff),
    ]);
    assert.ok(signed.signature, "подпись получена в esign-mock");
    assert.ok(["approved", "counter_offer"].includes(observingStaff.status));

    const signature = await reqOk(`/applications/${id}/sign`, {
      method: "POST",
      token: client,
      idempotencyKey: idem(),
      body: { context_id: ctx.context_id, signature: signed.signature, terms_accepted: true },
    });
    assert.ok(signature.signed_document_id, "заявка подписана");

    // 8. Итоги с двух сторон и документы.
    const [finalApp, finalSignature, finalStaffCard, contractPdf, signedPdf] = await Promise.all([
      reqOk(`/applications/${id}`, { token: client }),
      reqOk(`/applications/${id}/signature`, { token: client }),
      staffCard(id, staff),
      reqBytes(`/documents/${contract.id}/content`, { token: client }),
      reqBytes(`/documents/${signature.signed_document_id}/content`, { token: client }),
    ]);
    assert.equal(finalApp.status, "signing", "после подписи заявка в signing, выдача не заявлена");
    assert.equal(finalStaffCard.status, "signing", "сотрудник видит тот же статус");
    assert.equal(finalSignature.signed_document_id, signature.signed_document_id);
    assert.equal(finalSignature.signed_at, signature.signed_at);
    assert.equal(contractPdf.status, 200);
    assert.ok(contractPdf.bytes > 1000, "договор-PDF скачался");
    assert.equal(signedPdf.status, 200);
    assert.ok(signedPdf.bytes > contractPdf.bytes, "подписанный PDF больше исходного");
    t.diagnostic("обе стороны видят signing, PDF скачиваются");
  },
);
