import type { components } from '../contracts/schema';

export type CardKind = components['schemas']['CardKind'];
export type TableItem = components['schemas']['TableItem'];
export type TableLayout = components['schemas']['TableLayout'];
export type CardAnswer = components['schemas']['CardAnswer'];
export type Card = components['schemas']['Card'];
export type FeedCard = components['schemas']['FeedCard'];
export type FeedTable = components['schemas']['FeedTable'];
export type CardReveal = components['schemas']['CardReveal'];
export type AnswerResult = components['schemas']['AnswerQuestionResponse'];
export type CardSet = components['schemas']['CardSet'];
export type TodayData = components['schemas']['TodayData'];
/** How the reader did on a card of a run; the backend has recorded it. */
export interface Answer {
    cardId: string;
    correct: boolean;
}
export type User = components['schemas']['User'];
export type LeaderboardEntry = components['schemas']['LeaderboardEntry'];
export type SetLeaderboardResponse = components['schemas']['SetLeaderboardResponse'];
export type CardIssueReason = components['schemas']['CardIssueReason'];
export type SetIssue = components['schemas']['SetIssue'];
export type CardIssueStatus = components['schemas']['CardIssueStatus'];
export type AdminCardIssue = components['schemas']['AdminCardIssue'];
export type Fact = components['schemas']['Fact'];
