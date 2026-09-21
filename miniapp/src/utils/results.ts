import type { AnswerResult, Card } from '../types';

/** How a topic went in a run: errors out of the cards answered on it. */
export interface TopicScore {
    topic: string;
    wrong: number;
    total: number;
}

/** Topics of a finished run, the weakest first. */
export function scoreByTopic(cards: Card[], results: AnswerResult[]): TopicScore[] {
    const topicOf = new Map(cards.map((card) => [card.id, card.topic]));
    const scores = new Map<string, TopicScore>();

    for (const result of results) {
        const topic = topicOf.get(result.cardId);
        if (!topic) {
            continue;
        }
        const score = scores.get(topic) ?? { topic, wrong: 0, total: 0 };
        score.total += 1;
        if (!result.correct) {
            score.wrong += 1;
        }
        scores.set(topic, score);
    }

    // The share of errors sets the order; on a tie more errors go first,
    // so «2 ошибки из 8» stays above «1 ошибка из 4».
    return [...scores.values()].sort(
        (a, b) => b.wrong / b.total - a.wrong / a.total || b.wrong - a.wrong,
    );
}
