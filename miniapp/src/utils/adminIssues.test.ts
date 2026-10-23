import { describe, expect, it } from 'bun:test';
import type { AdminCardIssue } from '../types';
import { ADMIN_ISSUES_LIMIT, adminAnswer, issuesTotal, reasonCounts } from './adminIssues';

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

describe('issuesTotal', () => {
    it('says the list is cut off once it reaches the server limit', () => {
        expect(issuesTotal(3)).toBe('Всего 3');
        expect(issuesTotal(ADMIN_ISSUES_LIMIT)).toBe(`Последние ${ADMIN_ISSUES_LIMIT}`);
    });
});
