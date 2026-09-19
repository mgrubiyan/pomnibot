const rules = new Intl.PluralRules('ru-RU');

/**
 * Выбирает форму слова по числу: [1 карточка, 2 карточки, 5 карточек].
 * Само число не подставляет — только слово.
 */
export function plural(count: number, forms: [string, string, string]): string {
    const category = rules.select(count);

    if (category === 'one') {
        return forms[0];
    }
    if (category === 'few') {
        return forms[1];
    }
    return forms[2];
}

export const cardsLabel = (count: number) =>
    `${count} ${plural(count, ['карточка', 'карточки', 'карточек'])}`;

export const daysLabel = (count: number) =>
    `${count} ${plural(count, ['день', 'дня', 'дней'])}`;

export const minutesLabel = (count: number) =>
    `${count} ${plural(count, ['минута', 'минуты', 'минут'])}`;

/** «около 3 минут» — родительный падеж, отличается от именительного. */
export const aboutMinutesLabel = (count: number) =>
    `около ${count} ${plural(count, ['минуты', 'минут', 'минут'])}`;
