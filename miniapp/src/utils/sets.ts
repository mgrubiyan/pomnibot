import type { Card, CardSet, SetIssue } from '../types';
import { api } from '../api';

/** A set with its share code, the reader's progress and place in it. */
export async function loadSet(setId: string): Promise<CardSet> {
    const { data, error } = await api.GET('/sets/{setId}', {
        params: { path: { setId } },
    });
    if (error || !data) {
        throw new Error(error?.message ?? 'not found');
    }
    return data;
}

/** Every card of a set: the set screen counts them, the cards screen lists them. */
export async function loadCards(setId: string): Promise<Card[]> {
    const { data, error } = await api.GET('/sets/{setId}/cards', {
        params: { path: { setId } },
    });
    if (error || !data) {
        throw new Error(error?.message ?? 'not found');
    }
    return data;
}

/** Open complaints of the author's set; for anyone else the request fails. */
export async function loadSetIssues(setId: string): Promise<SetIssue[]> {
    const { data, error } = await api.GET('/sets/{setId}/issues', {
        params: { path: { setId } },
    });
    if (error || !data) {
        throw new Error(error?.message ?? 'not found');
    }
    return data;
}
