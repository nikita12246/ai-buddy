import { useMemo, useState } from 'react';
import { Card, CardContent, CardHeader, CardTitle } from './ui/card';
import { Button } from './ui/button';
import { Badge } from './ui/badge';
import { Input } from './ui/input';
import { Tabs, TabsContent, TabsList, TabsTrigger } from './ui/tabs';
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogTrigger } from './ui/dialog';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from './ui/table';
import { CheckCircle2, Clock3, LockKeyhole, BookOpen, Search, Sparkles, Target, ArrowUpRight } from 'lucide-react';
import { ReviewGuide } from './OnboardingNextStep';
import { goals, initialTasks, onboardingSteps, type Task } from '../data/demo';
import type { AccessRequest } from '../api/onboarding';

export function Internship({ access, ask, openDocument, teamId, reviewRead, markReviewRead }: {
  access: AccessRequest | null;
  ask: (question: string) => void;
  openDocument: (id: string) => void;
  teamId: string;
  reviewRead: boolean;
  markReviewRead: () => void;
}) {
  const [taskQuery,setTaskQuery]=useState(''); const [status,setStatus]=useState('Все'); const [selectedTask,setSelectedTask]=useState<Task|null>(null); const [selectedStep,setSelectedStep]=useState(onboardingSteps[1]);
  const filtered=useMemo(()=>initialTasks.map(task=>task.id==='ONB-014'&&access?.status==='Доступ предоставлен'?{...task,status:'Готово' as const,blocker:undefined}:task).filter(task=>(status==='Все'||task.status===status)&&`${task.title} ${task.why}`.toLowerCase().includes(taskQuery.toLowerCase())),[status,taskQuery,access?.status]);
  const accessStatus=access?.status==='Доступ предоставлен'?'done':access?'progress':'blocked';
  return <div className="space-y-6 internship-page"><div className="internship-heading"><h1 className="text-2xl font-medium">Моя стажировка</h1><p className="text-muted-foreground">Первые шаги, рабочие задачи и цели. Все персональные данные на экране демонстрационные.</p></div><div className="grid grid-cols-1 md:grid-cols-3 gap-6 internship-metrics">
    <Card className="border-0 shadow-lg">
      <CardContent className="p-6">
        <LockKeyhole className="mb-4 size-5 text-primary" />
        <p className="text-xs text-muted-foreground">Текущий этап</p>
        <p className="mt-1 font-medium">Инструменты и доступы</p>
        <p className="mt-1 text-xs text-muted-foreground">{access?.status || 'Запрос ещё нужен'}</p>
      </CardContent>
    </Card>
    <Dialog>
      <Card className="border-0 shadow-lg">
        <DialogTrigger asChild>
          <button type="button" className="internship-review-card">
            <BookOpen className="mb-4 size-5 text-primary" />
            <span className="text-xs text-muted-foreground">Доступная задача</span>
            <span className="mt-1 flex items-center gap-2 font-medium">Изучить code review <ArrowUpRight className="size-4" aria-hidden="true" /></span>
            <span className="mt-1 text-xs text-muted-foreground">{reviewRead ? 'Памятка прочитана' : 'Открыть памятку'}</span>
          </button>
        </DialogTrigger>
      </Card>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Памятка по код-ревью</DialogTitle>
          <DialogDescription>Что проверить перед отправкой pull request.</DialogDescription>
        </DialogHeader>
        <ReviewGuide reviewRead={reviewRead} markReviewRead={markReviewRead} />
      </DialogContent>
    </Dialog>
    <Card className="border-0 shadow-lg">
      <CardContent className="p-6">
        <Target className="mb-4 size-5 text-primary" />
        <p className="text-xs text-muted-foreground">Следующее обсуждение</p>
        <p className="mt-1 font-medium">Встреча с наставником</p>
        <p className="mt-1 text-xs text-muted-foreground">Дата не указана</p>
      </CardContent>
    </Card>
  </div>
  <Card className="border-0 shadow-lg internship-board"><CardContent className="p-6"><Tabs defaultValue="adaptation"><TabsList className="mb-5"><TabsTrigger value="adaptation">Адаптация</TabsTrigger><TabsTrigger value="tasks">Мои задачи</TabsTrigger><TabsTrigger value="growth">Мой рост</TabsTrigger></TabsList><TabsContent value="adaptation"><div className="grid lg:grid-cols-3 gap-5"><div className="lg:col-span-2 space-y-3">{onboardingSteps.map((step,index)=>{const realStatus=step.id==='access'?accessStatus:step.id==='process'&&accessStatus==='done'?'progress':step.status;return <button key={step.id} onClick={()=>setSelectedStep(step)} aria-pressed={selectedStep.id===step.id} className="internship-step w-full text-left rounded-xl border p-4 flex gap-4 hover:bg-accent/40 transition-colors"><span className={`w-9 h-9 rounded-full flex items-center justify-center shrink-0 ${realStatus==='done'?'bg-brand-green text-black':realStatus==='blocked'?'bg-brand-red text-white':'bg-black text-primary'}`}>{realStatus==='done'?<CheckCircle2 className="w-5 h-5"/>:index+1}</span><span className="flex-1"><b className="block">{step.title}</b><span className="text-sm text-muted-foreground">{step.date}</span></span><Badge variant="secondary">{realStatus==='done'?'Завершено':realStatus==='progress'?'В процессе':realStatus==='blocked'?'Зависит от доступа':'Следующий этап'}</Badge></button>})}</div><div className="border rounded-xl p-5 h-fit"><Badge variant="secondary">Выбранный этап</Badge><h3 className="text-lg font-medium mt-3">{selectedStep.title}</h3><p className="text-sm text-muted-foreground mt-2">{selectedStep.id==='access'&&accessStatus==='done'?'Доступ к GitLab предоставлен. Теперь можно клонировать репозиторий и продолжить настройку окружения.':selectedStep.detail}</p><p className="text-xs mt-4">Ответственный: {selectedStep.owner}</p><p className="text-xs text-muted-foreground">Источник: {selectedStep.source}</p><Button variant="outline" className="mt-5" onClick={()=>selectedStep.id==='access'?openDocument(teamId+'-repo'):selectedStep.id==='process'?ask('Как проходит code review?'):ask(`Что делать на этапе ${selectedStep.title}?`)}>{selectedStep.id==='access'&&accessStatus==='done'?'Открыть инструкцию':selectedStep.action}<ArrowUpRight className="w-4 h-4" /></Button></div></div></TabsContent>
    <TabsContent value="tasks"><div className="flex flex-wrap gap-2 mb-4"><div className="relative"><Search className="w-4 h-4 absolute left-3 top-2.5 text-muted-foreground" /><Input value={taskQuery} onChange={event=>setTaskQuery(event.target.value)} placeholder="Поиск задач" className="pl-9 w-56" /></div>{['Все','Заблокирована','В работе','К выполнению','Готово'].map(item=><Button key={item} size="sm" variant={status===item?'secondary':'ghost'} onClick={()=>setStatus(item)}>{item}</Button>)}</div>{filtered.length?<div className="overflow-x-auto"><Table><TableHeader><TableRow><TableHead>Задача</TableHead><TableHead>Статус</TableHead><TableHead>Срок</TableHead><TableHead>Источник</TableHead><TableHead></TableHead></TableRow></TableHeader><TableBody>{filtered.map(task=>{const actual=task.id==='ONB-014'&&access?.status==='Доступ предоставлен'?'Готово':task.status;return <TableRow key={task.id} className="cursor-pointer" tabIndex={0} aria-label={`Открыть задачу ${task.title}`} onClick={()=>setSelectedTask(task)} onKeyDown={event=>{if(event.key==='Enter'||event.key===' '){event.preventDefault();setSelectedTask(task)}}}><TableCell className="font-medium">{task.title}<p className="text-xs text-muted-foreground">{task.id} · {task.why}</p></TableCell><TableCell><Badge variant="secondary">{actual}</Badge></TableCell><TableCell>{task.due}</TableCell><TableCell className="text-muted-foreground">{task.source}</TableCell><TableCell><ArrowUpRight className="w-4 h-4" /></TableCell></TableRow>})}</TableBody></Table></div>:<p className="text-sm text-muted-foreground py-10 text-center">По выбранным фильтрам задач нет.</p>}</TabsContent>
    <TabsContent value="growth"><div className="rounded-xl bg-accent text-accent-foreground border border-border p-4 mb-4 text-sm">Контрольные точки учебные. Подтверждённые достижения и реальная обратная связь наставника не подключены.</div><div className="grid md:grid-cols-2 gap-4">{goals.map(goal=><div key={goal.title} className="border rounded-xl p-5"><div className="flex justify-between gap-2"><Badge variant="secondary">{goal.tag}</Badge><span className="text-xs text-muted-foreground">{goal.agreed?'План согласован · демо':'Требует согласования'}</span></div><h3 className="font-medium mt-3">{goal.title}</h3><p className="text-sm mt-2"><b>Ожидаемый результат:</b> {goal.result}</p><p className="text-sm text-muted-foreground mt-2">{goal.feedback}</p><p className="text-xs text-muted-foreground mt-4">Подтверждённое достижение: пока нет</p></div>)}</div></TabsContent></Tabs></CardContent></Card>
    <Dialog open={!!selectedTask} onOpenChange={open=>!open&&setSelectedTask(null)}><DialogContent><DialogHeader><DialogTitle>{selectedTask?.title}</DialogTitle><DialogDescription>{selectedTask?.id} · {selectedTask?.source}</DialogDescription></DialogHeader>{selectedTask&&<div className="space-y-3 text-sm"><p>{selectedTask.why}</p><p className="font-medium">Критерии готовности</p><ul className="list-disc pl-5 text-muted-foreground">{selectedTask.criteria.map(item=><li key={item}>{item}</li>)}</ul>{selectedTask.blocker&&access?.status!=='Доступ предоставлен'&&<p className="text-destructive">Блокер: {selectedTask.blocker}</p>}<Button onClick={()=>{ask(`Помоги разобраться с задачей ${selectedTask.title}`);setSelectedTask(null)}}><Sparkles className="w-4 h-4" />Спросить AI-buddy</Button></div>}</DialogContent></Dialog>
  </div>;
}
