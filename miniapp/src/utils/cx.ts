/** Joins class names, dropping false and undefined. */
export const cx = (...values: (string | false | undefined)[]) => values.filter(Boolean).join(' ');
