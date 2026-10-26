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

    it('gives each copy of a repeated term its own column', () => {
        const repeated = { columns: ['Чётные', 'Простые'], items: ['2', '4', '2'] };
        const revealed = {
            columns: repeated.columns,
            items: [
                { text: '2', column: 'Чётные' },
                { text: '4', column: 'Чётные' },
                { text: '2', column: 'Простые' },
            ],
        };
        expect(layoutOf(repeated, revealed).items.map((item) => item.column)).toEqual(['Чётные', 'Чётные', 'Простые']);
    });

    it('leaves a repeated term in a column it belongs to, as the backend grades it', () => {
        const repeated = { columns: ['Чётные', 'Простые'], items: ['2', '4', '2'] };
        const revealed = {
            columns: repeated.columns,
            items: [
                { text: '2', column: 'Чётные' },
                { text: '4', column: 'Чётные' },
                { text: '2', column: 'Простые' },
            ],
        };
        const placed = ['Простые', 'Простые', 'Чётные'];
        expect(layoutOf(repeated, revealed, placed).items.map((item) => item.column)).toEqual([
            'Простые',
            'Чётные',
            'Чётные',
        ]);
    });
});

describe('patchFeedCard', () => {
    it('drops an outdated question illustration after an edit', () => {
        const original = card('1', { questionSvg: '<svg viewBox="0 0 1 1"/>' });
        expect(patchFeedCard(original, { explanation: 'Новая формула $x$' }).questionSvg).toBeUndefined();
        expect(patchFeedCard(original).questionSvg).toBe(original.questionSvg);
    });
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

    it('keeps question and revealed explanation images in the full card', () => {
        const whole = revealedCard(card('1', { questionSvg: '<svg viewBox="0 0 1 1"/>' }), { ...reveal, explanationSvg: '<svg viewBox="0 0 2 2"/>' });
        expect(whole.questionSvg).toBe('<svg viewBox="0 0 1 1"/>');
        expect(whole.explanationSvg).toBe('<svg viewBox="0 0 2 2"/>');
    });

    it('gives a sorting card its placed terms', () => {
        const layout = { columns: ['A'], items: [{ text: 'x', column: 'A' }] };
        const whole = revealedCard(card('1', { kind: 'table', table: { columns: ['A'], items: ['x'] } }), { ...reveal, answer: layout });
        expect(whole.table).toEqual(layout);
    });
});

it('updates PDF attachments when resuming an edited feed and keeps reveal images', () => {
    const image = { assetId: 'a', url: '/api/images/a', alt: 'Figure', width: 100, height: 50, sourcePage: 2, sourceName: 'book.pdf' };
    const original = card('1', { questionImages: [image] });
    expect(patchFeedCard(original, { question: 'Updated' }).questionImages).toEqual([image]);
    expect(patchFeedCard(original, { questionImages: [] }).questionImages).toEqual([]);
    const full = revealedCard(original, { answer: 'A', explanation: 'E', sourceQuote: '', explanationImages: [image] });
    expect(full.questionImages).toEqual([image]);
    expect(full.explanationImages).toEqual([image]);
});
