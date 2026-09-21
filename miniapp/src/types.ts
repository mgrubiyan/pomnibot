export type CardKind = 'choice' | 'flip' | 'input' | 'boolean' | 'table';

/** A term of the table and the column it belongs to. */
export interface TableItem {
    text: string;
    column: string;
}

/** Sorting card: lay the terms out across the columns. */
export interface TableLayout {
    columns: string[];
    items: TableItem[];
}

export interface Card {
    id: string;
    setId: string;
    kind: CardKind;
    question: string;
    options?: string[];      // choice only
    table?: TableLayout;     // table only
    answer: string;          // correct answer; 'true' | 'false' for boolean,
                             // for table — the layout spelled out, for chat mode
    explanation: string;     // 2-3 sentences
    sourceQuote: string;     // quote from the notes
    sourceRef?: string;      // where the quote is from: «Лекция 3, стр. 2»
    topic: string;           // topic, used for stats
}

export interface CardSet {
    id: string;
    title: string;           // «Матанализ, лекция 3»
    cardsTotal: number;
    cardsDue: number;        // how many are due for review
    authorName?: string;     // set when the set came in by share code
    shareCode?: string;
}

export interface TodayData {
    userName: string;
    activeDays: number;      // as in «4 дня из 7»
    dueCount: number;
    estimatedMinutes: number;
    sets: CardSet[];
}

export interface AnswerResult {
    cardId: string;
    correct: boolean;
    answeredAt: string;
}

/** What is wrong with a card — asked before editing or deleting it. */
export type CardIssueReason = 'answer' | 'wording' | 'not-in-notes' | 'other';
