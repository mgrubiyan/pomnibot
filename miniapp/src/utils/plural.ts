const rules = new Intl.PluralRules('ru-RU');

/**
 * Picks the plural form for a number: [1 карточка, 2 карточки, 5 карточек].
 * Returns the word alone — the number is not inserted.
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

/** «карточек» alone, for when the number is set apart: «35» over «карточек». */
export const cardsWord = (count: number) => plural(count, ['карточка', 'карточки', 'карточек']);

export const cardsLabel = (count: number) => `${count} ${cardsWord(count)}`;

export const daysLabel = (count: number) =>
    `${count} ${plural(count, ['день', 'дня', 'дней'])}`;

export const errorsLabel = (count: number) =>
    `${count} ${plural(count, ['ошибка', 'ошибки', 'ошибок'])}`;

export const minutesLabel = (count: number) =>
    `${count} ${plural(count, ['минута', 'минуты', 'минут'])}`;

/** «около 3 минут» needs the genitive, which differs from the forms above. */
export const aboutMinutesLabel = (count: number) =>
    `около ${count} ${plural(count, ['минуты', 'минут', 'минут'])}`;
