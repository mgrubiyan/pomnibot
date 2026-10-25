import type { User } from '../types';

/** How a member who has not let the author see them is called. */
export const ANONYMOUS_MEMBER = 'Участник без имени';

export function formatAuthorName(user: User): string {
    if (user.anonymous) {
        return ANONYMOUS_MEMBER;
    }
    const fullName = [user.firstName, user.lastName].filter(Boolean).join(' ').trim();
    return fullName || user.username || `User ${user.id}`;
}

export function formatGreetingName(user: User): string {
    return user.firstName?.trim() || user.username?.trim() || 'друг';
}

/** The letter on a user's avatar; none for an anonymous member. */
export function avatarLetter(user: User): string {
    if (user.anonymous) {
        return '';
    }
    const name = formatAuthorName(user);
    return Array.from(name.trim())[0]?.toUpperCase() ?? '';
}
