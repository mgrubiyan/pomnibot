import { describe, expect, it } from 'bun:test';
import { formatTodayDate } from './today';

describe('formatTodayDate', () => {
    it('names the weekday and the date, capitalized', () => {
        expect(formatTodayDate(new Date(2026, 9, 18))).toBe('Воскресенье, 18 октября');
        expect(formatTodayDate(new Date(2026, 0, 1))).toBe('Четверг, 1 января');
    });
});
