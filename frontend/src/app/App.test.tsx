// @vitest-environment jsdom

import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import App from './App';

let container: HTMLDivElement;
let root: ReturnType<typeof createRoot>;

beforeEach(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);
  vi.stubGlobal('AbortSignal', { timeout: () => new AbortController().signal });
  Element.prototype.scrollIntoView = vi.fn();
  localStorage.clear();
  sessionStorage.clear();
  container = document.createElement('div');
  document.body.append(container);
  root = createRoot(container);
});

afterEach(async () => {
  await act(async () => root.unmount());
  container.remove();
  vi.unstubAllGlobals();
});

function button(label: string): HTMLButtonElement {
  const target = [...document.querySelectorAll('button')].find(element =>
    element.getAttribute('aria-label') === label || element.textContent?.trim() === label);
  if (!target) throw new Error(`Button missing: ${label}`);
  return target;
}

async function click(label: string) {
  await act(async () => button(label).click());
}

async function enter(selector: string, value: string) {
  const input = document.querySelector<HTMLInputElement>(selector);
  if (!input) throw new Error(`Input missing: ${selector}`);
  const setValue = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set;
  await act(async () => {
    setValue?.call(input, value);
    input.dispatchEvent(new Event('input', { bubbles: true }));
  });
}

it('restores an unanswered question and retries it with the original client message ID', async () => {
  const question = 'Как подключить учебный VPN?';
  const pending = { client_message_id: 'saved-message', text: question };
  sessionStorage.setItem('buddy-conversation:atlas-intern:atlas:intern', 'saved-chat');
  const fetch = vi.fn(async (url: string, request: RequestInit) => {
    switch (url) {
      case '/api/session':
        return Response.json({ id: 'atlas-intern', team_id: 'atlas', role: 'intern', name: 'Стажёр Atlas' });
      case '/api/conversations/saved-chat':
        return Response.json({
          id: 'saved-chat', messages: [{ id: 1, role: 'user', text: question }],
          draft: null, operation: null, pending_message: pending,
        });
      case '/api/documents':
        return Response.json({ documents: [] });
      case '/api/operations':
        return Response.json({ operations: [] });
      case '/api/conversations/saved-chat/messages':
        expect(request.method).toBe('POST');
        expect(JSON.parse(request.body as string)).toEqual(pending);
        return Response.json({ answer: 'Открой инструкцию команды.', sources: [], draft: null });
      default:
        throw new Error(`Unexpected request: ${url}`);
    }
  });
  vi.stubGlobal('fetch', fetch);

  await act(async () => root.render(<App />));
  expect(button('Повторить').disabled).toBe(false);
  expect(fetch.mock.calls.filter(([url]) => url.endsWith('/messages'))).toHaveLength(0);
  await click('Повторить');
  expect(container.textContent).toContain('Открой инструкцию команды.');
  expect(container.textContent?.split(question)).toHaveLength(2);
  expect(container.textContent).not.toContain('Повторить');
});

