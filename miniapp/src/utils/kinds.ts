import type { CardKind } from '../types';

/** Card kinds as the reader sees them, in the order they are offered. */
export const CARD_KINDS: { kind: CardKind; label: string; hint: string }[] = [
    { kind: 'flip', label: 'Вопрос и ответ', hint: 'Вопрос на лицевой стороне, ответ на обороте' },
    { kind: 'choice', label: 'Тест', hint: 'Один или несколько верных вариантов' },
    { kind: 'boolean', label: 'Верно или нет', hint: 'Утверждение, которое нужно оценить' },
    { kind: 'input', label: 'Свой ответ', hint: 'Ответ вписывают словом или числом' },
    { kind: 'table', label: 'Таблица', hint: 'Понятия раскладывают по колонкам' },
];

/** The label of a card kind, for a marker next to the question. */
export const kindLabel = (kind: CardKind): string =>
    CARD_KINDS.find((item) => item.kind === kind)?.label ?? kind;
