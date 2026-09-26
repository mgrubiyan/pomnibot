export const CODE_LENGTH = 6;

/** Set codes read as «482 917» everywhere: on the share screen and in the input. */
export const formatCode = (digits: string) =>
    digits.length > 3 ? `${digits.slice(0, 3)} ${digits.slice(3)}` : digits;

const INVITE_PARAM = new RegExp(`^join_(\\d{${CODE_LENGTH}})$`);

/**
 * An invite link launches the mini app with `join_<code>` as its start
 * parameter; anything else is not an invite.
 */
export const codeFromStartParam = (param: string) => INVITE_PARAM.exec(param)?.[1];