it('keeps access and review progress across conversations and shows the approved next step', async () => {
  let approved = false;
  let team = 'atlas';
  const mockFetch = vi.fn(async (url: string) => {
    switch (url) {
      case '/api/session':
        return Response.json({ id: `${team}-intern`, team_id: team, role: 'intern', name: 'Стажёр' });
      case '/api/documents':
        return Response.json({ documents: [] });
      case '/api/operations':
        return Response.json({ operations: team === 'orbit' ? [] : [{
          id: 'operation-1', tool_id: 'gitlab', resource_id: 'atlas-training-repo', external_id: 'REQ-1001',
          service_status: approved ? 'approved' : 'pending_approval', delivery_state: 'delivered',
          next_step: approved ? 'Откройте репозиторий команды.' : 'Дождитесь решения.',
        }] });
      case '/api/documents/atlas-first-task':
        return Response.json({ document_id: 'atlas-first-task', chunks: [{
          chunk_id: 'atlas-first-task-r1', title: 'Первое учебное задание Atlas', source_url: null,
          text: 'Прочитайте README каталога.',
        }] });
      default:
        throw new Error(`Unexpected request: ${url}`);
    }
  });
  vi.stubGlobal('fetch', mockFetch);
  await act(async () => root.render(<App />));
  await click('Доступы');
  expect(container.textContent).toContain('REQ-1001');
  expect(container.textContent).toContain('На согласовании');
  expect(container.textContent).not.toContain('Открыть инструкцию первой задачи');

  await click('Открыть памятку по код-ревью');
  await click('Отметить как прочитанную');
  expect(container.textContent).toContain('Памятка прочитана');

  await click('AI-buddy');
  await click('Новый диалог');
  await click('Доступы');
  expect(container.textContent).toContain('REQ-1001');
  expect(container.textContent).toContain('Памятка прочитана');

  approved = true;
  await act(async () => root.render(<App key="atlas-reload" />));
  await click('Доступы');
  expect(container.textContent).toContain('Памятка прочитана');
  expect(container.textContent).toContain('Доступ предоставлен');
  await click('Открыть инструкцию первой задачи');
  expect(document.body.textContent).toContain('Прочитайте README каталога.');
  await click('Закрыть');

  team = 'orbit';
  await act(async () => root.render(<App key="orbit-session" />));
  await click('Доступы');
  expect(container.textContent).toContain('Подтверждённых заявок пока нет.');
  expect(container.textContent).not.toContain('REQ-1001');
  expect(container.textContent).not.toContain('Памятка прочитана');
});

it('opens a cited source and submits a reviewed draft with the saved version', async () => {
  const draft = { id: 'draft-1', tool_id: 'gitlab', resource_id: 'atlas-training-repo', justification: '', version: 1 };
  const citation = {
    chunk_id: 'atlas-repo-r1', document_id: 'atlas-repo', title: 'GitLab Atlas', source_url: 'https://example.org/instructions#gitlab',
    source_kind: 'demo_policy', version: 'v1',
  };
  const operation = {
    id: 'operation-1', external_id: null, delivery_state: 'queued',
    service_status: null, next_step: 'Заявка в очереди на отправку.',
  };
  let submitted = false;
  const mockFetch = vi.fn(async (url: string, request: RequestInit) => {
    switch (url) {
      case '/api/session':
        return Response.json({ id: 'atlas-intern', team_id: 'atlas', role: 'intern', name: 'Стажёр Atlas' });
      case '/api/documents':
        return Response.json({ documents: [] });
      case '/api/operations':
        return Response.json({ operations: submitted ? [{
          ...operation, tool_id: draft.tool_id, resource_id: draft.resource_id,
        }] : [] });
      case '/api/conversations':
        return Response.json({ id: 'conversation-1' }, { status: 201 });
      case '/api/conversations/conversation-1/messages':
        expect(JSON.parse(request.body as string)).toMatchObject({ text: 'Нужен доступ к учебному GitLab' });
        return Response.json({ answer: 'Проверь ресурс и укажи обоснование.', sources: [citation], draft });
      case '/api/documents/atlas-repo':
        return Response.json({ document_id: 'atlas-repo', chunks: [{
          ...citation, text: 'Для учебного репозитория укажите роль Reporter.',
        }, {
          ...citation, chunk_id: 'atlas-repo-r2', text: 'Другой фрагмент.', source_url: 'javascript:void(0)',
        }] });
      case '/api/drafts/draft-1':
        expect(request.method).toBe('PATCH');
        expect(JSON.parse(request.body as string)).toEqual({ expected_version: 1, justification: 'Прочитать учебный README' });
        return Response.json({ ...draft, justification: 'Прочитать учебный README', version: 2 });
      case '/api/drafts/draft-1/confirm':
        expect(request.method).toBe('POST');
        expect(JSON.parse(request.body as string)).toEqual({ version: 2 });
        submitted = true;
        return Response.json(operation);
      default:
        throw new Error(`Unexpected request: ${url}`);
    }
  });
  vi.stubGlobal('fetch', mockFetch);
  await act(async () => root.render(<App />));

  await click('Нужен доступ к учебному GitLab');
  await click('GitLab Atlas');
  expect(document.body.textContent).toContain('Для учебного репозитория укажите роль Reporter.');
  const sourceSections = [...document.querySelectorAll('[role="dialog"] section')];
  expect(sourceSections[0]?.textContent).toContain('Фрагмент из ответа');
  expect(sourceSections[0]?.querySelector('a')?.getAttribute('href')).toBe(citation.source_url);
  expect(sourceSections[1]?.querySelector('a')).toBeNull();
  await click('Закрыть');
  await click('Проверить черновик');
  expect(submitted).toBe(false);
  await enter('#reason', 'Прочитать учебный README');
  await click('Подтвердить заявку');
  expect(submitted).toBe(true);
  expect(container.textContent).toContain('В очереди');
  expect(container.textContent).not.toContain(operation.id);
  expect(mockFetch.mock.calls.filter(([url]) => url.endsWith('/confirm'))).toHaveLength(1);
});

