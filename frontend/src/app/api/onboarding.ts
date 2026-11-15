export type Profile = { id: string; team_id: string; role: string; name: string };
export type DocumentSummary = { id: string; title: string; kind: string };
export const documentKinds: Record<string, string> = {
  demo_policy: 'Учебная политика', avito_public: 'Публичные материалы Авито', vendor_docs: 'Документация инструментов',
};
export type Citation = {
  chunk_id: string;
  document_id: string;
  title: string;
  source_url: string | null;
  source_kind: string;
  version: string;
  excerpt?: string;
};
export type DocumentResult = {
  document_id: string;
  chunks: (Citation & { text: string })[];
};
export type Draft = { id: string; tool_id: string; resource_id: string; justification: string; version: number };
export type Operation = {
  id: string;
  external_id: string | null;
  delivery_state: string;
  service_status: string | null;
  next_step: string;
  needs_manual_check?: boolean;
};
export type ConfirmedRequest = Operation & { tool_id: string; resource_id: string };
export type ChatMessage = {
  id: number;
  role: 'user' | 'assistant';
  text: string;
  sources?: Citation[];
  clarifying_question?: string | null;
};
export type Conversation = {
  id: string;
  messages: ChatMessage[] | null;
  draft: Draft | null;
  operation: Operation | null;
  pending_message?: { client_message_id: string; text: string };
};
export type Answer = { answer: string; sources: Citation[]; clarifying_question: string | null; draft: Draft | null };
export type AccessRequest = { externalId: string | null; status: string };

export function accessSummary(operation: Operation | null): AccessRequest | null {
  if (!operation) return null;
  const statuses: Record<string, string> = {
    approved: 'Доступ предоставлен',
    rejected: 'Отклонена',
    pending_approval: 'На согласовании',
    queued: 'В очереди',
    sending: 'Отправляется',
    reconciling: 'Уточняется статус',
  };
  return {
    externalId: operation.external_id,
    status: operation.needs_manual_check
      ? 'Требуется проверка'
      : statuses[operation.service_status ?? operation.delivery_state] ?? 'Статус уточняется',
  };
}

export class ApiError extends Error {
  constructor(message: string, readonly status: number, readonly code?: string) {
    super(message);
  }
}

export function onboardingApi(token?: string) {
  async function request<T>(path: string, method = 'GET', body?: object): Promise<T> {
    const headers: Record<string, string> = { Accept: 'application/json' };
    if (token) headers.Authorization = `Bearer ${token}`;
    if (body) headers['Content-Type'] = 'application/json';
    const response = await fetch(`/api${path}`, {
      method, headers, body: body ? JSON.stringify(body) : undefined,
      signal: AbortSignal.timeout(method === 'GET' ? 10000 : 65000),
    });
    const result = await response.json().catch(() => null);
    if (!response.ok || result === null) {
      throw new ApiError(result?.error?.message ?? 'API AI-buddy недоступен.', response.status, result?.error?.code);
    }
    return result as T;
  }

  const conversation = (id: string) => `/conversations/${encodeURIComponent(id)}`;
  const draft = (id: string) => `/drafts/${encodeURIComponent(id)}`;
  return {
    profile: () => request<Profile>('/me'),
    startSession: () => request<Profile>('/session', 'POST'),
    createConversation: () => request<{ id: string }>('/conversations', 'POST'),
    conversation: (id: string) => request<Conversation>(conversation(id)),
    send: (id: string, clientId: string, text: string) =>
      request<Answer>(`${conversation(id)}/messages`, 'POST', { client_message_id: clientId, text }),
    document: (id: string) => request<DocumentResult>(`/documents/${encodeURIComponent(id)}`),
    documents: () => request<{ documents: DocumentSummary[] }>('/documents').then(result => result.documents),
    updateDraft: (current: Draft, justification: string) =>
      request<Draft>(draft(current.id), 'PATCH', { expected_version: current.version, justification }),
    confirmDraft: (current: Draft) =>
      request<Operation>(`${draft(current.id)}/confirm`, 'POST', { version: current.version }),
    operations: () => request<{ operations: ConfirmedRequest[] }>('/operations').then(result => result.operations),
    operation: (id: string) => request<Operation>(`/operations/${encodeURIComponent(id)}`),
  };
}
