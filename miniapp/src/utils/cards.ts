import type { Card } from '../types';

/** What the user changed in a card. No backend yet, so edits live in App. */
export interface CardPatch {
    question?: string;
    answer?: string;
    options?: string[];
}

export const applyPatch = (card: Card, patch?: CardPatch): Card =>
    patch ? { ...card, ...patch } : card;

/** Cards of a set with this session's edits and deletions applied. */
export function visibleCards(
    cards: Card[],
    setId: string | undefined,
    removedIds: string[],
    patches: Record<string, CardPatch>,
): Card[] {
    return cards
        .filter((card) => (setId ? card.setId === setId : true))
        .filter((card) => !removedIds.includes(card.id))
        .map((card) => applyPatch(card, patches[card.id]));
}