it('recovers the confirmed request when the confirmation response is lost', async () => {
  const draft = {
    id: 'draft-1', tool_id: 'gitlab', resource_id: 'atlas-training-repo',
    justification: 'Прочитать учебный README', version: 2,
  };
  const operation = {
    id: 'operation-1', tool_id: draft.tool_id, resource_id: draft.resource_id,
    external_id: 'REQ-1001', delivery_state: 'delivered',
    service_status: 'pending_approval', next_step: 'Дождитесь решения.',
  };
  sessionStorage.setItem('buddy-conversation:atlas-intern:atlas:intern', 'saved-chat');
  let confirmed = false;
  const fetch = vi.fn(async (url: string, request: RequestInit) => {
    switch (url) {
      case '/api/session':
        return Response.json({ id: 'atlas-intern', team_id: 'atlas', role: 'intern', name: 'Стажёр Atlas' });
      case '/api/conversations/saved-chat':
        return Response.json({ id: 'saved-chat', messages: [], draft, operation: confirmed ? operation : null });
      case '/api/documents':
        return Response.json({ documents: [] });
      case '/api/operations':
        return Response.json({ operations: confirmed ? [operation] : [] });
      case '/api/drafts/draft-1/confirm':
        expect(JSON.parse(request.body as string)).toEqual({ version: 2 });
        confirmed = true;
        throw new TypeError('Connection lost after confirmation');
      default:
        throw new Error(`Unexpected request: ${url}`);
    }
  });
  vi.stubGlobal('fetch', fetch);

  await act(async () => root.render(<App />));
  await click('Доступы');
  await click('Проверить черновик');
  await click('Подтвердить заявку');
  expect(document.querySelector('[role="dialog"]')).toBeNull();
  expect(container.textContent).toContain('REQ-1001');
  expect(container.textContent).toContain('На согласовании');
  expect(fetch.mock.calls.filter(([url]) => url.endsWith('/confirm'))).toHaveLength(1);
});

it('recovers the knowledge catalog after a failed load and filters by title and source type', async () => {
  let catalogAttempts = 0;
  const fetch = vi.fn(async (url: string) => {
    switch (url) {
      case '/api/session':
        return Response.json({ id: 'atlas-intern', team_id: 'atlas', role: 'intern', name: 'Стажёр Atlas' });
      case '/api/operations':
        return Response.json({ operations: [] });
      case '/api/documents':
        if (++catalogAttempts === 1) return Response.json({ error: { message: 'Каталог временно недоступен' } }, { status: 503 });
        return Response.json({ documents: [
          { id: 'atlas-repo', title: 'Доступ к GitLab Atlas', kind: 'demo_policy' },
          { id: 'gitlab-docs', title: 'Справка GitLab', kind: 'vendor_docs' },
          { id: 'vpn-docs', title: 'Подключение VPN', kind: 'vendor_docs' },
        ] });
      default:
        throw new Error(`Unexpected request: ${url}`);
    }
  });
  vi.stubGlobal('fetch', fetch);

  await act(async () => root.render(<App />));
  await click('База знаний');
  expect(container.textContent).toContain('Каталог временно недоступен');
  await click('Повторить загрузку');
  expect(container.textContent).toContain('Доступ к GitLab Atlas');
  expect(container.textContent).toContain('Подключение VPN');
  await enter('input[aria-label="Найти документ по названию"]', ' gitlab ');
  expect(container.textContent).not.toContain('Подключение VPN');
  const type = document.querySelector<HTMLSelectElement>('select[aria-label="Тип источника"]');
  if (!type) throw new Error('Source type filter is missing');
  await act(async () => {
    type.value = 'vendor_docs';
    type.dispatchEvent(new Event('change', { bubbles: true }));
  });
  expect(container.textContent).toContain('Справка GitLab');
  expect(container.textContent).not.toContain('Доступ к GitLab Atlas');
});

