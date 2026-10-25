import type { FactMistakes, SetMistakes, User } from '../types';
import { errorsLabel, plural } from './plural';

/** «из 5 ответов» needs the genitive: «из 1 ответа», «из 21 ответа». */
const ofAnswersLabel = (count: number) =>
    `из ${count} ${plural(count, ['ответа', 'ответов', 'ответов'])}`;

/** «2 ошибки из 5 ответов», or that nothing was answered yet. */
export function mistakesLabel(mistakes: number, answers: number): string {
    if (answers === 0) {
        return 'Пока без ответов';
    }
    return `${errorsLabel(mistakes)} ${ofAnswersLabel(answers)}`;
}

/** How many of those who answered a fact got it wrong at least once. */
export function factLabel(fact: FactMistakes): string {
    if (fact.studentsAnswered === 0) {
        return 'Пока без ответов';
    }
    return `Ошибались ${fact.studentsMistaken} из ${fact.studentsAnswered} · ${mistakesLabel(fact.mistakes, fact.answers)}`;
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
