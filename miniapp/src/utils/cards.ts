import type { CardAnswer, CardKind, TableLayout, CardImage } from '../types';

/** What the user changed in a card. */
export interface CardPatch {
    questionImages?: CardImage[];
    explanationImages?: CardImage[];
    questionImageIds?: string[];
    explanationImageIds?: string[];
    kind?: CardKind;
    question?: string;
    answer?: CardAnswer;
    options?: string[];
    table?: TableLayout;
    explanation?: string;
    sourceQuote?: string;
    sourceRef?: string;
}
