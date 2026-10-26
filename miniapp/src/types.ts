import type { components } from '../contracts/schema';

export type CardKind = components['schemas']['CardKind'];
export type TableItem = components['schemas']['TableItem'];
export type TableLayout = components['schemas']['TableLayout'];
export type CardAnswer = components['schemas']['CardAnswer'];
export type CardImage = components['schemas']['CardImage'];
export type Card = Omit<components['schemas']['Card'], 'questionImages' | 'explanationImages'> & { questionImages?: CardImage[]; explanationImages?: CardImage[] };
export type FeedCard = Omit<components['schemas']['FeedCard'], 'questionImages'> & { questionImages?: CardImage[] };
export type FeedTable = components['schemas']['FeedTable'];
export type CardReveal = Omit<components['schemas']['CardReveal'], 'explanationImages'> & { explanationImages?: CardImage[] };
export type AnswerResult = components['schemas']['AnswerQuestionResponse'];
export type CardSet = components['schemas']['CardSet'];
export type TodayData = components['schemas']['TodayData'];
export type NotesJob = components['schemas']['NotesJob'];
/** How the reader did on a card of a run; the backend has recorded it. */
export interface Answer {
    cardId: string;
    correct: boolean;
}
export type User = components['schemas']['User'];
export type Profile = components['schemas']['ProfileResponse'];
export type LeaderboardEntry = components['schemas']['LeaderboardEntry'];
export type SetLeaderboardResponse = components['schemas']['SetLeaderboardResponse'];
export type SetMistakes = components['schemas']['SetMistakesResponse'];
export type FactMistakes = components['schemas']['FactMistakes'];
export type CardIssueReason = components['schemas']['CardIssueReason'];
export type SetIssue = components['schemas']['SetIssue'];
export type CardIssueStatus = components['schemas']['CardIssueStatus'];
export type AdminCardIssue = components['schemas']['AdminCardIssue'];
export type Fact = components['schemas']['Fact'];

export type SetRetention = components['schemas']['SetRetention'];
