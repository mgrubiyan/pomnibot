import type { User } from '../types';

export function formatAuthorName(user: User): string {
    const fullName = [user.firstName, user.lastName].filter(Boolean).join(' ').trim();
    return fullName || user.username || `User ${user.id}`;
}

export function formatGreetingName(user: User): string {
    return user.firstName?.trim() || user.username?.trim() || 'друг';
}
