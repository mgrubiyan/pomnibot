import { describe, expect, it } from 'bun:test';
import type { SetRetention } from '../types';
import { ABOVE_TARGET_SHARE, retentionGraph, retentionPercent } from './retention';

const forecast: SetRetention = {
    calculatedAt: '2026-10-26T12:00:00Z', horizonDays: 13, target: 0.9,
    totalFacts: 2, current: 0.8, groups: { below70: 0, from70To90: 2, above90: 0 },
    points: [
        { day: 0, withReviews: 0.8, withoutReviews: 0.8 },
        { day: 0, withReviews: 1, withoutReviews: 0.8 },
        { day: 0.5, withReviews: 0.98, withoutReviews: 0.79 },
        { day: 13, withReviews: 0.96, withoutReviews: 0.6 },
    ], withReviews: 0.96, withoutReviews: 0.6,
};

describe('retentionGraph', () => {
    it('shares the start before the first review and preserves vertical jumps', () => {
        const graph = retentionGraph(forecast);
        const first = graph.reviews.split(' L')[0];
        expect(first).toBe(graph.pause.split(' L')[0]);
        expect(graph.reviews.split(' L')[1]).toBe('0 12');
        expect(graph.area.endsWith(' Z')).toBe(true);
    });
    it('includes both curves and the target in a common scale', () => {
        const graph = retentionGraph(forecast);
        expect(graph.lower).toBeCloseTo(0.55);
        expect(graph.targetY).toBeCloseTo(12 + ABOVE_TARGET_SHARE * 112);
        expect(graph.reviews.split(' L').at(-1)?.startsWith('342 ')).toBe(true);
        expect(graph.pause.split(' L').at(-1)?.startsWith('342 ')).toBe(true);
    });
    it('gives the band above the target the same height however low the pause falls', () => {
        const low = { ...forecast, withoutReviews: 0, points: [...forecast.points.slice(0, -1), { day: 13, withReviews: 0.96, withoutReviews: 0 }] };
        for (const data of [forecast, low]) {
            const graph = retentionGraph(data);
            const yOf = (path: string, index: number) => Number(path.split(' L')[index].split(' ')[1]);
            // 1 → the top, the target → 40% down; 0.98 sits a fifth of the way between them.
            expect(yOf(graph.reviews, 1)).toBeCloseTo(12);
            expect(yOf(graph.reviews, 2)).toBeCloseTo(12 + 0.2 * (graph.targetY - 12));
        }
    });
    it('separates coinciding endpoint labels and clamps the scale at zero', () => {
        const data = { ...forecast, current: 0, withReviews: 0, withoutReviews: 0,
            points: [{ day: 0, withReviews: 0, withoutReviews: 0 }, { day: 13, withReviews: 0, withoutReviews: 0 }] };
        const graph = retentionGraph(data);
        expect(graph.lower).toBe(0);
        expect(Math.abs(graph.pauseLabelY - graph.reviewLabelY)).toBeGreaterThanOrEqual(16);
        expect(graph.pauseLabelY).toBeLessThanOrEqual(132);
        expect(graph.reviews.includes('NaN')).toBe(false);
    });
    it('rounds percentages only for display', () => {
        expect(retentionPercent(forecast.withReviews)).toBe('96%');
        expect(retentionPercent(forecast.withoutReviews)).toBe('60%');
        expect(retentionPercent(0.94501)).toBe('95%');
        expect(forecast.withReviews).toBe(0.96);
    });
});
