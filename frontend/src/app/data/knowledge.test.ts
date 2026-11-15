import { describe, expect, it } from 'vitest';
import { answerFromDemoSources, demoKnowledge, searchDemoKnowledge } from './knowledge';

describe('synthetic knowledge search', () => {
  it('finds a relevant GitLab access instruction', () => {
    const ids = searchDemoKnowledge('Как запросить доступ к GitLab?').map(item => item.id);
    expect(ids).toContain('gitlab-access');
  });
  it('does not fabricate a grounded answer when no document matches', () => {
    expect(searchDemoKnowledge('ксилофон-снежинка-72381')).toEqual([]);
    expect(answerFromDemoSources([])).toContain('не нашлось');
  });
  it('uses only matched synthetic sources', () => {
    const doc = demoKnowledge.find(item => item.id === 'service-a');
    expect(doc).toBeDefined();
    expect(answerFromDemoSources([doc!])).toBe(doc!.summary);
  });
});
