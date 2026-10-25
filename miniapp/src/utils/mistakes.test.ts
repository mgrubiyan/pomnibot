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
    it('agrees the words with any number', () => {
        const cases: [number, number, string][] = [
            [1, 1, '1 ошибка в 1 ответе'],
            [2, 2, '2 ошибки в 2 ответах'],
            [3, 4, '3 ошибки в 4 ответах'],
            [5, 5, '5 ошибок в 5 ответах'],
            [11, 11, '11 ошибок в 11 ответах'],
            [12, 21, '12 ошибок в 21 ответе'],
            [13, 22, '13 ошибок в 22 ответах'],
            [14, 25, '14 ошибок в 25 ответах'],
            [21, 101, '21 ошибка в 101 ответе'],
            [22, 111, '22 ошибки в 111 ответах'],
            [111, 112, '111 ошибок в 112 ответах'],
        ];
        for (const [mistakes, answers, text] of cases) {
            expect(mistakesLabel(mistakes, answers)).toBe(text);
        }
    });

    it('says there were no mistakes instead of counting zero', () => {
        expect(mistakesLabel(0, 1)).toBe('Без ошибок · 1 ответ');
        expect(mistakesLabel(0, 3)).toBe('Без ошибок · 3 ответа');
        expect(mistakesLabel(0, 11)).toBe('Без ошибок · 11 ответов');
        expect(mistakesLabel(0, 21)).toBe('Без ошибок · 21 ответ');
        expect(mistakesLabel(0, 0)).toBe('Пока без ответов');
    });
});

describe('factLabel', () => {
    const counts = (answered: number, mistaken: number, answers: number, mistakes: number) =>
        ({ ...data.facts[0], studentsAnswered: answered, studentsMistaken: mistaken, answers, mistakes });

    it('says how many of those who answered got the fact wrong', () => {
        expect(factLabel(data.facts[0])).toBe('Ошиблись 2 из 2 · 4 ошибки в 8 ответах');
        expect(factLabel(counts(13, 1, 21, 1))).toBe('Ошиблись 1 из 13 · 1 ошибка в 21 ответе');
    });

    it('drops the zeros when nobody erred', () => {
        expect(factLabel(counts(1, 0, 1, 0))).toBe('Без ошибок · 1 ответ');
        expect(factLabel(counts(2, 0, 4, 0))).toBe('Все без ошибок · 4 ответа');
        expect(factLabel(counts(3, 0, 5, 0))).toBe('Все 3 без ошибок · 5 ответов');
        expect(factLabel(counts(21, 0, 42, 0))).toBe('Все 21 без ошибок · 42 ответа');
    });

    it('waits for the first answer', () => {
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
