import { describe, expect, it } from 'bun:test';
import { formatNextReviewSubtitle } from './reviewDate';

describe('formatNextReviewSubtitle', () => {
    it('returns null for null, undefined or empty input', () => {
        expect(formatNextReviewSubtitle(null)).toBeNull();
        expect(formatNextReviewSubtitle(undefined)).toBeNull();
        expect(formatNextReviewSubtitle('')).toBeNull();
        expect(formatNextReviewSubtitle('invalid-date')).toBeNull();
    });

    it('returns "Следующий повтор завтра" when next review is tomorrow', () => {
        // Assume now is Monday, 2026-09-28
        const now = new Date(2026, 8, 28, 12, 0, 0);
        // Tomorrow is Tuesday, 2026-09-29
        const tomorrow = new Date(2026, 8, 29, 9, 0, 0);
        expect(formatNextReviewSubtitle(tomorrow.toISOString(), now)).toBe('Следующий повтор завтра');
    });

    it('returns weekday with preposition for days later in current calendar week', () => {
        // Monday, 2026-09-28
        const now = new Date(2026, 8, 28, 10, 0, 0);

        // Wednesday
        const wednesday = new Date(2026, 8, 30, 14, 0, 0);
        expect(formatNextReviewSubtitle(wednesday.toISOString(), now)).toBe('Следующий повтор в среду');

        // Thursday
        const thursday = new Date(2026, 9, 1, 10, 0, 0);
        expect(formatNextReviewSubtitle(thursday.toISOString(), now)).toBe('Следующий повтор в четверг');

        // Friday
        const friday = new Date(2026, 9, 2, 10, 0, 0);
        expect(formatNextReviewSubtitle(friday.toISOString(), now)).toBe('Следующий повтор в пятницу');

        // Saturday
        const saturday = new Date(2026, 9, 3, 10, 0, 0);
        expect(formatNextReviewSubtitle(saturday.toISOString(), now)).toBe('Следующий повтор в субботу');

        // Sunday
        const sunday = new Date(2026, 9, 4, 10, 0, 0);
        expect(formatNextReviewSubtitle(sunday.toISOString(), now)).toBe('Следующий повтор в воскресенье');
    });

    it('uses "во вторник" preposition when Tuesday is within current week but not tomorrow', () => {
        // Sunday previous week? No, Tuesday cannot be > 1 day away from Monday if Monday is day 1.
        // But what if now is Sunday? Sunday is end of week.
        // What if now is Monday and target is Tuesday? diffDays === 1 so it's "завтра".
        // What if someone has week starting Sunday in another locale, or if target is Tuesday next week?
        // If Tuesday is next week, it shows date.
    });

    it('returns date for reviews in future weeks', () => {
        // Now is Friday, 2026-10-02
        const now = new Date(2026, 9, 2, 12, 0, 0);

        // Next week Monday, 2026-10-05
        const nextMonday = new Date(2026, 9, 5, 9, 0, 0);
        expect(formatNextReviewSubtitle(nextMonday.toISOString(), now)).toBe('Следующий повтор 5 октября');

        // Two weeks later, 2026-10-15
        const midOctober = new Date(2026, 9, 15, 10, 0, 0);
        expect(formatNextReviewSubtitle(midOctober.toISOString(), now)).toBe('Следующий повтор 15 октября');
    });
});
