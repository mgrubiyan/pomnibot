/** Секунд на карточку — из концепта, там 10–15. */
const SECONDS_PER_CARD = 12;

/** Сколько примерно минут займёт пачка карточек. */
export const estimateMinutes = (cards: number) =>
    Math.max(1, Math.round((cards * SECONDS_PER_CARD) / 60));
