import { describe, expect, it } from 'bun:test';
import type { Answer, Card } from '../types';
import { allCorrect, factsWithErrors } from './results';

const card = (id: string, factId: string, factName: string) => ({ id, factId, factName }) as Card;
const result = (cardId: string, correct: boolean) => ({ cardId, correct }) as Answer;

describe('factsWithErrors', () => {
    const cards = [
        card('1', 'f1', 'Митоз'),
        card('2', 'f1', 'Митоз'),
        card('3', 'f2', 'Мейоз'),
        card('4', 'f3', 'Апоптоз'),
    ];

    it('lists only the facts that had an error, the weakest first', () => {
        const facts = factsWithErrors(cards, [
            result('1', true), result('2', false), result('3', false), result('4', true),
        ]);
        expect(facts.map((f) => [f.factName, f.wrong, f.total])).toEqual([['Мейоз', 1, 1], ['Митоз', 1, 2]]);
    });

    it('is empty when every answer was right', () => {
        expect(factsWithErrors(cards, cards.map((c) => result(c.id, true)))).toEqual([]);
    });
});

describe('allCorrect', () => {
    it('sees an error on a card without a fact', () => {
        const cards = [card('1', '', ''), card('2', '', '')];
        const results = [result('1', true), result('2', false)];
        expect(factsWithErrors(cards, results)).toEqual([]);
        expect(allCorrect(results)).toBe(false);
    });

    it('is false for a run with no answers', () => {
        expect(allCorrect([])).toBe(false);
    });

    it('is true when every answer was right', () => {
        expect(allCorrect([result('1', true), result('2', true)])).toBe(true);
    });
});
