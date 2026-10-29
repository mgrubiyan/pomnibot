import { expect, test } from 'bun:test';
import { miniAppAuthorization, type TelegramApp } from './platform';

test('preserves the entire original credential and uses the active SDK', () => {
    const raw = 'user=%7B%22name%22%3A%22A+B%22%7D&hash=abc&auth_date=1';
    expect(miniAppAuthorization({ WebApp: { initData: raw } })).toBe(`max ${raw}`);
    expect(miniAppAuthorization({ Telegram: { WebApp: { initData: raw } as TelegramApp }, WebApp: { initData: 'max-data' } })).toBe(`tma ${raw}`);
    expect(miniAppAuthorization({ Telegram: { WebApp: { initData: '' } as TelegramApp } })).toBeUndefined();
    expect(miniAppAuthorization({})).toBeUndefined();
});
