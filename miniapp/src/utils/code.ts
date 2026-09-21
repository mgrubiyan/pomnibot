/** Set codes read as «482 917» everywhere: on the share screen and in the input. */
export const formatCode = (digits: string) =>
    digits.length > 3 ? `${digits.slice(0, 3)} ${digits.slice(3)}` : digits;
