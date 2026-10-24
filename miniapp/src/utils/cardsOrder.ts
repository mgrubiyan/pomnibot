import type { Card } from '../types';

/**
 * Reorders cards inside a specific fact by moving the item at fromIndex to toIndex,
 * and updates each card's step to reflect its new 0-based position within the fact.
 */
export function reorderCardsInFact(
    allCards: Card[],
    factId: string,
    fromIndex: number,
    toIndex: number,
): Card[] {
    const factCards = allCards.filter((c) => c.factId === factId);
    if (
        fromIndex < 0 ||
        fromIndex >= factCards.length ||
        toIndex < 0 ||
        toIndex >= factCards.length ||
        fromIndex === toIndex
    ) {
        return allCards;
    }

    const reorderedFactCards = [...factCards];
    const [moved] = reorderedFactCards.splice(fromIndex, 1);
    reorderedFactCards.splice(toIndex, 0, moved);

    const updatedFactCards = reorderedFactCards.map((card, idx) => ({
        ...card,
        step: idx,
    }));

    let nextFactCardIdx = 0;
    return allCards.map((c) => {
        if (c.factId === factId) {
            const updated = updatedFactCards[nextFactCardIdx];
            nextFactCardIdx++;
            return updated;
        }
        return c;
    });
}

/**
 * Returns an array of card IDs for the given fact in their current order.
 */
export function getFactCardIds(allCards: Card[], factId: string): string[] {
    return allCards
        .filter((c) => c.factId === factId)
        .map((c) => c.id);
}
