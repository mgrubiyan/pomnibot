declare global {
    interface Window {
        WebApp?: {
            ready?: () => void;
            close?: () => void;
            initData?: string;
            [key: string]: unknown;
        };
    }
}

/** The MAX launch data, passed on whole; only the server verifies identity. */
export function miniAppAuthorization(sdk: Pick<Window, 'WebApp'>): string | undefined {
    const max = sdk.WebApp?.initData;
    if (max) return `max ${max}`;
    return undefined;
}

/** Closes the mini app, when the platform lets it; undefined in a browser. */
export function closeMiniApp(): (() => void) | undefined {
    const max = window.WebApp;
    if (max?.initData && max.close) return () => max.close?.();
    return undefined;
}
