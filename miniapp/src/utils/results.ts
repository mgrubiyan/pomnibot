import type { Answer, Card } from '../types';

/** How a fact went in a run: errors out of the cards answered on it. */
export interface FactScore {
    factId: string;
    factName: string;
    wrong: number;
    total: number;
}

/** Facts of a finished run, the weakest first. */
export function scoreByFact(cards: Card[], results: Answer[]): FactScore[] {
    const factOf = new Map(cards.map((card) => [card.id, { factId: card.factId, factName: card.factName }]));
    const scores = new Map<string, FactScore>();

    for (const result of results) {
        const fact = factOf.get(result.cardId);
        if (!fact || !fact.factId) {
            continue;
        }
        const score = scores.get(fact.factId) ?? { factId: fact.factId, factName: fact.factName, wrong: 0, total: 0 };
        score.total += 1;
        if (!result.correct) {
            score.wrong += 1;
        }
        scores.set(fact.factId, score);
    }

    // The share of errors sets the order; on a tie more errors go first,
    // so «2 ошибки из 8» stays above «1 ошибка из 4».
    return [...scores.values()].sort(
        (a, b) => b.wrong / b.total - a.wrong / a.total || b.wrong - a.wrong,
    );
}

/** Facts of a finished run that had an error, the weakest first: a fact
 *  answered without a mistake has nothing to show. */
export function factsWithErrors(cards: Card[], results: Answer[]): FactScore[] {
    return scoreByFact(cards, results).filter((fact) => fact.wrong > 0);
}

/** A run without a single error. Judged by the answers, not the facts: a
    card without a fact is left out of them, and its error would read as none. */
export function allCorrect(results: Answer[]): boolean {
    return results.length > 0 && results.every((result) => result.correct);
}

// Backwards compatibility aliases
export { factsWithErrors as topicsWithErrors, scoreByFact as scoreByTopic };
export type { FactScore as TopicScore };
