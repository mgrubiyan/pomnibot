import type { FactMistakes, SetMistakes, User } from '../types';
import { errorsLabel, plural } from './plural';

/** «4 ответа»: how many answers a row stands on. */
const answersLabel = (count: number) =>
    `${count} ${plural(count, ['ответ', 'ответа', 'ответов'])}`;

/** «в 5 ответах» needs the prepositional: «в 1 ответе», «в 21 ответе». */
const inAnswersLabel = (count: number) =>
    `в ${count} ${plural(count, ['ответе', 'ответах', 'ответах'])}`;

/** «2 ошибки в 5 ответах», «Без ошибок · 4 ответа», or that nothing was
    answered yet. */
export function mistakesLabel(mistakes: number, answers: number): string {
    if (answers === 0) {
        return 'Пока без ответов';
    }
    if (mistakes === 0) {
        return `Без ошибок · ${answersLabel(answers)}`;
    }
    return `${errorsLabel(mistakes)} ${inAnswersLabel(answers)}`;
}

/** How many of those who answered a fact got it wrong at least once.
    «Все 2 без ошибок» does not read, so two get no number. */
export function factLabel(fact: FactMistakes): string {
    const { studentsAnswered: answered, studentsMistaken: mistaken } = fact;
    if (answered === 0) {
        return 'Пока без ответов';
    }
    if (mistaken > 0) {
        return `Ошиблись ${mistaken} из ${answered} · ${mistakesLabel(fact.mistakes, fact.answers)}`;
    }
    const everyone = answered === 1 ? 'Без ошибок' : answered === 2 ? 'Все без ошибок' : `Все ${answered} без ошибок`;
    return `${everyone} · ${answersLabel(fact.answers)}`;
}

interface Counts {
    answers: number;
    mistakes: number;
}

const byMistakes = (a: Counts, b: Counts) => b.mistakes - a.mistakes || b.answers - a.answers;

/** One member's facts, the most mistakes first; the facts the member has
    not answered are only counted. */
export function studentFacts(data: SetMistakes, userId: string) {
    const answered: (Counts & { fact: FactMistakes })[] = [];
    let unanswered = 0;
    for (const fact of data.facts) {
        const counts = fact.students.find((entry) => entry.userId === userId);
        if (counts) {
            answered.push({ fact, answers: counts.answers, mistakes: counts.mistakes });
        } else {
            unanswered++;
        }
    }
    answered.sort(byMistakes);
    return { answered, unanswered };
}

/** The members who answered a fact, the most mistakes first; the rest are
    only counted. */
export function factStudents(data: SetMistakes, fact: FactMistakes) {
    const names = new Map(data.students.map((student) => [student.user.id, student.user]));
    const answered: (Counts & { user: User })[] = [];
    for (const entry of fact.students) {
        const user = names.get(entry.userId);
        if (user) {
            answered.push({ user, answers: entry.answers, mistakes: entry.mistakes });
        }
    }
    answered.sort(byMistakes);
    return { answered, unanswered: data.students.length - answered.length };
}
