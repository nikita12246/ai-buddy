import { useEffect, useMemo, useRef, useState, type FormEvent, type ReactNode } from 'react';
import { DashboardLayout } from './components/DashboardLayout';
import { DashboardOverview } from './components/DashboardOverview';
import { KnowledgeBase } from './components/KnowledgeBase';
import { AccessRequests } from './components/AccessRequests';
import { AIAssistant } from './components/AIAssistant';
import { ProjectLinks } from './components/ProjectLinks';
import { Internship } from './components/Internship';
import { OnboardingNextStep } from './components/OnboardingNextStep';
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from './components/ui/dialog';
import { Button } from './components/ui/button';
import { Input } from './components/ui/input';
import { Label } from './components/ui/label';
import { accessSummary, ApiError, onboardingApi, type Citation, type ConfirmedRequest, type Conversation, type DocumentResult, type DocumentSummary, type Draft, type Operation, type Profile } from './api/onboarding';

export type Message = { id: string; who: 'user' | 'buddy'; text: string; sources?: Citation[]; clarification?: string | null };
type Pending = { id: string; text: string; conversationId?: string };
const greeting: Message = { id: 'greeting', who: 'buddy', text: 'Привет! Я помогу найти задачи в Jira, разобраться с учебным VPN или подготовить заявку на доступ к GitLab.' };

const conversationKey = (profile: Profile) => `buddy-conversation:${profile.id}:${profile.team_id}:${profile.role}`;
const reviewKey = (profile: Profile) => `buddy-review-read:${profile.id}:${profile.team_id}:${profile.role}`;

function restorePending(conversation?: Conversation): Pending | null {
  const message = conversation?.pending_message;
  return message && conversation
    ? { id: message.client_message_id, text: message.text, conversationId: conversation.id }
    : null;
}

function history(result?: Conversation): Message[] {
  return [greeting, ...(result?.messages ?? []).map((entry): Message => ({
    id: String(entry.id), who: entry.role === 'user' ? 'user' : 'buddy',
    text: entry.text, sources: entry.sources, clarification: entry.clarifying_question,
  }))];
}

function errorMessage(error: unknown) {
  return error instanceof ApiError ? error.message : 'Связь с AI-buddy прервалась. Попробуй ещё раз.';
}

function originalURL(value: string | null): string | undefined {
  if (!value) return undefined;
  try {
    const url = new URL(value);
    if (url.protocol === 'https:') return url.href;
  } catch {
    return undefined;
  }
}

export default function App() {
  const [session, setSession] = useState<{ profile: Profile; previous?: Conversation } | null>(null);
  const [attempt, setAttempt] = useState(0);
  const [error, setError] = useState('');
  const connection = useRef<{ attempt: number; result: Promise<Profile> } | null>(null);

  useEffect(() => {
    let active = true;
    async function connect() {
      setError('');
      try {
        const api = onboardingApi();
        if (connection.current?.attempt !== attempt) {
          connection.current = { attempt, result: api.startSession() };
        }
        const profile = await connection.current.result;
        const id = sessionStorage.getItem(conversationKey(profile));
        let previous: Conversation | undefined;
        if (id) {
          try {
            previous = await api.conversation(id);
          } catch (error) {
            if (!(error instanceof ApiError) || error.status !== 404) throw error;
            sessionStorage.removeItem(conversationKey(profile));
          }
        }
        if (active) setSession({ profile, previous });
      } catch (error) {
        if (active) setError(errorMessage(error));
      }
    }
    void connect();
    return () => { active = false; };
  }, [attempt]);

  if (session) return <Workspace {...session} />;
  return <main className="min-h-screen grid place-items-center bg-background p-6">
    <div className="w-full max-w-md space-y-4 rounded-2xl border bg-card p-6 shadow-lg">
      <h1 className="text-2xl font-medium">AI-buddy</h1>
      {error ? <>
        <p role="alert" className="text-sm text-destructive">{error}</p>
        <Button onClick={() => setAttempt(value => value + 1)}>Повторить подключение</Button>
      </> : <p role="status" className="text-sm text-muted-foreground">Открываем рабочее пространство…</p>}
    </div>
  </main>;
}

