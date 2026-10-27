import type { CardKind } from '../types';

interface KindText {
    /** The kind's name in the kind picker and on the card list. */
    label: string;
    /** What the kind is, under its name in the picker. */
    hint: string;
    /** What the card asks of the reader, said above the question in the feed. */
    task: string;
}

/**
 * Every card kind as the reader sees it, in the order they are offered:
 * object keys keep the order they are written in. A new kind without
 * text does not compile.
 */
const KINDS: Record<CardKind, KindText> = {
    flip: {
        label: 'Вопрос и ответ',
        hint: 'Вопрос на лицевой стороне, ответ на обороте',
        task: 'Вспомните ответ',
    },
    choice: {
        label: 'Тест',
        hint: 'Один или несколько верных вариантов',
        task: 'Выберите все верные ответы',
    },
    boolean: {
        label: 'Верно или нет',
        hint: 'Утверждение, которое нужно оценить',
        task: 'Верно или нет?',
    },
    input: {
        label: 'Свой ответ',
        hint: 'Ответ вписывают словом или числом',
        task: 'Ответьте своими словами',
    },
    table: {
        label: 'Таблица',
        hint: 'Понятия раскладывают по колонкам',
        task: 'Разложите по колонкам',
    },
};

/** Card kinds in the order they are offered. */
export const CARD_KINDS: ({ kind: CardKind } & KindText)[] = (Object.keys(KINDS) as CardKind[]).map(
    (kind) => ({ kind, ...KINDS[kind] }),
);

/**
 * A kind this bundle does not know yet (the backend gained one while an old
 * bundle sits in the messenger's cache) gets no text instead of a crash.
 */
const textOf = (kind: CardKind): KindText | undefined =>
    (KINDS as Partial<Record<string, KindText>>)[kind];

/** The kind's name, or an empty string for an unknown kind. */
export const kindLabel = (kind: CardKind): string => textOf(kind)?.label ?? '';

/** What the card asks of the reader, or an empty string for an unknown kind. */
export const kindTask = (kind: CardKind): string => textOf(kind)?.task ?? '';
