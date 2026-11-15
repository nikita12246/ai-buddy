// @vitest-environment jsdom

import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { DashboardLayout } from './DashboardLayout';
import { Internship } from './Internship';
import { ProjectLinks } from './ProjectLinks';

let container: HTMLDivElement;
let root: Root;

beforeEach(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);
  container = document.createElement('div');
  document.body.append(container);
  root = createRoot(container);
});

afterEach(async () => {
  await act(async () => root.unmount());
  container.remove();
  vi.unstubAllGlobals();
});

async function click(label: string, scope: ParentNode = document) {
  const button = [...scope.querySelectorAll('button')].find(element =>
    element.getAttribute('aria-label') === label || element.textContent?.trim() === label);
  if (!button) throw new Error(`Button missing: ${label}`);
  await act(async () => {
    if (button.getAttribute('role') === 'tab') {
      button.dispatchEvent(new MouseEvent('mousedown', { bubbles: true, button: 0 }));
    }
    button.click();
  });
}

it('uses the approved request in the internship checklist and opens the team guide', async () => {
  const openDocument = vi.fn();
  const markReviewRead = vi.fn();
  await act(async () => root.render(<Internship teamId="orbit" ask={vi.fn()} openDocument={openDocument}
    reviewRead={false} markReviewRead={markReviewRead}
    access={{ externalId: 'REQ-1', status: 'Доступ предоставлен' }} />));
  expect(container.textContent).toContain('Доступ к GitLab предоставлен.');
  await click('Открыть инструкцию');
  expect(openDocument).toHaveBeenCalledWith('orbit-repo');
  const reviewCard = container.querySelector<HTMLButtonElement>('.internship-review-card');
  await act(async () => reviewCard?.click());
  expect(document.querySelector('[role="dialog"]')?.textContent).toContain('Памятка по код-ревью');
  await click('Отметить как прочитанную');
  expect(markReviewRead).toHaveBeenCalledOnce();
  await click('Закрыть');
  await click('Мои задачи');
  await click('Готово');
  const row = container.querySelector('[aria-label="Открыть задачу Получить доступ к GitLab"]');
  expect(row?.textContent).toContain('Готово');
  expect(container.textContent).not.toContain('Познакомиться со структурой сервиса');
  await act(async () => row?.dispatchEvent(new KeyboardEvent('keydown', { bubbles: true, key: 'Enter' })));
  expect(document.querySelector('[role="dialog"]')?.textContent).toContain('Критерии готовности');
  expect(document.querySelector('[role="dialog"]')?.textContent).not.toContain('Блокер');
  await click('Закрыть');
  await click('Мой рост');
  expect(container.textContent).toContain('Контрольные точки учебные.');
});

it('shows team policies and forwards document and assistant actions', async () => {
  const ask = vi.fn();
  const openDocument = vi.fn();
  await act(async () => root.render(<ProjectLinks ask={ask} openDocument={openDocument} documents={[
    { id: 'atlas-repo', title: 'Доступ к репозиторию Atlas', kind: 'demo_policy' },
    { id: 'public-guide', title: 'Публичный справочник', kind: 'vendor_docs' },
  ]} />));
  expect(container.textContent).toContain('Доступ к репозиторию Atlas');
  expect(container.querySelector('h2')?.textContent).toBe('GitLab');
  expect(container.textContent).not.toContain('Публичный справочник');
  await click('Открыть инструкцию');
  await click('Спросить AI-buddy');
  expect(openDocument).toHaveBeenCalledWith('atlas-repo');
  expect(ask).toHaveBeenCalledWith('Объясни, как применить инструкцию «Доступ к репозиторию Atlas».');
});

it('supports navigation from the labelled mobile menu and shows the pending notification', async () => {
  const navigate = vi.fn();
  const theme = vi.fn();
  await act(async () => root.render(<DashboardLayout activeTab="dashboard" setActiveTab={navigate}
    isDarkMode={false} toggleDarkMode={theme} onSearch={vi.fn()}
    access={{ externalId: 'REQ-2', status: 'На согласовании' }}>Рабочий экран</DashboardLayout>));
  await click('Открыть меню');
  const menu = document.querySelector('[role="dialog"][aria-labelledby]');
  expect(menu?.textContent).toContain('AI-buddy Workspace');
  if (!menu) throw new Error('Mobile menu did not open');
  await click('База знаний', menu);
  expect(navigate).toHaveBeenCalledWith('knowledge');
  await click('Тёмная тема');
  expect(theme).toHaveBeenCalledOnce();
  await click('Уведомления');
  expect(container.textContent).toContain('Заявка REQ-2: На согласовании');
});
