import { describe, expect, it } from 'bun:test';
import type { AnswerResult, Card } from '../types';
import { allCorrect, topicsWithErrors } from './results';

const card = (id: string, topic: string) => ({ id, topic }) as Card;
const result = (cardId: string, correct: boolean) => ({ cardId, correct, answeredAt: '' }) as AnswerResult;

describe('topicsWithErrors', () => {
    const cards = [card('1', 'Митоз'), card('2', 'Митоз'), card('3', 'Мейоз'), card('4', 'Апоптоз')];

    it('lists only the topics that had an error, the weakest first', () => {
        const topics = topicsWithErrors(cards, [
            result('1', true), result('2', false), result('3', false), result('4', true),
        ]);
        expect(topics.map((t) => [t.topic, t.wrong, t.total])).toEqual([['Мейоз', 1, 1], ['Митоз', 1, 2]]);
    });

    it('is empty when every answer was right', () => {
        expect(topicsWithErrors(cards, cards.map((c) => result(c.id, true)))).toEqual([]);
    });
});

describe('allCorrect', () => {
    it('sees an error on a card without a topic', () => {
        const cards = [card('1', ''), card('2', '')];
        const results = [result('1', true), result('2', false)];
        expect(topicsWithErrors(cards, results)).toEqual([]);
        expect(allCorrect(results)).toBe(false);
    });

    it('is false for a run with no answers', () => {
        expect(allCorrect([])).toBe(false);
    });

    it('is true when every answer was right', () => {
        expect(allCorrect([result('1', true), result('2', true)])).toBe(true);
    });
});
