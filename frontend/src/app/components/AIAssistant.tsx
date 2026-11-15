import { useEffect, useRef } from 'react';
import { BookOpen, FileText, KeyRound, LoaderCircle, Network, Send, Sparkles } from 'lucide-react';
import { Card, CardContent, CardHeader, CardTitle } from './ui/card';
import { Button } from './ui/button';
import { Input } from './ui/input';
import { ScrollArea } from './ui/scroll-area';
import { DecorativeFlower } from './DecorativeFlower';
import type { Message } from '../App';
import type { AccessRequest, Citation } from '../api/onboarding';

const quickActions = [
  { icon: BookOpen, query: 'Где смотреть задачи команды в Jira?' },
  { icon: Network, query: 'Как подключиться к учебному VPN?' },
  { icon: KeyRound, query: 'Нужен доступ к учебному GitLab' },
  { icon: KeyRound, query: 'Проверить статус заявки' },
];

type Props = {
  messages: Message[];
  searching: boolean;
  input: string;
  setInput: (input: string) => void;
  ask: (question: string) => void;
  openSource: (citation: Citation) => void;
  openRequest: () => void;
  hasDraft: boolean;
  access: AccessRequest | null;
  nextAccessStep?: string;
  error: string;
  retry?: () => void;
  newConversation: () => void;
};

export function AIAssistant({
  messages, searching, input, setInput, ask, openSource, openRequest,
  hasDraft, access, nextAccessStep, error, retry, newConversation,
}: Props) {
  const bottom = useRef<HTMLDivElement>(null);
  useEffect(() => { bottom.current?.scrollIntoView({ block: 'nearest', behavior: 'smooth' }); }, [messages, searching]);

  const requestCard = (hasDraft || access) && (
    <Card className="studio-request-card border-0 shadow-lg">
      <CardHeader><CardTitle className="text-base">{hasDraft ? 'Черновик заявки' : 'Заявка на доступ'}</CardTitle></CardHeader>
      <CardContent className="space-y-3">
        {hasDraft ? (
          <Button onClick={openRequest} disabled={searching}><KeyRound className="size-4" />Проверить черновик</Button>
        ) : access && <>
          <p className="font-medium">{[access.externalId, access.status].filter(Boolean).join(' · ')}</p>
          {nextAccessStep && <p className="text-sm text-muted-foreground">{nextAccessStep}</p>}
        </>}
      </CardContent>
    </Card>
  );

  return <div className="space-y-4 assistant-page">
    <div className="flex items-start justify-between gap-3">
      <div className="assistant-heading">
        <DecorativeFlower tone="blue" className="assistant-flower" />
        <h1 className="flex items-center gap-3 text-2xl font-medium"><Sparkles className="size-6 text-primary" />AI-buddy</h1>
      </div>
      <Button variant="outline" onClick={newConversation} disabled={searching}>Новый диалог</Button>
    </div>
    <div className="studio-chat-layout grid grid-cols-1 gap-4 xl:grid-cols-3">
      <Card className="studio-chat-card border-0 shadow-lg xl:col-span-2">
        <CardContent className="p-0">
          <ScrollArea className="h-[460px] p-5">
            <div className="space-y-5">
              {messages.map(message => <div key={message.id} className={`flex ${message.who === 'user' ? 'justify-end' : 'justify-start'}`}>
                <div className={`max-w-[90%] rounded-2xl p-4 text-sm ${message.who === 'user' ? 'bg-primary text-primary-foreground' : 'bg-muted/60'}`}>
                  <p className="whitespace-pre-wrap leading-6">{message.text}</p>
                  {message.clarification && message.clarification.trim() !== message.text.trim() &&
                    <p className="mt-3 font-medium">{message.clarification}</p>}
                  {!!message.sources?.length && <div className="studio-chat-sources mt-3 border-t border-border pt-2">
                    <ul className="space-y-2">{message.sources.map(source =>
                      <li key={source.chunk_id} className="min-w-0">
                        <button type="button" className="studio-chat-source" onClick={() => openSource(source)}>
                          <FileText className="size-4 shrink-0" aria-hidden="true" />{source.title}
                        </button>
                        {source.excerpt && <p className="line-clamp-1 text-xs text-muted-foreground">{source.excerpt}</p>}
                      </li>)}</ul>
                  </div>}
                </div>
              </div>)}
              {searching && <p role="status" className="flex items-center gap-2 text-sm text-muted-foreground"><LoaderCircle className="size-4 animate-spin" />Готовлю ответ…</p>}
              {error && <div className="flex flex-wrap items-center gap-2">
                <p role="alert" className="text-sm text-destructive">{error}</p>
                {retry && <Button size="sm" variant="outline" onClick={retry} disabled={searching}>Повторить</Button>}
              </div>}
              <div ref={bottom} />
            </div>
          </ScrollArea>
          <form className="flex gap-2 border-t p-4" onSubmit={event => {
            event.preventDefault();
            if (!input.trim() || searching || retry) return;
            ask(input);
            setInput('');
          }}>
            <Input value={input} onChange={event => setInput(event.target.value)} maxLength={4000}
              placeholder="Спроси о документации, проекте или доступе…" aria-label="Вопрос AI-buddy" />
            <Button type="submit" disabled={!input.trim() || searching || !!retry} aria-label="Отправить"><Send className="size-4" /></Button>
          </form>
        </CardContent>
      </Card>
      <div className="studio-chat-rail space-y-4">
        {requestCard}
        <Card className="border-0 shadow-lg">
          <CardHeader><CardTitle className="text-base">Что спросить у AI-buddy</CardTitle></CardHeader>
          <CardContent className="space-y-2">
            {quickActions.map(({ icon: Icon, query }) =>
            <Button key={query} variant="outline" className="h-auto w-full justify-start whitespace-normal py-2 text-left" disabled={searching || !!retry} onClick={() => ask(query)}>
              <Icon className="size-4 shrink-0" />{query}
            </Button>)}</CardContent>
        </Card>
      </div>
    </div>
  </div>;
}
