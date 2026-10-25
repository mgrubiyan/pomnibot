import type { NotesJob } from '../types';
import { cardsLabel, plural } from './plural';

/** As much as the server takes at once, all the files together. */
export const MAX_NOTES_BYTES = 20 * 1024 * 1024;
export const MAX_NOTES_FILES = 10;

/** The client checks the limit first; the server says the same with 413. */
export const NOTES_TOO_BIG = 'Файлы больше 20 МБ вместе. Выберите меньше страниц или разделите конспект на части.';

/** The job is still on its way to done or failed. */
export function isNotesRunning(job: NotesJob | null | undefined): boolean {
    return job?.state === 'queued' || job?.state === 'reading' || job?.state === 'generating';
}

/** Why the picked files cannot go, or '' when they can. */
export function notesFilesProblem(files: readonly { size: number }[]): string {
    if (files.length === 0) {
        return '';
    }
    if (files.length > MAX_NOTES_FILES) {
        return `Можно выбрать до ${MAX_NOTES_FILES} файлов за раз.`;
    }
    if (files.some((file) => file.size === 0)) {
        return 'Один из файлов пустой. Выберите другой.';
    }
    if (files.reduce((sum, file) => sum + file.size, 0) > MAX_NOTES_BYTES) {
        return NOTES_TOO_BIG;
    }
    return '';
}

/** The title and line of the progress screen. */
export function notesProgress(job: NotesJob): { title: string; text: string } {
    switch (job.state) {
        case 'queued': {
            const ahead = job.ahead ?? 0;
            return {
                title: 'Конспект в очереди',
                text: ahead > 0
                    ? `Перед вами ${ahead} ${plural(ahead, ['конспект', 'конспекта', 'конспектов'])}. Начнём, как только освободимся`
                    : 'Начнём, как только освободимся',
            };
        }
        case 'generating':
            return {
                title: 'Делаем карточки',
                text: job.cards ? `Уже готово: ${cardsLabel(job.cards)}` : 'Разбираем темы конспекта',
            };
        default:
            return { title: 'Читаем конспект', text: 'Распознаём текст' };
    }
}

/** What to tell the student when the notes did not become a set. */
export function notesFailureText(job: NotesJob): string {
    switch (job.reason) {
        case 'unreadable':
            return job.message || 'Не получилось открыть файл. Выберите другой.';
        case 'no_text':
            return 'Не нашли в файле текста. Если это фото, снимите страницу целиком, при хорошем свете и без размытия.';
        case 'no_cards':
            return 'По этому тексту не получилось составить карточки. Нужен конспект подробнее: с определениями, свойствами, причинами и следствиями.';
        case 'other_text':
            return 'По этому тексту не получилось составить карточки. Попробуйте другой конспект.';
        case 'too_long':
            return 'Не получилось разобрать конспект целиком. Загрузите отдельную главу или фрагмент поменьше.';
        case 'too_much':
            return 'Конспект слишком большой для одного набора. Загрузите фрагмент поменьше.';
        default:
            return job.message || 'Что-то пошло не так на нашей стороне. Попробуйте ещё раз через пару минут.';
    }
}

/** A running job the server no longer knows: it restarted. */
export const NOTES_LOST: NotesJob = {
    state: 'failed',
    message: 'Связь с обработкой потерялась. Если набор не появится на главной, загрузите конспект ещё раз.',
};
