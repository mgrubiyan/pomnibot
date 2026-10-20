import { describe, expect, it } from 'bun:test';
import { cardsWord } from './plural';

describe('cardsWord', () => {
    it('agrees with the number it stands next to', () => {
        const cases: [number, string][] = [
            [1, 'карточка'], [2, 'карточки'], [4, 'карточки'], [5, 'карточек'], [11, 'карточек'],
            [12, 'карточек'], [21, 'карточка'], [22, 'карточки'], [35, 'карточек'], [111, 'карточек'], [101, 'карточка'],
        ];
        for (const [count, word] of cases) {
            expect(cardsWord(count)).toBe(word);
        }
    });
});
