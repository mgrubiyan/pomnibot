/** Склейка классов: false и undefined отбрасываются. */
export const cx = (...values: (string | false | undefined)[]) => values.filter(Boolean).join(' ');
