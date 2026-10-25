import { describe, expect, it } from 'bun:test';
import type { FactMistakes, SetMistakes } from '../types';
import { factLabel, factStudents, mistakesLabel, studentFacts } from './mistakes';

const fact = (factId: string, students: FactMistakes['students']): FactMistakes => ({
    factId,
    factName: `Факт ${factId}`,
    answers: students.reduce((sum, s) => sum + s.answers, 0),
    mistakes: students.reduce((sum, s) => sum + s.mistakes, 0),
    studentsAnswered: students.length,
    studentsMistaken: students.filter((s) => s.mistakes > 0).length,
    students,
});

const data: SetMistakes = {
    setId: 'set',
    students: [
        { user: { id: 'bob', firstName: 'Боб' }, answers: 9, mistakes: 3 },
        { user: { id: 'carol', firstName: 'Карина' }, answers: 4, mistakes: 3 },
        { user: { id: 'dan', firstName: 'Дан' }, answers: 0, mistakes: 0 },
    ],
    facts: [
        fact('keys', [{ userId: 'bob', answers: 4, mistakes: 1 }, { userId: 'carol', answers: 4, mistakes: 3 }]),
        fact('joins', [{ userId: 'bob', answers: 3, mistakes: 2 }]),
        fact('forms', [{ userId: 'bob', answers: 2, mistakes: 0 }]),
        fact('views', []),
    ],
};

describe('mistakesLabel', () => {
    it('agrees the words with the numbers', () => {
        expect(mistakesLabel(1, 1)).toBe('1 ошибка из 1 ответа');
        expect(mistakesLabel(2, 21)).toBe('2 ошибки из 21 ответа');
        expect(mistakesLabel(5, 12)).toBe('5 ошибок из 12 ответов');
        expect(mistakesLabel(0, 3)).toBe('0 ошибок из 3 ответов');
        expect(mistakesLabel(0, 0)).toBe('Пока без ответов');
    });
});

describe('factLabel', () => {
    it('says how many of those who answered got the fact wrong', () => {
        expect(factLabel(data.facts[0])).toBe('Ошибались 2 из 2 · 4 ошибки из 8 ответов');
        expect(factLabel(data.facts[3])).toBe('Пока без ответов');
    });
});

describe('studentFacts', () => {
    it('lists the facts the member answered, the most mistakes first', () => {
        const { answered, unanswered } = studentFacts(data, 'bob');
        expect(answered.map((entry) => [entry.fact.factId, entry.mistakes, entry.answers])).toEqual([
            ['joins', 2, 3],
            ['keys', 1, 4],
            ['forms', 0, 2],
        ]);
        expect(unanswered).toBe(1);
    });

    it('counts every fact as unanswered for a member who has not started', () => {
        expect(studentFacts(data, 'dan')).toEqual({ answered: [], unanswered: 4 });
    });
});

describe('factStudents', () => {
    it('lists the members who answered the fact, the most mistakes first', () => {
        const { answered, unanswered } = factStudents(data, data.facts[0]);
        expect(answered.map((entry) => [entry.user.firstName, entry.mistakes])).toEqual([
            ['Карина', 3],
            ['Боб', 1],
        ]);
        expect(unanswered).toBe(1);
    });
});
