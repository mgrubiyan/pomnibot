import type { Card, CardAnswer, CardKind, TableLayout } from '../types';

/** What the user changed in a card. */
export interface CardPatch {
    kind?: CardKind;
    question?: string;
    answer?: CardAnswer;
    options?: string[];
    table?: TableLayout;
    explanation?: string;
    topic?: string;
    sourceQuote?: string;
    sourceRef?: string;
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
