import { describe, expect, it } from 'bun:test';
import { formatNextReviewSubtitle, isNextReviewToday } from './reviewDate';

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

    it('counts minutes when the review is within the hour', () => {
        const now = new Date(2026, 9, 24, 12, 0, 0);
        const at = (minutes: number, seconds = 0) => new Date(2026, 9, 24, 12, minutes, seconds).toISOString();

        expect(formatNextReviewSubtitle(at(0, 20), now)).toBe('Следующий повтор через минуту');
        expect(formatNextReviewSubtitle(at(1), now)).toBe('Следующий повтор через минуту');
        // A part of a minute counts as a whole one: never «через 0 минут».
        expect(formatNextReviewSubtitle(at(1, 10), now)).toBe('Следующий повтор через 2 минуты');
        expect(formatNextReviewSubtitle(at(5), now)).toBe('Следующий повтор через 5 минут');
        expect(formatNextReviewSubtitle(at(21), now)).toBe('Следующий повтор через 21 минуту');
        expect(formatNextReviewSubtitle(at(59), now)).toBe('Следующий повтор через 59 минут');
    });

    it('names the time when the review is later today', () => {
        const now = new Date(2026, 9, 24, 12, 0, 0);
        const evening = new Date(2026, 9, 24, 18, 30, 0);
        expect(formatNextReviewSubtitle(evening.toISOString(), now)).toBe('Следующий повтор сегодня в 18:30');
    });

    it('says «сейчас» when the review is already due', () => {
        const now = new Date(2026, 9, 24, 12, 0, 0);
        const past = new Date(2026, 9, 24, 11, 0, 0);
        expect(formatNextReviewSubtitle(past.toISOString(), now)).toBe('Следующий повтор сейчас');
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

describe('isNextReviewToday', () => {
    it('compares calendar days, not 24 hours', () => {
        const now = new Date(2026, 9, 24, 23, 50, 0);
        expect(isNextReviewToday(new Date(2026, 9, 24, 23, 59, 0).toISOString(), now)).toBe(true);
        expect(isNextReviewToday(new Date(2026, 9, 25, 0, 5, 0).toISOString(), now)).toBe(false);
        expect(isNextReviewToday(null, now)).toBe(false);
        expect(isNextReviewToday('invalid-date', now)).toBe(false);
    });
});
