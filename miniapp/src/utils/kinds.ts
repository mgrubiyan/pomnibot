import type { CardKind } from '../types';

/** Every card kind as the reader sees it; a new kind without a name does not compile. */
const KINDS: Record<CardKind, { label: string; hint: string }> = {
    flip: { label: 'Вопрос и ответ', hint: 'Вопрос на лицевой стороне, ответ на обороте' },
    choice: { label: 'Тест', hint: 'Один или несколько верных вариантов' },
    boolean: { label: 'Верно или нет', hint: 'Утверждение, которое нужно оценить' },
    input: { label: 'Свой ответ', hint: 'Ответ вписывают словом или числом' },
    table: { label: 'Таблица', hint: 'Понятия раскладывают по колонкам' },
};

/** Card kinds in the order they are offered. */
export const CARD_KINDS: { kind: CardKind; label: string; hint: string }[] = (
    Object.keys(KINDS) as CardKind[]
).map((kind) => ({ kind, ...KINDS[kind] }));

/** The label of a card kind, for a marker next to the question. */
export const kindLabel = (kind: CardKind): string => KINDS[kind].label;
