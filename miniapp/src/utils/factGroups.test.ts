import { describe, expect, it } from 'bun:test';
import type { Card, Fact } from '../types';
import { groupCardsByFacts } from './factGroups';

const createCard = (id: string, factId: string | undefined, question: string, step: number): Card => ({
    id,
    setId: 'set-1',
    factId,
    factName: factId ? `Fact ${factId}` : undefined,
    kind: 'flip',
    question,
    answer: { string: 'ans' },
    step,
});

describe('groupCardsByFacts', () => {
    it('creates groups for all facts in order, including empty facts', () => {
        const facts: Fact[] = [
            { id: 'f-1', setId: 'set-1', name: 'Fact 1' },
            { id: 'f-2', setId: 'set-1', name: 'Fact 2 (Empty)' },
            { id: 'f-3', setId: 'set-1', name: 'Fact 3' },
        ];

        const cards: Card[] = [
            createCard('c-1', 'f-1', 'Q1', 1),
            createCard('c-2', 'f-1', 'Q0', 0),
            createCard('c-3', 'f-3', 'Q3', 0),
        ];

        const groups = groupCardsByFacts(facts, cards);

        expect(groups).toHaveLength(3);
        expect(groups[0].factId).toBe('f-1');
        expect(groups[0].cards.map((c) => c.id)).toEqual(['c-2', 'c-1']); // ordered by step ASC: 0, 1

        expect(groups[1].factId).toBe('f-2');
        expect(groups[1].cards).toHaveLength(0); // empty fact preserved

        expect(groups[2].factId).toBe('f-3');
        expect(groups[2].cards.map((c) => c.id)).toEqual(['c-3']);
    });

    it('collects orphan cards into a fallback group if any exist', () => {
        const facts: Fact[] = [
            { id: 'f-1', setId: 'set-1', name: 'Fact 1' },
        ];

        const cards: Card[] = [
            createCard('c-1', 'f-1', 'Q1', 0),
            createCard('c-legacy', undefined, 'Legacy Card', 0),
        ];

        const groups = groupCardsByFacts(facts, cards);

        expect(groups).toHaveLength(2);
        expect(groups[0].factId).toBe('f-1');
        expect(groups[1].factId).toBe('orphan');
        expect(groups[1].cards.map((c) => c.id)).toEqual(['c-legacy']);
    });
});
