import type { TableLayout } from '../types';

/** Where each term landed: term index -> column, or null. */
export type Placement = (string | null)[];

export const emptyPlacement = (layout: TableLayout): Placement => layout.items.map(() => null);

export const placedCount = (placement: Placement) =>
    placement.filter((column) => column !== null).length;

export const correctCount = (layout: TableLayout, placement: Placement) =>
    layout.items.reduce(
        (total, item, index) => (placement[index] === item.column ? total + 1 : total),
        0,
    );
