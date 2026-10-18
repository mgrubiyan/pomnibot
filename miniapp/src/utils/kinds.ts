import type { CardKind } from '../types';

/** Card kinds as the reader sees them, in the order they are offered. */
export const CARD_KINDS: { kind: CardKind; label: string; hint: string }[] = [
    { kind: 'flip', label: 'Флип-карточка', hint: 'Вопрос на лицевой стороне, ответ на обороте' },
    { kind: 'choice', label: 'Выбор ответа', hint: 'Один верный вариант среди нескольких' },
    { kind: 'boolean', label: 'Верно / Неверно', hint: 'Утверждение, которое нужно оценить' },
    { kind: 'input', label: 'Ввод текста', hint: 'Ответ вписывают словом или числом' },
    { kind: 'table', label: 'Таблица', hint: 'Понятия раскладывают по колонкам' },
];
