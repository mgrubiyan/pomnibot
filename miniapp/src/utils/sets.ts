import type { Card, CardSet, Fact, SetIssue } from '../types';
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

/** Every fact of a set. */
export async function loadFacts(setId: string): Promise<Fact[]> {
    const { data, error } = await api.GET('/sets/{setId}/facts', {
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

export async function updateSetTitle(setId: string, title: string): Promise<void> {
    const { error } = await api.PATCH('/sets/{setId}', {
        params: { path: { setId } },
        body: { title: title.trim() },
    });
    if (error) {
        throw new Error('Failed to update set title');
    }
}

export async function createFact(setId: string, name: string) {
    const { data, error } = await api.POST('/sets/{setId}/facts', {
        params: { path: { setId } },
        body: { name: name.trim() },
    });
    if (error || !data) {
        throw new Error('Failed to create fact');
    }
    return data;
}

export async function updateFact(factId: string, name: string): Promise<void> {
    const { error } = await api.PATCH('/facts/{factId}', {
        params: { path: { factId } },
        body: { name: name.trim() },
    });
    if (error) {
        throw new Error('Failed to update fact');
    }
}

export async function deleteFact(factId: string): Promise<void> {
    const { error } = await api.DELETE('/facts/{factId}', {
        params: { path: { factId } },
    });
    if (error) {
        throw new Error('Failed to delete fact');
    }
}

export async function reorderCards(factId: string, cardIds: string[]): Promise<void> {
    const { error } = await api.PUT('/facts/{factId}/cards/order', {
        params: { path: { factId } },
        body: { cardIds },
    });
    if (error) {
        throw new Error('Failed to reorder cards');
    }
}
