import { describe, expect, it } from 'bun:test';
import { MAX_NOTES_BYTES, isNotesRunning, notesFailureText, notesFilesProblem, notesProgress } from './notes';

describe('notesFilesProblem', () => {
    it('lets through up to ten files of 20 MB in all', () => {
        expect(notesFilesProblem([])).toBe('');
        expect(notesFilesProblem([{ size: MAX_NOTES_BYTES / 2 }, { size: MAX_NOTES_BYTES / 2 }])).toBe('');
        expect(notesFilesProblem(Array.from({ length: 10 }, () => ({ size: 1 })))).toBe('');
    });

    it('stops what the server would refuse', () => {
        expect(notesFilesProblem([{ size: MAX_NOTES_BYTES / 2 }, { size: MAX_NOTES_BYTES / 2 + 1 }])).toContain('20 МБ');
        expect(notesFilesProblem(Array.from({ length: 11 }, () => ({ size: 1 })))).toContain('до 10 файлов');
        expect(notesFilesProblem([{ size: 5 }, { size: 0 }])).toContain('пустой');
    });
});

describe('notes job', () => {
    it('runs until done or failed', () => {
        expect(isNotesRunning({ state: 'queued' })).toBe(true);
        expect(isNotesRunning({ state: 'reading' })).toBe(true);
        expect(isNotesRunning({ state: 'generating' })).toBe(true);
        expect(isNotesRunning({ state: 'done' })).toBe(false);
        expect(isNotesRunning({ state: 'failed' })).toBe(false);
        expect(isNotesRunning(null)).toBe(false);
    });

    it('tells how far it is', () => {
        expect(notesProgress({ state: 'queued', ahead: 2 }).text).toBe('Перед вами 2 конспекта. Начнём, как только освободимся');
        expect(notesProgress({ state: 'queued', ahead: 5 }).text).toContain('5 конспектов');
        expect(notesProgress({ state: 'reading' }).title).toBe('Читаем конспект');
        expect(notesProgress({ state: 'generating', cards: 0 }).text).toBe('Разбираем темы конспекта');
        expect(notesProgress({ state: 'generating', cards: 21 }).text).toBe('Уже готово: 21 карточка');
    });

    it('passes on what is wrong with the files, and words the rest itself', () => {
        expect(notesFailureText({ state: 'failed', reason: 'unreadable', message: 'Файл «a.docx»: не открылся.' })).toBe('Файл «a.docx»: не открылся.');
        expect(notesFailureText({ state: 'failed', reason: 'unreadable' })).toContain('Выберите другой');
        const reasons = ['no_text', 'no_cards', 'other_text', 'too_long', 'too_much', 'try_later'] as const;
        const texts = reasons.map((reason) => notesFailureText({ state: 'failed', reason }));
        expect(new Set(texts).size).toBe(reasons.length);
        expect(notesFailureText({ state: 'failed', reason: 'try_later' })).toBe(notesFailureText({ state: 'failed' }));
    });
});
