import { describe, expect, it } from 'bun:test';
import { classifyError } from './errors';

describe('classifyError', () => {
    it('detects network or offline errors', () => {
        const netErr = new Error('Failed to fetch');
        expect(classifyError(netErr)).toEqual({
            kind: 'offline',
            title: 'Нет соединения',
            text: 'Проверьте интернет и попробуйте ещё раз',
            canRetry: true,
        });

        const loadFailedErr = new Error('Load failed');
        expect(classifyError(loadFailedErr)).toEqual({
            kind: 'offline',
            title: 'Нет соединения',
            text: 'Проверьте интернет и попробуйте ещё раз',
            canRetry: true,
        });
    });

    it('detects 403 forbidden errors', () => {
        const forbiddenErr = new Error('forbidden');
        expect(classifyError(forbiddenErr)).toEqual({
            kind: 'forbidden',
            title: 'Доступ ограничен',
            text: 'Просматривать карточки и факты может только автор набора',
            canRetry: false,
        });

        const status403Err = { status: 403, message: 'access denied' };
        expect(classifyError(status403Err)).toEqual({
            kind: 'forbidden',
            title: 'Доступ ограничен',
            text: 'Просматривать карточки и факты может только автор набора',
            canRetry: false,
        });
    });

    it('detects 404 not found errors', () => {
        const notFoundErr = new Error('not found');
        expect(classifyError(notFoundErr)).toEqual({
            kind: 'not_found',
            title: 'Не найдено',
            text: 'Набор не найден или был удалён',
            canRetry: false,
        });

        const status404Err = { status: 404 };
        expect(classifyError(status404Err)).toEqual({
            kind: 'not_found',
            title: 'Не найдено',
            text: 'Набор не найден или был удалён',
            canRetry: false,
        });
    });

    it('detects server and unknown errors as retryable server errors', () => {
        const serverErr = new Error('internal server error');
        expect(classifyError(serverErr)).toEqual({
            kind: 'server',
            title: 'Не удалось загрузить данные',
            text: 'Произошла ошибка на сервере. Попробуйте позже',
            canRetry: true,
        });

        const status500Err = { status: 500 };
        expect(classifyError(status500Err)).toEqual({
            kind: 'server',
            title: 'Не удалось загрузить данные',
            text: 'Произошла ошибка на сервере. Попробуйте позже',
            canRetry: true,
        });
    });
});
