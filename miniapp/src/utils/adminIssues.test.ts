import { describe, expect, it } from 'bun:test';
import type { AdminCardIssue } from '../types';
import { adminAnswer, reasonCounts } from './adminIssues';

const issue = (reason: AdminCardIssue['reason'], kind: AdminCardIssue['kind'] = 'flip', answer?: string): AdminCardIssue => ({
    id: crypto.randomUUID(),
    reason,
    status: 'open',
    kind,
    question: 'Q',
    answer,
    explanation: '',
    sourceQuote: '',
    reporter: { id: 1, firstName: 'Анна' },
    createdAt: '2026-10-23T10:00:00Z',
});

describe('reasonCounts', () => {
    it('counts each reason in the order of the complaint screen, leaving out the absent ones', () => {
        expect(reasonCounts([issue('other'), issue('answer'), issue('other')])).toEqual([
            { reason: 'answer', count: 1 },
            { reason: 'other', count: 2 },
        ]);
    });
});

describe('adminAnswer', () => {
    it('reads the stored answer as the card shows it', () => {
        expect(adminAnswer(issue('answer', 'boolean', 'true'))).toBe('Верно');
        expect(adminAnswer(issue('answer', 'boolean', 'false'))).toBe('Неверно');
        expect(adminAnswer(issue('answer', 'table'))).toBe('Таблица');
        expect(adminAnswer(issue('answer', 'input', 'Митоз'))).toBe('Митоз');
        expect(adminAnswer(issue('answer', 'flip'))).toBe('');
    });
});
