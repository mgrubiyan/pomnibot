import type { CardAnswer, CardKind, TableLayout } from '../types';

/** What the user changed in a card. */
export interface CardPatch {
    kind?: CardKind;
    question?: string;
    answer?: CardAnswer;
    options?: string[];
    table?: TableLayout;
    explanation?: string;
    sourceQuote?: string;
    sourceRef?: string;
}
