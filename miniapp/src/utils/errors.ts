export type ErrorKind = 'offline' | 'forbidden' | 'not_found' | 'server';

export interface ErrorClassification {
    kind: ErrorKind;
    title: string;
    text: string;
    canRetry: boolean;
}

export function classifyError(err: unknown): ErrorClassification {
    if (typeof navigator !== 'undefined' && navigator.onLine === false) {
        return {
            kind: 'offline',
            title: 'Нет соединения',
            text: 'Проверьте интернет и попробуйте ещё раз',
            canRetry: true,
        };
    }

    const message = (err instanceof Error ? err.message : String((err as { message?: unknown })?.message ?? ''))
        .toLowerCase();
    const status = typeof err === 'object' && err !== null && 'status' in err
        ? Number((err as { status: unknown }).status)
        : undefined;

    if (
        message.includes('failed to fetch') ||
        message.includes('load failed') ||
        message.includes('network') ||
        message.includes('offline')
    ) {
        return {
            kind: 'offline',
            title: 'Нет соединения',
            text: 'Проверьте интернет и попробуйте ещё раз',
            canRetry: true,
        };
    }

    if (status === 403 || message.includes('403') || message.includes('forbidden')) {
        return {
            kind: 'forbidden',
            title: 'Доступ ограничен',
            text: 'Просматривать карточки и факты может только автор набора',
            canRetry: false,
        };
    }

    if (status === 404 || message.includes('404') || message.includes('not found')) {
        return {
            kind: 'not_found',
            title: 'Не найдено',
            text: 'Набор не найден или был удалён',
            canRetry: false,
        };
    }

    return {
        kind: 'server',
        title: 'Не удалось загрузить данные',
        text: 'Произошла ошибка на сервере. Попробуйте позже',
        canRetry: true,
    };
}
