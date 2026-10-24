import type { Card, Fact } from '../types';

export interface FactGroup {
    factId: string;
    factName: string;
    cards: Card[];
}

/**
 * Groups cards by facts, preserving all facts (even empty ones) in their defined order,
 * sorting cards inside each fact by step ASC (then id ASC), and placing any orphan cards
 * into a fallback group.
 */
export function groupCardsByFacts(facts: Fact[], cards: Card[]): FactGroup[] {
    const sortCards = (a: Card, b: Card) =>
        (a.step ?? 0) - (b.step ?? 0) || a.id.localeCompare(b.id);

    const factMap = new Map<string, Card[]>();
    for (const fact of facts) {
        factMap.set(fact.id, []);
    }

    const orphanCards: Card[] = [];

    for (const card of cards) {
        if (card.factId && factMap.has(card.factId)) {
            factMap.get(card.factId)!.push(card);
        } else {
            orphanCards.push(card);
        }
    }

    const groups: FactGroup[] = facts.map((fact) => {
        const factCards = factMap.get(fact.id) || [];
        factCards.sort(sortCards);
        return {
            factId: fact.id,
            factName: fact.name,
            cards: factCards,
        };
    });

    if (orphanCards.length > 0) {
        orphanCards.sort(sortCards);
        groups.push({
            factId: 'orphan',
            factName: 'Другие карточки',
            cards: orphanCards,
        });
    }

    return groups;
}
