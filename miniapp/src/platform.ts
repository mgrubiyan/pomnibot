export interface TelegramApp {
    initData: string;
    colorScheme?: 'light' | 'dark';
    safeAreaInset?: Partial<Record<'top' | 'right' | 'bottom' | 'left', number>>;
    contentSafeAreaInset?: Partial<Record<'top' | 'right' | 'bottom' | 'left', number>>;
    ready: () => void;
    expand: () => void;
    close?: () => void;
    onEvent: (event: string, callback: () => void) => void;
    offEvent: (event: string, callback: () => void) => void;
}

declare global {
    interface Window {
        Telegram?: { WebApp?: TelegramApp };
        WebApp?: {
            ready?: () => void;
            close?: () => void;
            initData?: string;
            [key: string]: unknown;
        };
    }
}

/** SDK detection selects a credential scheme; only the server verifies identity. */
export function miniAppAuthorization(sdk: Pick<Window, 'Telegram' | 'WebApp'>): string | undefined {
    const telegram = sdk.Telegram?.WebApp?.initData;
    if (telegram) return `tma ${telegram}`;
    const max = sdk.WebApp?.initData;
    if (max) return `max ${max}`;
    return undefined;
}

export function telegramApp(): TelegramApp | undefined {
    const app = window.Telegram?.WebApp;
    return app?.initData ? app : undefined;
}

/** Closes the mini app, when the platform lets it; undefined in a browser. */
export function closeMiniApp(): (() => void) | undefined {
    const telegram = telegramApp();
    if (telegram?.close) return () => telegram.close?.();
    const max = window.WebApp;
    if (max?.initData && max.close) return () => max.close?.();
    return undefined;
}
