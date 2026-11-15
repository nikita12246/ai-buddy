import { readFileSync } from 'node:fs';
import { setTimeout as delay } from 'node:timers/promises';
import { expect, test, type Page } from '@playwright/test';

async function holdForRecording() {
  if (process.env.RECORD_DEMO === '1') await delay(2500);
}

async function expectGuestWorkspace(page: Page) {
  await expect(page.getByText('Стажёр Atlas · atlas', { exact: true })).toBeVisible();
  await expect(page.getByLabel('Токен учебной сессии')).toHaveCount(0);
}

test('guest enters automatically and approved GitLab request stays private after reload', async ({ browser, page, request }) => {
  const credentials = Object.fromEntries(
    readFileSync(new URL('../../.env.backend-demo', import.meta.url), 'utf8')
      .trim().split(/\r?\n/).map(line => line.split('=', 2)),
  );
  await page.goto('/');
  await expectGuestWorkspace(page);
  await page.getByRole('button', { name: 'Доступы', exact: true }).click();
  await page.getByRole('button', { name: 'Подготовить заявку на GitLab' }).click();
  await expect(page.getByRole('textbox', { name: 'Вопрос AI-buddy' })).toHaveValue(/Нужен доступ к учебному GitLab/);
  await expect(page.getByRole('button', { name: 'Проверить черновик' })).toHaveCount(0);
  await page.getByRole('button', { name: 'Отправить', exact: true }).click();
  const draftButton = page.getByRole('button', { name: 'Проверить черновик' });
  await expect(draftButton).toBeEnabled();
  await expect(page.locator('.studio-chat-rail').getByRole('heading').first()).toHaveText('Черновик заявки');
  await expect(page.locator('.studio-chat-sources')).toContainText('atlas-training-repo');
  await page.getByRole('button', { name: 'Тёмная тема' }).click();
  await page.locator('.studio-chat-sources').getByRole('button', { name: /^Atlas\./ }).click({ timeout: 10000 });
  const source = page.getByRole('dialog');
  await expect(source.getByText('Фрагмент из ответа')).toBeVisible();
  await expect(source.getByText(/Reporter/)).toBeVisible();
  await expect(source).toHaveCSS('background-color', 'rgb(23, 23, 23)');
  await holdForRecording();
  await source.getByRole('button', { name: 'Закрыть' }).click();
  await page.getByRole('button', { name: 'Светлая тема' }).click();

  await draftButton.click();
  await page.getByLabel('Обоснование').fill('Прочитать README для первого задания');
  await holdForRecording();
  const confirmation = page.waitForResponse(response =>
    response.request().method() === 'POST' && /\/api\/drafts\/[^/]+\/confirm$/.test(new URL(response.url()).pathname));
  await page.getByRole('button', { name: 'Подтвердить заявку' }).click();
  const confirmed = await confirmation;
  expect(confirmed.ok()).toBe(true);
  const { id: operationId } = await confirmed.json() as { id: string };
  const operationURL = `/api/operations/${encodeURIComponent(operationId)}`;
  let requestNumber = '';
  await expect.poll(async () => {
    const response = await page.request.get(operationURL);
    if (!response.ok()) return undefined;
    const operation = await response.json() as { external_id: string | null; service_status: string | null };
    requestNumber = operation.external_id ?? '';
    return operation.service_status;
  }, { timeout: 15000 }).toBe('pending_approval');
  expect(requestNumber).toBeTruthy();

  const card = page.getByRole('listitem').filter({ hasText: requestNumber });
  await expect(card.getByText('На согласовании')).toBeVisible();
  const openSummary = card.getByRole('button', { name: 'Сводка для наставника' });
  const summary = page.getByRole('dialog', { name: 'Сводка для наставника' });
  const summaryText = summary.getByRole('textbox', { name: 'Текст сводки' });
  await openSummary.click();
  await expect(summaryText).toHaveValue(new RegExp(requestNumber));
  await expect(summaryText).toHaveCSS('background-color', 'rgb(255, 255, 255)');
  await expect(summaryText).toHaveCSS('color', 'rgb(23, 23, 23)');
  await summary.getByRole('button', { name: 'Закрыть' }).click();

  await page.getByRole('button', { name: 'Тёмная тема' }).click();
  await openSummary.click();
  await expect(summary).toHaveCSS('background-color', 'rgb(23, 23, 23)');
  await expect(summaryText).toHaveCSS('background-color', 'rgb(36, 36, 36)');
  await expect(summaryText).toHaveCSS('color', 'rgb(255, 255, 255)');
  await summary.getByRole('button', { name: 'Закрыть' }).click();
  await page.getByRole('button', { name: 'Светлая тема' }).click();
  await holdForRecording();
  await page.getByRole('button', { name: 'Открыть памятку по код-ревью' }).click();
  await holdForRecording();
  await page.getByRole('button', { name: 'Отметить как прочитанную' }).click();
  await expect(page.getByText('Памятка прочитана')).toBeVisible();

  const mockURL = `http://127.0.0.1:${process.env.BACKEND_DEMO_MOCK_PORT || '8082'}`;
  const decision = await request.patch(`${mockURL}/requests/${encodeURIComponent(requestNumber)}/decision`, {
    headers: { Authorization: `Bearer ${credentials.BACKEND_DEMO_DECISION_TOKEN}` },
    data: { status: 'approved' },
  });
  expect(decision.ok()).toBe(true);
  await expect(card.getByText('Доступ предоставлен')).toBeVisible({ timeout: 15000 });
  await holdForRecording();
  await page.getByRole('button', { name: 'Открыть инструкцию первой задачи' }).click();
  await expect(page.getByRole('dialog').getByText(/прочитать учебный README каталога/)).toBeVisible();
  await holdForRecording();

  await page.reload();
  await expectGuestWorkspace(page);
  await page.getByRole('button', { name: 'Доступы', exact: true }).click();
  await expect(card.getByText('Доступ предоставлен')).toBeVisible();
  await expect(page.getByText('Памятка прочитана')).toBeVisible();
  await holdForRecording();

  const otherSession = await browser.newContext();
  try {
    const other = await otherSession.newPage();
    await other.goto(new URL('/', page.url()).href);
    await expectGuestWorkspace(other);
    await expect(other.getByText(/Учебная база знаний/)).toBeVisible();
    await expect(other.getByPlaceholder('Спросить AI-buddy')).toBeVisible();
    const search = other.getByRole('textbox', { name: 'Найти документ', exact: true });
    await search.fill('Первое учебное задание');
    await search.press('Enter');
    await expect(other.getByText('Найдено: 1')).toBeVisible();
    await other.getByRole('button', { name: 'Тёмная тема' }).click();
    const documentCard = other.getByRole('button', { name: 'Atlas. Первое учебное задание', exact: true });
    const cardPadding = { x: 12, y: 36 };
    await documentCard.hover({ position: cardPadding });
    await expect(documentCard).toHaveCSS('border-top-color', 'rgb(0, 170, 255)');
    await expect(documentCard.locator('.knowledge-document-arrow')).toHaveCSS('background-color', 'rgb(0, 170, 255)');
    await documentCard.click({ position: cardPadding });
    const documentDialog = other.getByRole('dialog');
    await expect(documentDialog.getByText(/прочитать учебный README каталога/)).toBeVisible();
    await documentDialog.getByRole('button', { name: 'Закрыть' }).click();
    await documentCard.press('Enter');
    await expect(documentDialog).toBeVisible();
    await documentDialog.getByRole('button', { name: 'Закрыть' }).click();

    const navigation = other.getByRole('navigation');
    await navigation.getByRole('button', { name: 'Главная', exact: true }).click();
    const recent = other.locator('.studio-recent-document').filter({ hasText: 'Atlas. Первое учебное задание' });
    await recent.hover();
    await expect(recent).toHaveCSS('background-color', 'rgb(36, 36, 36)');
    await expect(recent.getByText('Atlas. Первое учебное задание')).toHaveCSS('color', 'rgb(255, 255, 255)');
    await other.getByRole('button', { name: 'Светлая тема' }).click();
    await expect(recent).toHaveCSS('border-top-color', 'rgb(230, 230, 230)');
    await recent.hover();
    await expect(recent).toHaveCSS('background-color', 'rgb(244, 244, 244)');
    await navigation.getByRole('button', { name: 'Проект и инструменты', exact: true }).click();
    await expect(other.getByRole('heading', { name: 'GitLab', exact: true })).toBeVisible();
    await other.getByRole('button', { name: 'Доступы', exact: true }).click();
    await expect(other.getByText('Подтверждённых заявок пока нет.')).toBeVisible();
    expect((await other.request.get(new URL(operationURL, page.url()).href)).status()).toBe(404);
    await other.getByRole('navigation').getByRole('button', { name: 'Моя стажировка', exact: true }).click();
    await other.getByRole('button', { name: /Изучить code review/ }).click();
    const guide = other.getByRole('dialog', { name: 'Памятка по код-ревью' });
    await expect(guide).toBeVisible();
    await guide.getByRole('button', { name: 'Отметить как прочитанную' }).click();
    await expect(guide.getByText('Памятка прочитана')).toBeVisible();
    await guide.getByRole('button', { name: 'Закрыть' }).click();
    const introduction = other.getByRole('button', { name: /Знакомство с командой/ });
    await introduction.click();
    await expect(introduction).toHaveAttribute('aria-pressed', 'true');
    await expect(introduction).toHaveCSS('background-color', 'rgb(255, 255, 255)');
    await other.getByRole('button', { name: 'Тёмная тема' }).click();
    await expect(introduction).toHaveCSS('background-color', 'rgb(36, 36, 36)');
    await expect(introduction.getByText('Знакомство с командой')).toHaveCSS('color', 'rgb(255, 255, 255)');
    const processes = other.getByRole('button', { name: /Изучение процессов/ });
    await processes.click();
    await expect(introduction).toHaveAttribute('aria-pressed', 'false');
    await expect(processes).toHaveCSS('background-color', 'rgb(36, 36, 36)');
  } finally {
    await otherSession.close();
  }
  await holdForRecording();
});
