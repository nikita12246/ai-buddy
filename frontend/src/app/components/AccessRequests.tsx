import { useState } from 'react';
import { Clipboard, KeyRound } from 'lucide-react';
import { Badge } from './ui/badge';
import { Button } from './ui/button';
import { Card, CardContent, CardHeader, CardTitle } from './ui/card';
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from './ui/dialog';
import { accessSummary, type ConfirmedRequest } from '../api/onboarding';

export function AccessRequests({ requests, loading, error, reload, hasDraft, openRequest, prepareGitLabRequest }: {
  requests: ConfirmedRequest[];
  loading: boolean;
  error: string;
  reload: () => void;
  hasDraft: boolean;
  openRequest: () => void;
  prepareGitLabRequest: () => void;
}) {
  const [summaryRequestId, setSummaryRequestId] = useState<string | null>(null);
  const [copyMessage, setCopyMessage] = useState('');
  const summaryRequest = requests.find(request => request.id === summaryRequestId);
  const selectedSummary = accessSummary(summaryRequest ?? null);
  const summaryText = summaryRequest && selectedSummary?.externalId
    ? [
        `Запрос доступа к ${summaryRequest.tool_id} · ${summaryRequest.resource_id}`,
        `Номер заявки: ${selectedSummary.externalId}`,
        `Статус на момент проверки: ${selectedSummary.status}`,
        summaryRequest.next_step,
      ].filter(Boolean).join('\n')
    : '';

  async function copySummary() {
    try {
      await navigator.clipboard.writeText(summaryText);
      setCopyMessage('Сводка скопирована.');
    } catch {
      setCopyMessage('Выдели текст сводки и скопируй его вручную.');
    }
  }

  return <section className="space-y-6 access-page">
    <h1 className="text-2xl font-medium">Запросы доступа</h1>
    <Card className="border-0 shadow-lg">
      <CardHeader><CardTitle className="flex items-center gap-2"><KeyRound className="size-5 text-primary" />Мои заявки</CardTitle></CardHeader>
      <CardContent className="space-y-4">
        {loading && <p role="status" className="text-sm text-muted-foreground">Загружаем заявки…</p>}
        {error && <div className="space-y-2">
          <p role="alert" className="text-sm text-destructive">{error}</p>
          <Button variant="outline" onClick={reload}>Обновить заявки</Button>
        </div>}
        {!error && requests.some(request => request.needs_manual_check) &&
          <Button variant="outline" onClick={reload} disabled={loading}>Обновить заявки</Button>}
        {requests.length > 0 && <ul className="space-y-3">{requests.map(request => {
          const summary = accessSummary(request);
          return <li key={request.id} className="space-y-2 rounded-xl border p-4">
            {summary && <div className="flex flex-wrap items-center gap-3">
              {summary.externalId && <span className="font-medium">{summary.externalId}</span>}
              <Badge>{summary.status}</Badge>
            </div>}
            <p className="text-sm">Ресурс: {request.tool_id} · {request.resource_id}</p>
            {request.next_step && <p className="text-sm text-muted-foreground">{request.next_step}</p>}
            {summary?.externalId && <Button type="button" variant="outline" size="sm"
              onClick={() => { setCopyMessage(''); setSummaryRequestId(request.id); }}>
              <Clipboard className="size-4" />Сводка для наставника
            </Button>}
          </li>;
        })}</ul>}
        {!loading && !error && requests.length === 0 && <p className="text-sm text-muted-foreground">Подтверждённых заявок пока нет.</p>}
        {hasDraft && <Button onClick={openRequest}>Проверить черновик</Button>}
        {!hasDraft && !loading && !error && requests.length === 0 && (
          <Button onClick={prepareGitLabRequest}>Подготовить заявку на GitLab</Button>
        )}
      </CardContent>
    </Card>
    <Dialog open={!!summaryText} onOpenChange={open => { if (!open) setSummaryRequestId(null); }}>
      <DialogContent className="mentor-summary-dialog">
        <DialogHeader>
          <DialogTitle>Сводка для наставника</DialogTitle>
          <DialogDescription>Скопируй сведения о заявке, чтобы обсудить её с наставником.</DialogDescription>
        </DialogHeader>
        <textarea aria-label="Текст сводки" readOnly value={summaryText} rows={5}
          className="w-full rounded-md border bg-background p-3 text-sm" />
        <Button type="button" onClick={() => void copySummary()}>Скопировать сводку</Button>
        {copyMessage && <p role="status" className="text-sm">{copyMessage}</p>}
      </DialogContent>
    </Dialog>
  </section>;
}
