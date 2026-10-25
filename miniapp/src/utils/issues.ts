import type { Card, CardIssueReason, SetIssue } from '../types';

/** `hint` is the comment field's placeholder for the reason. */
export const ISSUE_REASONS: { value: CardIssueReason; label: string; hint: string }[] = [
    { value: 'answer', label: 'Ответ не совпадает с конспектом', hint: 'Какой ответ в конспекте?' },
    { value: 'wording', label: 'Вопрос непонятно сформулирован', hint: 'Что именно непонятно?' },
    { value: 'not-in-notes', label: 'Этого нет в моём конспекте', hint: 'Например, из какой это темы' },
    { value: 'other', label: 'Другое', hint: 'Опишите, что не так с карточкой' },
];

/** The server takes at most this many characters. */
export const ISSUE_COMMENT_MAX_LENGTH = 500;

/** What the reader sends: the reason and, for «Другое» always, their words. */
export interface CardIssueReport {
    reason: CardIssueReason;
    comment: string;
}

export const issueReasonLabel = (reason: CardIssueReason) =>
    ISSUE_REASONS.find((item) => item.value === reason)?.label ?? reason;

/** The answer as one line under the question. */
export function answerPreview(card: Card): string {
    if (Array.isArray(card.answer)) {
        return card.answer.join('; ');
    }
    if (typeof card.answer === 'boolean') {
        return card.answer ? 'Верно' : 'Неверно';
    }
    if (typeof card.answer === 'object' && card.answer !== null) {
        return 'Таблица';
    }
    return String(card.answer ?? '');
}

export interface CardIssues {
    card: Card;
    total: number;
    reasons: { reason: CardIssueReason; count: number; comments: string[] }[];
    lastReportedAt: string;
}

/** One entry per card with complaints: the most complained first, then the
    most recent. A card already gone from the set is left out. */
export function cardsWithIssues(cards: Card[], issues: SetIssue[]): CardIssues[] {
    const byId = new Map(cards.map((card) => [card.id, card]));
    const grouped = new Map<string, CardIssues>();
    for (const issue of issues) {
        const card = byId.get(issue.cardId);
        if (!card) {
            continue;
        }
        const entry = grouped.get(card.id) ?? { card, total: 0, reasons: [], lastReportedAt: issue.lastReportedAt };
        entry.total += issue.count;
        entry.reasons.push({ reason: issue.reason, count: issue.count, comments: issue.comments });
        if (Date.parse(issue.lastReportedAt) > Date.parse(entry.lastReportedAt)) {
            entry.lastReportedAt = issue.lastReportedAt;
        }
        grouped.set(card.id, entry);
    }
    const result = [...grouped.values()];
    for (const entry of result) {
        entry.reasons.sort((a, b) => b.count - a.count);
    }
    return result.sort((a, b) => b.total - a.total || Date.parse(b.lastReportedAt) - Date.parse(a.lastReportedAt));
}
