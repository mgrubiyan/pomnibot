/**
 * The backend refuses a sign-in that does not hold or has expired (401).
 * The mini app cannot renew it: the platform signs it once, when the app
 * opens, so the reader has to reopen the app. The flag stays raised.
 */
let expired = false;
const listeners = new Set<() => void>();

/** Notes a response; a 401 means the sign-in expired. */
export function noteResponseStatus(status: number): void {
    if (status !== 401 || expired) {
        return;
    }
    expired = true;
    for (const listener of listeners) {
        listener();
    }
}

export function signInExpired(): boolean {
    return expired;
}

/** Subscribes to the sign-in expiring; returns the unsubscribe. */
export function onSignInExpired(listener: () => void): () => void {
    listeners.add(listener);
    return () => {
        listeners.delete(listener);
    };
}
