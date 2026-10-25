/** Whether the user lets the interface move, not just fade. */
export const motionAllowed = () => !window.matchMedia('(prefers-reduced-motion: reduce)').matches;
