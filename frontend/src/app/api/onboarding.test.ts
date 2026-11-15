import { afterEach, describe, expect, it, vi } from 'vitest';
import { onboardingApi } from './onboarding';

afterEach(() => vi.unstubAllGlobals());

describe('onboarding API', () => {
  it('sends the session token and keeps the message ID on retry', async () => {
    const fetch = vi.fn().mockImplementation(() => Promise.resolve(Response.json({ answer: 'Ответ' })));
    vi.stubGlobal('fetch', fetch);
    const api = onboardingApi('demo-session');

    await api.send('conversation/id', 'message-id', 'Вопрос');
    await api.send('conversation/id', 'message-id', 'Вопрос');

    expect(fetch.mock.calls).toHaveLength(2);
    for (const [path, request] of fetch.mock.calls) {
      expect(path).toBe('/api/conversations/conversation%2Fid/messages');
      expect(request.method).toBe('POST');
      expect(request.headers.Authorization).toBe('Bearer demo-session');
      expect(JSON.parse(request.body)).toEqual({ client_message_id: 'message-id', text: 'Вопрос' });
    }
  });

  it('preserves a server denial when opening a source', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(Response.json({
      error: { code: 'not_found', message: 'Объект не найден.' },
    }, { status: 404 })));

    await expect(onboardingApi('demo-session').document('atlas-repo')).rejects.toMatchObject({
      status: 404, code: 'not_found', message: 'Объект не найден.',
    });
  });

  it('loads confirmed requests for the session without a conversation ID', async () => {
    const request = {
      id: 'operation-1', tool_id: 'gitlab', resource_id: 'atlas-training-repo',
      external_id: 'REQ-1001', service_status: 'approved', delivery_state: 'delivered',
      next_step: 'Откройте репозиторий команды.',
    };
    const fetch = vi.fn().mockResolvedValue(Response.json({ operations: [request] }));
    vi.stubGlobal('fetch', fetch);

    await expect(onboardingApi('demo-session').operations()).resolves.toEqual([request]);
    expect(fetch).toHaveBeenCalledWith('/api/operations', expect.objectContaining({
      method: 'GET', headers: expect.objectContaining({ Authorization: 'Bearer demo-session' }),
    }));
  });
});
