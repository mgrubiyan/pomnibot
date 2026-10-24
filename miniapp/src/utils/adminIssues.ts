import type { AdminCardIssue, CardIssueReason, CardIssueStatus } from '../types';
import { ISSUE_REASONS } from './issues';

export const STATUS_LABELS: Record<CardIssueStatus, string> = {
    open: 'Ждёт автора',
    fixed: 'Исправлена',
    kept: 'Автор оставил как есть',
    deleted: 'Карточка удалена',
};

/** How many complaints the server sends at most: LIMIT in ListCardIssues. */
export const ADMIN_ISSUES_LIMIT = 1000;

/** The header line; at the limit older complaints are cut off, so it says so. */
export function issuesTotal(count: number): string {
    return count >= ADMIN_ISSUES_LIMIT ? `Последние ${ADMIN_ISSUES_LIMIT}` : `Всего ${count}`;
}

/** Reasons short enough for a row of filters. */
export const REASON_FILTERS: Record<CardIssueReason, string> = {
    answer: 'Ответ',
    wording: 'Формулировка',
    'not-in-notes': 'Нет в конспекте',
    other: 'Другое',
};

/** How many complaints of each reason, in the order of the complaint screen. */
export function reasonCounts(issues: AdminCardIssue[]): { reason: CardIssueReason; count: number }[] {
    return ISSUE_REASONS.map(({ value }) => ({
        reason: value,
        count: issues.filter((issue) => issue.reason === value).length,
    })).filter(({ count }) => count > 0);
}

/** The answer stored with the copy of the card, as the card shows it. */
export function adminAnswer(issue: AdminCardIssue): string {
    if (issue.kind === 'table') {
        return 'Таблица';
    }
    if (issue.kind === 'boolean') {
        return issue.answer === 'true' ? 'Верно' : 'Неверно';
    }
    return issue.answer ?? '';
}
