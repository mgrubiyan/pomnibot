import type { Answer, Card, CardAnswer, CardReveal, FeedCard, FeedTable, TableLayout } from '../types';
import type { CardPatch } from './cards';

/**
 * A run left for the complaint screen. Answers are recorded as they are
 * given, so a fresh feed would no longer hold the answered cards: the run
 * comes back with its own cards, at the card after the answered one.
 */
export interface FeedResume {
    cards: FeedCard[];
    index: number;
    results: Answer[];
}

/** A feed card with the author's edit applied: only what the feed asks. */
export function patchFeedCard(card: FeedCard, patch?: CardPatch): FeedCard {
    if (!patch) {
        return card;
    }
    return {
        ...card,
        kind: patch.kind ?? card.kind,
        question: patch.question ?? card.question,
        options: patch.options ?? card.options,
        table: patch.table
            ? { columns: patch.table.columns, items: patch.table.items.map((item) => item.text) }
            : card.table,
    };
}

/** Cards of the feed with this session's deletions and edits applied. */
export function visibleFeed(cards: FeedCard[], removedIds: string[], patches: Record<string, CardPatch>): FeedCard[] {
    return cards
        .filter((card) => !removedIds.includes(card.id))
        .map((card) => patchFeedCard(card, patches[card.id]));
}

/** The cards already gone through stay as they were, for the result screen. */
export function resumedCards(resume: FeedResume, removedIds: string[], patches: Record<string, CardPatch>): FeedCard[] {
    return [
        ...resume.cards.slice(0, resume.index),
        ...visibleFeed(resume.cards.slice(resume.index), removedIds, patches),
    ];
}

/**
 * The terms of a sorting card as the table components take them: without
 * their columns until the answer, in the columns the backend revealed after.
 */
export function layoutOf(table: FeedTable, revealed?: CardAnswer): TableLayout {
    const columnOf = new Map(
        typeof revealed === 'object' && !Array.isArray(revealed)
            ? revealed.items.map((item) => [item.text, item.column])
            : [],
    );
    return {
        columns: table.columns,
        items: table.items.map((text) => ({ text, column: columnOf.get(text) ?? '' })),
    };
}

/** The whole card once the feed has its answer, for the complaint and the edit. */
export function revealedCard(card: FeedCard, reveal: CardReveal): Card {
    const table = card.table && typeof reveal.answer === 'object' && !Array.isArray(reveal.answer)
        ? reveal.answer
        : undefined;
    return { ...card, ...reveal, table };
}
