import { useState } from 'react';
import { ArrowUpRight, Sparkles } from 'lucide-react';
import { Button } from './ui/button';
import { Input } from './ui/input';
import { documentKinds, type DocumentSummary } from '../api/onboarding';

export function KnowledgeBase({ documents, loading, error, query, setQuery, reload, openDocument, ask }: {
  documents: DocumentSummary[];
  loading: boolean;
  error: string;
  query: string;
  setQuery: (query: string) => void;
  reload: () => void;
  openDocument: (id: string) => void;
  ask: (question: string) => void;
}) {
  const [kind, setKind] = useState('');
  const filtered = documents.filter(document =>
    (!kind || document.kind === kind) && document.title.toLowerCase().includes(query.trim().toLowerCase()));
  return <section className="space-y-5 knowledge-page">
    <h1 className="text-2xl font-medium">База знаний</h1>
    <div className="flex flex-wrap items-center gap-3">
      <Input className="w-64" aria-label="Найти документ по названию" placeholder="Название документа" value={query} onChange={event => setQuery(event.target.value)} />
      <select className="rounded-md border bg-background px-3 text-sm" aria-label="Тип источника" value={kind} onChange={event => setKind(event.target.value)}>
        <option value="">Все источники</option>
        {Object.entries(documentKinds).map(([id, name]) => <option key={id} value={id}>{name}</option>)}
      </select>
      <Button variant="outline" disabled={!query.trim()} onClick={() => ask(query)}><Sparkles className="size-4" />Спросить по этой теме</Button>
      {!loading && !error && <p role="status" className="text-sm text-muted-foreground">Найдено: {filtered.length}</p>}
    </div>
    {loading && <p role="status">Загружаем документы…</p>}
    {error && <div role="alert" className="space-y-2"><p>{error}</p><Button variant="outline" onClick={reload}>Повторить загрузку</Button></div>}
    {!error && !loading && !filtered.length && <p>Подходящих документов нет.</p>}
    <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
      {filtered.map(document => (
        <button key={document.id} type="button" data-slot="card" className="knowledge-document-card"
          aria-label={document.title} onClick={() => openDocument(document.id)}>
          <span className="min-w-0">
            <span className="block text-xs text-muted-foreground">{documentKinds[document.kind] ?? document.kind}</span>
            <span className="mt-2 block font-semibold">{document.title}</span>
          </span>
          <span className="knowledge-document-arrow" aria-hidden="true"><ArrowUpRight className="size-4" /></span>
        </button>
      ))}
    </div>
  </section>;
}