it('copies a request summary for the mentor and leaves selectable text if clipboard access fails', async () => {
  const writeText = vi.fn()
    .mockResolvedValueOnce(undefined)
    .mockRejectedValueOnce(new Error('Clipboard denied'));
  const previousClipboard = Object.getOwnPropertyDescriptor(navigator, 'clipboard');
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } });
  try {
    const fetch = vi.fn(async (url: string) => {
      switch (url) {
        case '/api/session':
          return Response.json({ id: 'atlas-intern', team_id: 'atlas', role: 'intern', name: 'Стажёр Atlas' });
        case '/api/documents':
          return Response.json({ documents: [] });
        case '/api/operations':
          return Response.json({ operations: [{
            id: 'operation-1', tool_id: 'gitlab', resource_id: 'atlas-training-repo',
            external_id: 'REQ-1001', service_status: 'rejected', delivery_state: 'delivered',
            next_step: 'Уточните причину у наставника.',
          }] });
        default:
          throw new Error(`Unexpected request: ${url}`);
      }
    });
    vi.stubGlobal('fetch', fetch);
    await act(async () => root.render(<App />));
    await click('Доступы');
    await click('Сводка для наставника');

    const text = document.querySelector<HTMLTextAreaElement>('textarea[aria-label="Текст сводки"]');
    expect(text?.readOnly).toBe(true);
    expect(text?.value).toContain('atlas-training-repo');
    expect(text?.value).toContain('Номер заявки: REQ-1001');
    expect(text?.value).toContain('Статус на момент проверки: Отклонена');
    expect(text?.value).not.toContain('operation-1');
    await click('Скопировать сводку');
    expect(writeText).toHaveBeenCalledWith(text?.value);
    expect(document.body.textContent).toContain('Сводка скопирована.');
    await click('Скопировать сводку');
    expect(document.body.textContent).toContain('скопируй его вручную');
  } finally {
    if (previousClipboard) Object.defineProperty(navigator, 'clipboard', previousClipboard);
    else Reflect.deleteProperty(navigator, 'clipboard');
  }
});

it('stops polling exhausted reconciliation and allows a manual refresh', async () => {
  const fetch = vi.fn(async (url: string) => {
    switch (url) {
      case '/api/session':
        return Response.json({ id: 'atlas-intern', team_id: 'atlas', role: 'intern', name: 'Стажёр Atlas' });
      case '/api/documents':
        return Response.json({ documents: [] });
      case '/api/operations':
        return Response.json({ operations: [{
          id: 'operation-1', tool_id: 'gitlab', resource_id: 'atlas-training-repo',
          external_id: null, service_status: null, delivery_state: 'reconciling',
          needs_manual_check: true, next_step: 'Результат пока неизвестен.',
        }] });
      default:
        throw new Error(`Unexpected request: ${url}`);
    }
  });
  vi.stubGlobal('fetch', fetch);
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
  try {
    await act(async () => root.render(<App />));
    await click('Доступы');
    expect(container.textContent).toContain('Требуется проверка');
    expect(container.textContent).toContain('Результат пока неизвестен.');
    const operationCalls = () => fetch.mock.calls.filter(([url]) => url === '/api/operations').length;
    const before = operationCalls();
    await act(async () => vi.advanceTimersByTimeAsync(6000));
    expect(operationCalls()).toBe(before);
    await click('Обновить заявки');
    expect(operationCalls()).toBe(before + 1);
  } finally {
    vi.useRealTimers();
  }
});
