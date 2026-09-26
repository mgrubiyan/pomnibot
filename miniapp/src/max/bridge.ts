/**
 * MAX Bridge — the `window.WebApp` object that max-web-app.js from
 * index.html puts on the page. Only what the app uses is typed here,
 * the full reference is https://dev.max.ru/docs/webapps/bridge.
 * Outside MAX the script may fail to load, so every call is optional.
 */
interface MaxBackButton {
    show: () => void;
    hide: () => void;
    onClick: (callback: () => void) => void;
    offClick: (callback: () => void) => void;
}

interface MaxWebApp {
    /** Signed launch data as a query string; the backend validates it. */
    initData?: string;
    /** The same data parsed, unsigned: fine for the UI, never for trust. */
    initDataUnsafe?: {
        /** Payload of the link the mini app was opened with. */
        start_param?: string;
    };
    /** Optional on purpose: a partial `window.WebApp` must not break start-up. */
    BackButton?: MaxBackButton;
    ready?: () => void;
}

declare global {
    interface Window {
        WebApp?: MaxWebApp;
    }
}

export const webApp = (): MaxWebApp | undefined => window.WebApp;

/** Tells MAX the app is ready to be shown. */
export const ready = () => webApp()?.ready?.();

/** Empty outside MAX — then there is nothing to send. */
export const initData = () => webApp()?.initData ?? '';

export const startParam = () => webApp()?.initDataUnsafe?.start_param ?? '';