function Workspace({ profile, previous }: { profile: Profile; previous?: Conversation }) {
  const api = useMemo(() => onboardingApi(), []);
  const [activeTab, setActiveTab] = useState(previous?.pending_message ? 'ai-assistant' : 'dashboard');
  const [dark, setDark] = useState(false);
  const [conversationId, setConversationId] = useState(previous?.id);
  const [messages, setMessages] = useState(() => history(previous));
  const [pending, setPending] = useState<Pending | null>(() => restorePending(previous));
  const [sending, setSending] = useState(false);
  const inFlight = useRef(false);
  const [chatError, setChatError] = useState(previous?.pending_message ? 'Вопрос сохранён. Если ответ не появился, попробуй повторить запрос.' : '');
  const [draft, setDraft] = useState(previous?.draft ?? null);
  const [operation, setOperation] = useState(previous?.operation ?? null);
  const [requests, setRequests] = useState<ConfirmedRequest[]>([]);
  const [requestsLoading, setRequestsLoading] = useState(true);
  const [requestsError, setRequestsError] = useState('');
  const [requestsAttempt, setRequestsAttempt] = useState(0);
  const [reviewRead, setReviewRead] = useState(() => localStorage.getItem(reviewKey(profile)) === 'true');
  const [requestOpen, setRequestOpen] = useState(false);
  const [requestBusy, setRequestBusy] = useState(false);
  const [requestError, setRequestError] = useState('');
  const [reason, setReason] = useState('');
  const [documents, setDocuments] = useState<DocumentSummary[]>([]);
  const [documentsLoading, setDocumentsLoading] = useState(true);
  const [documentsError, setDocumentsError] = useState('');
  const [documentsAttempt, setDocumentsAttempt] = useState(0);
  const [recentIds, setRecentIds] = useState<string[]>([]);
  const [knowledgeQuery, setKnowledgeQuery] = useState('');
  const [chatInput, setChatInput] = useState('');
  const [source, setSource] = useState<{ title: string; citedChunk?: string; data?: DocumentResult; error?: string } | null>(null);
  const sourceRequest = useRef(0);
  const operationId = operation?.id;
  const gitlabRequests = requests.filter(request => request.tool_id === 'gitlab');
  const approvedAccess = gitlabRequests.find(request =>
    request.resource_id === `${profile.team_id}-training-repo` && request.service_status === 'approved');
  const currentAccess = approvedAccess ?? gitlabRequests[0] ?? (draft?.tool_id === 'gitlab' ? operation : null);
  const access = accessSummary(currentAccess);
  const chatRequest = requests.find(request => request.id === operationId) ?? operation ?? currentAccess;
  const viewingRequests = activeTab === 'access';

  useEffect(() => {
    let active = true;
    setDocumentsLoading(true);
    setDocumentsError('');
    api.documents().then(result => { if (active) setDocuments(result); })
      .catch(error => { if (active) setDocumentsError(errorMessage(error)); })
      .finally(() => { if (active) setDocumentsLoading(false); });
    return () => { active = false; };
  }, [api, documentsAttempt]);

  useEffect(() => {
    document.documentElement.classList.toggle('dark', dark);
    return () => document.documentElement.classList.remove('dark');
  }, [dark]);

  useEffect(() => {
    let active = true;
    let timer: ReturnType<typeof setTimeout>;
    async function poll() {
      try {
        const current = await api.operations();
        if (!active) return;
        setRequests(current);
        setRequestsError('');
        if (!current.some(request => !request.needs_manual_check &&
          request.service_status !== 'approved' && request.service_status !== 'rejected')) return;
      } catch (error) {
        if (!active) return;
        setRequestsError(errorMessage(error));
        if (error instanceof ApiError && error.status === 401) {
          setRequests([]);
          return;
        }
      } finally {
        if (active) setRequestsLoading(false);
      }
      timer = setTimeout(() => void poll(), 2000);
    }
    void poll();
    return () => { active = false; clearTimeout(timer); };
  }, [api, operationId, requestsAttempt, viewingRequests]);

  function applyConversation(result: Conversation) {
    setMessages(history(result));
    setDraft(result.draft);
    setOperation(result.operation);
  }

  async function send(message: Pending) {
    if (inFlight.current) return;
    inFlight.current = true;
    setSending(true);
    setChatError('');
    let current = message;
    try {
      const id = current.conversationId ?? (await api.createConversation()).id;
      setConversationId(id);
      current = { ...current, conversationId: id };
      sessionStorage.setItem(conversationKey(profile), id);
      const result = await api.send(id, current.id, current.text);
      setMessages(previous => [...previous, {
        id: `reply-${current.id}`, who: 'buddy', text: result.answer,
        sources: result.sources, clarification: result.clarifying_question,
      }]);
      if (result.draft) setDraft(result.draft);
      setPending(null);
    } catch (error) {
      setPending(current);
      setChatError(errorMessage(error));
    } finally {
      inFlight.current = false;
      setSending(false);
    }
  }

  function ask(question: string) {
    const text = question.trim();
    if (!text || pending || inFlight.current) return;
    setActiveTab('ai-assistant');
    const message = { id: crypto.randomUUID(), text, conversationId };
    setMessages(previous => [...previous, { id: message.id, who: 'user', text }]);
    setPending(message);
    void send(message);
  }

  function newConversation() {
    if (inFlight.current || requestBusy) return;
    setConversationId(undefined);
    sessionStorage.removeItem(conversationKey(profile));
    setMessages([greeting]);
    setChatInput('');
    setPending(null);
    setChatError('');
    setDraft(null);
    setOperation(null);
    setRequestError('');
    setActiveTab('ai-assistant');
  }

  async function loadDocument(id: string, title: string, citedChunk?: string) {
    const request = ++sourceRequest.current;
    setSource({ title });
    try {
      const data = await api.document(id);
      if (request === sourceRequest.current) {
        setSource({ title, citedChunk, data });
        setRecentIds(previous => [id, ...previous.filter(item => item !== id)].slice(0, 5));
      }
    } catch (error) {
      if (request === sourceRequest.current) setSource({ title, error: errorMessage(error) });
    }
  }

  function openSource(citation: Citation) {
    void loadDocument(citation.document_id, citation.title, citation.chunk_id);
  }

  function openDocument(id: string) {
    const document = documents.find(item => item.id === id);
    void loadDocument(id, document?.title ?? 'Документ');
  }

  function markReviewRead() {
    localStorage.setItem(reviewKey(profile), 'true');
    setReviewRead(true);
  }

  function openRequest() {
    if (!draft || operation) return;
    setRequestError('');
    setReason(draft.justification);
    setRequestOpen(true);
  }

  function prepareGitLabRequest() {
    setChatInput('Нужен доступ к учебному GitLab. Нужно прочитать README для первого задания.');
    setActiveTab('ai-assistant');
  }

  function searchDocuments(query: string) {
    if (!query.trim()) return;
    setKnowledgeQuery(query);
    setActiveTab('knowledge');
  }

  async function confirmRequest(event: FormEvent) {
    event.preventDefault();
    if (!draft || !reason.trim() || requestBusy) return;
    setRequestBusy(true);
    setRequestError('');
    try {
      const saved = reason.trim() === draft.justification ? draft : await api.updateDraft(draft, reason.trim());
      setDraft(saved);
      const result = await api.confirmDraft(saved);
      setOperation(result);
      setRequestOpen(false);
      setActiveTab('access');
    } catch (error) {
      setRequestError(errorMessage(error));
      if (conversationId && (!(error instanceof ApiError) || error.status >= 500 || error.status === 409)) {
        try {
          const current = await api.conversation(conversationId);
          applyConversation(current);
          if (current.operation) {
            setRequestOpen(false);
            setActiveTab('access');
          }
        } catch {
          // Keep the original error and the user's justification for retry.
        }
      }
    } finally {
      setRequestBusy(false);
    }
  }

  const pages: Record<string, ReactNode> = {
    dashboard: <DashboardOverview ask={ask} navigate={setActiveTab} openDocument={openDocument} access={access} recentIds={recentIds} documents={documents} />,
    knowledge: <KnowledgeBase documents={documents} loading={documentsLoading} error={documentsError}
      query={knowledgeQuery} setQuery={setKnowledgeQuery}
      reload={() => setDocumentsAttempt(value => value + 1)} openDocument={openDocument} ask={ask} />,
    access: <div className="space-y-6">
      <AccessRequests requests={requests} loading={requestsLoading} error={requestsError}
        reload={() => { setRequestsLoading(true); setRequestsError(''); setRequestsAttempt(value => value + 1); }}
        hasDraft={!!draft && !operation} openRequest={openRequest} prepareGitLabRequest={prepareGitLabRequest} />
      {(approvedAccess || gitlabRequests.some(request => request.service_status === 'pending_approval')) &&
        <OnboardingNextStep approved={!!approvedAccess} reviewRead={reviewRead} markReviewRead={markReviewRead}
          openFirstTask={() => openDocument(`${profile.team_id}-first-task`)} />}
    </div>,
    projects: <ProjectLinks documents={documents} openDocument={openDocument} ask={ask} />,
    internship: <Internship access={access} ask={ask} openDocument={openDocument} teamId={profile.team_id}
      reviewRead={reviewRead} markReviewRead={markReviewRead} />,
    'ai-assistant': <AIAssistant messages={messages} searching={sending} ask={ask} openSource={openSource}
      input={chatInput} setInput={setChatInput} nextAccessStep={chatRequest?.next_step}
      openRequest={openRequest} hasDraft={!!draft && !operation} access={accessSummary(chatRequest)} error={chatError}
      retry={pending ? () => void send(pending) : undefined} newConversation={newConversation} />,
  };

  return <DashboardLayout activeTab={activeTab} setActiveTab={setActiveTab} isDarkMode={dark}
    toggleDarkMode={() => setDark(value => !value)} onSearch={searchDocuments} access={access}>
    <div className="mb-4 flex items-center justify-end gap-3 text-sm text-muted-foreground">
      <span>{profile.name} · {profile.team_id}</span>
    </div>
    {pages[activeTab]}
    <Dialog open={requestOpen} onOpenChange={open => { if (!requestBusy) setRequestOpen(open); }}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Подтвердить учебную заявку</DialogTitle>
          <DialogDescription>После подтверждения заявка поступит в учебный сервис согласования.</DialogDescription>
        </DialogHeader>
        {draft && <form onSubmit={confirmRequest} className="space-y-4">
          <p className="text-sm">Ресурс: {draft.tool_id} · {draft.resource_id}</p>
          <Label htmlFor="reason">Обоснование</Label>
          <Input id="reason" value={reason} maxLength={1000} onChange={event => setReason(event.target.value)} required />
          {requestError && <p role="alert" className="text-sm text-destructive">{requestError}</p>}
          <Button type="submit" disabled={requestBusy || !reason.trim()}>{requestBusy ? 'Отправляем…' : 'Подтвердить заявку'}</Button>
        </form>}
      </DialogContent>
    </Dialog>
    <Dialog open={!!source} onOpenChange={open => { if (!open) { ++sourceRequest.current; setSource(null); } }}>
      <DialogContent className="source-dialog sm:max-w-2xl max-h-[85vh] overflow-y-auto">
        <DialogHeader><DialogTitle>{source?.title}</DialogTitle><DialogDescription>Документ из базы знаний</DialogDescription></DialogHeader>
        {source?.error && <p role="alert">{source.error}</p>}
        {source && !source.error && !source.data && <p role="status">Загружаем документ…</p>}
        {source?.data?.chunks.map(chunk => {
          const cited = chunk.chunk_id === source.citedChunk;
          const url = originalURL(chunk.source_url);
          return <section key={chunk.chunk_id} className={`rounded-xl border p-4 ${cited ? 'border-primary bg-primary/5' : ''}`}>
            {cited && <p className="mb-2 text-xs text-muted-foreground">Фрагмент из ответа</p>}
            <h3 className="mb-2 font-medium">{chunk.title}</h3>
            <p className="whitespace-pre-wrap text-sm">{chunk.text}</p>
            {url && <a href={url} target="_blank" rel="noopener noreferrer" className="mt-3 inline-block text-sm underline">Открыть оригинал</a>}
          </section>;
        })}
      </DialogContent>
    </Dialog>
  </DashboardLayout>;
}
