import { useState } from 'react';
import { BookOpen, CheckCircle2 } from 'lucide-react';
import { Button } from './ui/button';
import { Card, CardContent, CardHeader, CardTitle } from './ui/card';

export function ReviewGuide({ reviewRead, markReviewRead }: {
  reviewRead: boolean;
  markReviewRead: () => void;
}) {
  return <div className="space-y-3 text-sm">
    <ol className="list-decimal space-y-2 pl-5">
      <li>Объясни в pull request, какую задачу решает изменение и как ты его проверил.</li>
      <li>Проверь свой diff, запусти тесты и укажи, где ревьюеру стоит посмотреть внимательнее.</li>
      <li>Назначь ревьюера по правилам команды и ответь на его замечания перед слиянием.</li>
    </ol>
    {reviewRead
      ? <p role="status" className="flex items-center gap-2"><CheckCircle2 className="size-4" />Памятка прочитана</p>
      : <Button onClick={markReviewRead}>Отметить как прочитанную</Button>}
  </div>;
}

export function OnboardingNextStep({ approved, reviewRead, markReviewRead, openFirstTask }: {
  approved: boolean;
  reviewRead: boolean;
  markReviewRead: () => void;
  openFirstTask: () => void;
}) {
  const [showReview, setShowReview] = useState(false);

  return <Card className="border-0 shadow-lg">
    <CardHeader>
      <CardTitle>{approved ? 'Первая учебная задача' : 'Пока ждёшь доступ'}</CardTitle>
    </CardHeader>
    <CardContent className="space-y-4">
      {approved && <p className="text-sm text-muted-foreground">Доступ предоставлен. Открой инструкцию команды и проверь, какой README нужно прочитать.</p>}
      {approved && <Button onClick={openFirstTask}>Открыть инструкцию первой задачи</Button>}
      {!approved && <p className="text-sm text-muted-foreground">Памятку по код-ревью можно прочитать до получения доступа к репозиторию.</p>}
      {reviewRead && !showReview && <p role="status" className="flex items-center gap-2 text-sm"><CheckCircle2 className="size-4" />Памятка прочитана</p>}
      <Button type="button" variant="outline" aria-expanded={showReview} aria-controls="review-guide" onClick={() => setShowReview(value => !value)}>
        <BookOpen className="size-4" />{showReview ? 'Свернуть памятку' : 'Открыть памятку по код-ревью'}
      </Button>
      <div id="review-guide" hidden={!showReview} className="rounded-xl border p-4">
        {showReview && <ReviewGuide reviewRead={reviewRead} markReviewRead={markReviewRead} />}
      </div>
    </CardContent>
  </Card>;
}
