import { describe, expect, test } from 'bun:test';
import { ANONYMOUS_MEMBER, avatarLetter, formatAuthorName } from './user';

describe('formatAuthorName', () => {
    test('names a member who agreed', () => {
        expect(formatAuthorName({ id: '1', firstName: 'Анна', lastName: 'Смирнова' })).toBe('Анна Смирнова');
    });

    test('hides a member who did not agree', () => {
        expect(formatAuthorName({ id: '1', firstName: '', anonymous: true })).toBe(ANONYMOUS_MEMBER);
    });
});

describe('avatarLetter', () => {
    test('takes the first letter of the name', () => {
        expect(avatarLetter({ id: '1', firstName: 'анна' })).toBe('А');
        expect(avatarLetter({ id: '1', firstName: '', username: 'bob' })).toBe('B');
    });

    test('keeps an emoji whole', () => {
        expect(avatarLetter({ id: '1', firstName: '😀 Ann' })).toBe('😀');
    });

    test('has no letter for an anonymous member', () => {
        expect(avatarLetter({ id: '1', firstName: '', anonymous: true })).toBe('');
    });
});
