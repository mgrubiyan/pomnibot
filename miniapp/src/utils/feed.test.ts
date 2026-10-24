import { describe, expect, it } from 'bun:test';
import type { CardReveal, FeedCard } from '../types';
import { layoutOf, patchFeedCard, rejoinedFeed, resumedCards, revealedCard } from './feed';

const card = (id: string, extra: Partial<FeedCard> = {}): FeedCard => ({
    id,
    setId: 's',
    factId: `f${id}`,
    factName: `Факт ${id}`,
    kind: 'flip',
    question: `Вопрос ${id}`,
    ...extra,
});

describe('layoutOf', () => {
    const table = { columns: ['Растения', 'Животные'], items: ['кот', 'дуб'] };

    it('places no term before the answer', () => {
        expect(layoutOf(table).items).toEqual([
            { text: 'кот', column: '' },
            { text: 'дуб', column: '' },
        ]);
    });

    it('keeps the order the reader saw and takes the columns from the reveal', () => {
        const revealed = {
            columns: table.columns,
            items: [
                { text: 'дуб', column: 'Растения' },
                { text: 'кот', column: 'Животные' },
            ],
        };
        expect(layoutOf(table, revealed).items).toEqual([
            { text: 'кот', column: 'Животные' },
            { text: 'дуб', column: 'Растения' },
        ]);
    });
});

describe('patchFeedCard', () => {
    it('takes the edited question and the terms without their columns', () => {
        const patched = patchFeedCard(card('1', { kind: 'table', table: { columns: ['A'], items: ['x'] } }), {
            question: 'Новый вопрос',
            table: { columns: ['A', 'B'], items: [{ text: 'y', column: 'B' }] },
            answer: 'не для ленты',
        });
        expect(patched.question).toBe('Новый вопрос');
        expect(patched.table).toEqual({ columns: ['A', 'B'], items: ['y'] });
        expect('answer' in patched).toBe(false);
    });
});

describe('resumedCards', () => {
    const cards = [card('1'), card('2'), card('3')];

    it('drops a card reported meanwhile only from the cards ahead', () => {
        const resumed = resumedCards({ cards, index: 1, results: [] }, ['1', '2'], {});
        expect(resumed.map((c) => c.id)).toEqual(['1', '3']);
    });
});

describe('rejoinedFeed', () => {
    it('keeps the cards gone through and adds the rest of the reloaded feed', () => {
        const passed = [card('1'), card('2')];
        const feed = [card('2'), card('3'), card('4')];
        expect(rejoinedFeed(passed, feed, ['4'], {}).map((c) => c.id)).toEqual(['1', '2', '3']);
    });
});

describe('revealedCard', () => {
    const reveal: CardReveal = { answer: 'ответ', explanation: 'потому что', sourceQuote: 'цитата' };

    it('makes the whole card for the complaint and the edit', () => {
        const whole = revealedCard(card('1'), reveal);
        expect(whole.answer).toBe('ответ');
        expect(whole.sourceQuote).toBe('цитата');
        expect(whole.table).toBeUndefined();
    });

    it('gives a sorting card its placed terms', () => {
        const layout = { columns: ['A'], items: [{ text: 'x', column: 'A' }] };
        const whole = revealedCard(card('1', { kind: 'table', table: { columns: ['A'], items: ['x'] } }), { ...reveal, answer: layout });
        expect(whole.table).toEqual(layout);
    });
});
