import type { Card } from '../types';

/** Что пользователь поправил в карточке. Бэкенда нет — правки живут в App. */
export interface CardPatch {
    question?: string;
    answer?: string;
    options?: string[];
}

export const applyPatch = (card: Card, patch?: CardPatch): Card =>
    patch ? { ...card, ...patch } : card;

/** Карточки набора с учётом правок и удалений этой сессии. */
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
