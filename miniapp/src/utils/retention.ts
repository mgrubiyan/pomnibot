import type { SetRetention } from '../types';

export function retentionPercent(value: number): string {
    return `${Math.round(value * 100)}%`;
}

/** Both scenarios and the target share one scale. Coordinates are in Saw's SVG space. */
export function retentionGraph(data: SetRetention) {
    const minimum = data.points.reduce((lowest, point) =>
        Math.min(lowest, point.withReviews, point.withoutReviews), data.target);
    const lower = Math.max(0, minimum - 0.05);
    const y = (value: number) => 12 + (1 - value) / (1 - lower) * 112;
    const coordinates = (kind: 'withReviews' | 'withoutReviews') => data.points.map((point) =>
        `${point.day / data.horizonDays * 342} ${y(point[kind])}`);
    const reviews = coordinates('withReviews');
    const pause = coordinates('withoutReviews');
    const path = (points: string[]) => points.length ? `M${points.join(' L')}` : '';
    // Keep labels inside the SVG with at least 16px between baselines,
    // including when both scenarios finish at the same probability.
    let reviewLabelY = Math.max(14, Math.min(120, y(data.withReviews) - 8));
    let pauseLabelY = Math.max(14, Math.min(132, y(data.withoutReviews) - 8));
    if (Math.abs(reviewLabelY - pauseLabelY) < 16) {
        reviewLabelY = Math.min(reviewLabelY, 116);
        pauseLabelY = reviewLabelY + 16;
    }
    return {
        lower,
        reviews: path(reviews),
        pause: path(pause),
        area: reviews.length ? `${path(reviews)} L${pause.toReversed().join(' L')} Z` : '',
        targetY: y(data.target),
        startY: y(data.current),
        reviewLabelY,
        pauseLabelY,
    };
}
