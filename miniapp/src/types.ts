export type CardKind = 'choice' | 'flip' | 'input' | 'boolean' | 'table';

/** Термин таблицы и колонка, в которой ему место. */
export interface TableItem {
    text: string;
    column: string;
}

/** Карточка-сортировка: разложить термины по колонкам. */
export interface TableLayout {
    columns: string[];
    items: TableItem[];
}

export interface Card {
    id: string;
    setId: string;
    kind: CardKind;
    question: string;
    options?: string[];      // для choice
    table?: TableLayout;     // для table
    answer: string;          // правильный ответ; для boolean — 'true' | 'false',
                             // для table — раскладка словами, для чат-режима
    explanation: string;     // 2-3 предложения
    sourceQuote: string;     // цитата из конспекта
    sourceRef?: string;      // откуда цитата: «Лекция 3, стр. 2»
    topic: string;           // тема для статистики
}

export interface CardSet {
    id: string;
    title: string;           // «Матанализ, лекция 3»
    cardsTotal: number;
    cardsDue: number;        // сколько на повтор
    authorName?: string;     // если набор получен по коду
    shareCode?: string;
}

export interface TodayData {
    userName: string;
    activeDays: number;      // «4 дня из 7»
    dueCount: number;
    estimatedMinutes: number;
    sets: CardSet[];
}

export interface AnswerResult {
    cardId: string;
    correct: boolean;
    answeredAt: string;
}

/** Почему карточка плохая — спрашиваем перед правкой или удалением. */
export type CardIssueReason = 'answer' | 'wording' | 'not-in-notes' | 'other';
