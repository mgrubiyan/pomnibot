import { expect, test } from 'bun:test';
import { miniAppAuthorization } from './platform';

test('preserves the entire original credential', () => {
    const raw = 'user=%7B%22name%22%3A%22A+B%22%7D&hash=abc&auth_date=1';
    expect(miniAppAuthorization({ WebApp: { initData: raw } })).toBe(`max ${raw}`);
    expect(miniAppAuthorization({ WebApp: { initData: '' } })).toBeUndefined();
    expect(miniAppAuthorization({})).toBeUndefined();
});
