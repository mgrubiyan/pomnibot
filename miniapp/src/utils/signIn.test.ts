import { describe, expect, it } from 'bun:test';
import { noteResponseStatus, onSignInExpired, signInExpired } from './signIn';

describe('signIn', () => {
    it('raises the flag on the first 401 only and tells the subscribers once', () => {
        let told = 0;
        const stop = onSignInExpired(() => {
            told += 1;
        });
        noteResponseStatus(200);
        noteResponseStatus(404);
        expect(signInExpired()).toBe(false);
        noteResponseStatus(401);
        noteResponseStatus(401);
        expect(signInExpired()).toBe(true);
        expect(told).toBe(1);
        stop();
    });
});
