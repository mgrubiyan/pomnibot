/** Line breaks of pasted text as spaces, for fields that hold one line of meaning. */
export const flattenLines = (text: string) => text.replace(/\s*\n\s*/g, ' ');
