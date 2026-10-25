import { describe, expect, it } from 'bun:test';
import type { CardKind } from '../types';
import { CARD_KINDS, kindLabel, kindTask } from './kinds';

describe('card kinds', () => {
    it('offers the kinds in the written order', () => {
        expect(CARD_KINDS.map((item) => item.kind)).toEqual(['flip', 'choice', 'boolean', 'input', 'table']);
    });

    it('names a known kind', () => {
        expect(kindLabel('choice')).toBe('Тест');
        expect(kindTask('choice')).toBe('Выберите все верные ответы');
    });

    it('gives no text for a kind this bundle does not know', () => {
        const unknown = 'cloze' as CardKind;
        expect(kindLabel(unknown)).toBe('');
        expect(kindTask(unknown)).toBe('');
    });
});
