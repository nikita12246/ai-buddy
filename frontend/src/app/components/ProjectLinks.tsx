import { ArrowUpRight } from 'lucide-react';
import { Button } from './ui/button';
import { Card, CardContent } from './ui/card';
import type { DocumentSummary } from '../api/onboarding';

const toolNames: Record<string, string> = {
  repo: 'GitLab',
  tracker: 'Jira',
  vpn: 'VPN',
  docs: 'Confluence',
  approval: 'Согласование заявок',
  task: 'Первая задача',
};

export function ProjectLinks({ documents, openDocument, ask }: {
  documents: DocumentSummary[];
  openDocument: (id: string) => void;
  ask: (question: string) => void;
}) {
  const projectDocuments = documents.filter(document => document.kind === 'demo_policy');
  return <section className="space-y-5 projects-page">
    <h1 className="text-2xl font-medium">Проект и инструменты команды</h1>
    {!projectDocuments.length && <p>Инструкции команды пока недоступны.</p>}
    <div className="grid gap-3 sm:grid-cols-2">
      {projectDocuments.map(document => {
        const toolName = toolNames[document.id.split('-').at(-1) ?? ''];
        return <Card key={document.id}><CardContent className="space-y-3 p-4">
          <h2 className="text-lg font-semibold">{toolName ?? document.title}</h2>
          {toolName && <p className="text-sm text-muted-foreground">{document.title.replace(/^[^.]+\. /, '')}</p>}
          <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
            <Button onClick={() => openDocument(document.id)}>Открыть инструкцию</Button>
            <button type="button" className="project-ask" onClick={() => ask(`Объясни, как применить инструкцию «${document.title}».`)}>
              Спросить AI-buddy <ArrowUpRight className="size-3" aria-hidden="true" />
            </button>
          </div>
        </CardContent></Card>;
      })}
    </div>
  </section>;
}
