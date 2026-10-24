import { describe, expect, it } from 'bun:test';
import type { Card } from '../types';
import { getFactCardIds, reorderCardsInFact } from './cardsOrder';

const createMockCard = (id: string, factId: string, question: string, step: number): Card => ({
    id,
    setId: 'set-1',
    factId,
    factName: `Fact ${factId}`,
    kind: 'flip',
    question,
    answer: { string: 'answer' },
    explanation: 'explanation',
    sourceQuote: 'quote',
    step,
});

describe('cardsOrder', () => {
    const cardA = createMockCard('c-1', 'f-1', 'Card 1', 0);
    const cardB = createMockCard('c-2', 'f-1', 'Card 2', 1);
    const cardC = createMockCard('c-3', 'f-1', 'Card 3', 2);
    const otherFactCard = createMockCard('c-4', 'f-2', 'Other Fact Card', 0);

    const initialCards = [cardA, cardB, cardC, otherFactCard];

    it('moves card from start to end within fact and updates step', () => {
        const reordered = reorderCardsInFact(initialCards, 'f-1', 0, 2);
        const fact1Cards = reordered.filter((c) => c.factId === 'f-1');

        expect(fact1Cards.map((c) => c.id)).toEqual(['c-2', 'c-3', 'c-1']);
        expect(fact1Cards.map((c) => c.step)).toEqual([0, 1, 2]);

        // other fact remains untouched
        const fact2Cards = reordered.filter((c) => c.factId === 'f-2');
        expect(fact2Cards.map((c) => c.id)).toEqual(['c-4']);
    });

    it('moves card from end to start within fact', () => {
        const reordered = reorderCardsInFact(initialCards, 'f-1', 2, 0);
        const fact1Cards = reordered.filter((c) => c.factId === 'f-1');

        expect(fact1Cards.map((c) => c.id)).toEqual(['c-3', 'c-1', 'c-2']);
        expect(fact1Cards.map((c) => c.step)).toEqual([0, 1, 2]);
    });

    it('returns array of card IDs for specific fact in order', () => {
        const ids = getFactCardIds(initialCards, 'f-1');
        expect(ids).toEqual(['c-1', 'c-2', 'c-3']);
    });
});
