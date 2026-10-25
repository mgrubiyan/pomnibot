import { describe, expect, it } from 'bun:test';
import type { SetRetention } from '../types';
import { retentionGraph, retentionPercent } from './retention';

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
        const target = 12 + (1 - 0.9) / (1 - graph.lower) * 112;
        expect(graph.targetY).toBeCloseTo(target);
        expect(graph.reviews.split(' L').at(-1)?.startsWith('342 ')).toBe(true);
        expect(graph.pause.split(' L').at(-1)?.startsWith('342 ')).toBe(true);
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
