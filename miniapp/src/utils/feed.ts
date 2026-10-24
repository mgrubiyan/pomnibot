import type { Answer, Card, CardAnswer, CardReveal, FeedCard, FeedTable, TableLayout } from '../types';
import type { CardPatch } from './cards';
import type { Placement } from './table';

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

/** A stopped run reloaded: the cards gone through, then the rest of the feed. */
export function rejoinedFeed(
    passed: FeedCard[],
    feed: FeedCard[],
    removedIds: string[],
    patches: Record<string, CardPatch>,
): FeedCard[] {
    const passedIds = new Set(passed.map((card) => card.id));
    return [...passed, ...visibleFeed(feed.filter((card) => !passedIds.has(card.id)), removedIds, patches)];
}

/**
 * The terms of a sorting card as the table components take them: without
 * their columns until the answer, in the columns the backend revealed after.
 */
export function layoutOf(table: FeedTable, revealed?: CardAnswer, placed?: Placement): TableLayout {
    // A term may repeat. The backend counts a copy right in any column the
    // term belongs to, so a copy keeps the column it was placed in while
    // that column is still free for the term; the rest take what is left.
    const columnsOf = new Map<string, string[]>();
    if (typeof revealed === 'object' && !Array.isArray(revealed)) {
        for (const item of revealed.items) {
            columnsOf.set(item.text, [...(columnsOf.get(item.text) ?? []), item.column]);
        }
    }
    const columns = table.items.map((text, index) => {
        const free = columnsOf.get(text);
        const at = free && placed?.[index] ? free.indexOf(placed[index]) : -1;
        return at >= 0 && free ? free.splice(at, 1)[0] : null;
    });
    return {
        columns: table.columns,
        items: table.items.map((text, index) => ({
            text,
            column: columns[index] ?? columnsOf.get(text)?.shift() ?? '',
        })),
    };
}

/** The whole card once the feed has its answer, for the complaint and the edit. */
export function revealedCard(card: FeedCard, reveal: CardReveal): Card {
    const table = card.table && typeof reveal.answer === 'object' && !Array.isArray(reveal.answer)
        ? reveal.answer
        : undefined;
    return { ...card, ...reveal, table };
}
