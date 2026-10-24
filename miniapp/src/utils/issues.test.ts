import { describe, expect, it } from 'bun:test';
import type { Card, SetIssue } from '../types';
import { answerPreview, cardsWithIssues } from './issues';

const card = (id: string) => ({ id, question: `Q${id}` }) as Card;
const issue = (cardId: string, reason: SetIssue['reason'], count: number, lastReportedAt: string): SetIssue => ({
    cardId, reason, count, lastReportedAt,
});

describe('cardsWithIssues', () => {
    it('puts the reasons of a card together, the most complained card first', () => {
        const grouped = cardsWithIssues([card('1'), card('2')], [
            issue('1', 'answer', 1, '2026-10-22T10:00:00Z'),
            issue('2', 'wording', 1, '2026-10-22T09:00:00Z'),
            issue('2', 'not-in-notes', 2, '2026-10-22T08:00:00Z'),
        ]);
        expect(grouped.map((g) => [g.card.id, g.total, g.reasons.map((r) => r.reason)])).toEqual([
            ['2', 3, ['not-in-notes', 'wording']],
            ['1', 1, ['answer']],
        ]);
    });

    it('breaks a tie by the latest complaint', () => {
        const grouped = cardsWithIssues([card('1'), card('2')], [
            issue('1', 'answer', 1, '2026-10-22T08:00:00Z'),
            issue('2', 'answer', 1, '2026-10-22T09:00:00Z'),
        ]);
        expect(grouped.map((g) => g.card.id)).toEqual(['2', '1']);
    });

    it('skips complaints about cards that are no longer in the set', () => {
        expect(cardsWithIssues([card('1')], [issue('9', 'other', 1, '2026-10-22T08:00:00Z')])).toEqual([]);
    });
});

describe('answerPreview', () => {
    it('shows all correct choice answers in complaints', () => {
        expect(answerPreview({ ...card('1'), kind: 'choice', answer: ['A', 'B'] })).toBe('A; B');
    });
});
