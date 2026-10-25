import { describe, expect, it } from 'bun:test';
import type { Card, SetIssue } from '../types';
import { answerPreview, cardsWithIssues } from './issues';

const card = (id: string) => ({ id, question: `Q${id}` }) as Card;
const issue = (cardId: string, reason: SetIssue['reason'], count: number, lastReportedAt: string, comments: string[] = []): SetIssue => ({
    cardId, reason, count, comments, lastReportedAt,
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

    it('keeps what readers wrote with each reason', () => {
        const [grouped] = cardsWithIssues([card('1')], [
            issue('1', 'other', 1, '2026-10-22T10:00:00Z', ['Опечатка']),
            issue('1', 'answer', 2, '2026-10-22T09:00:00Z', ['Ответ из другой лекции']),
        ]);
        expect(grouped.reasons).toEqual([
            { reason: 'answer', count: 2, comments: ['Ответ из другой лекции'] },
            { reason: 'other', count: 1, comments: ['Опечатка'] },
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
