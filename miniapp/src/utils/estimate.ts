/** Seconds per card — the concept doc puts it at 10–15. */
const SECONDS_PER_CARD = 12;

/** Roughly how many minutes a batch of cards takes. */
export const estimateMinutes = (cards: number) =>
    Math.max(1, Math.round((cards * SECONDS_PER_CARD) / 60));
