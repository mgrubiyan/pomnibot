import { describe, expect, it } from 'bun:test';
import type { AnswerResult, Card } from '../types';
import { topicsWithErrors } from './results';

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
