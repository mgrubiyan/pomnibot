import { useCallback, useEffect, useRef, useState } from 'react';
import { api } from '../api';
import type { NotesJob } from '../types';
import { NOTES_LOST, NOTES_TOO_BIG, isNotesRunning } from './notes';

/** How often to ask how the notes go; the bot edits its message as often. */
const POLL_MS = 2000;

/**
 * ready: the mini app takes notes. off: here they go to the bot, as on a
 * server without models or outside MAX.
 */
export type NotesAccess = 'loading' | 'ready' | 'off';

export type NotesUpload = { ok: true } | { ok: false; error: string };

/**
 * The user's latest notes on the server, followed while they are made.
 * The job outlives the screen: it is the server's, kept for an hour.
 * `followed` is whether this screen saw it running or sent it, or, with
 * followFailed, found it failed; onFinish hears when a job seen running ends.
 */
export function useNotesJob(onFinish?: (job: NotesJob) => void, { followFailed = false } = {}) {
    const [access, setAccess] = useState<NotesAccess>('loading');
    const [job, setJob] = useState<NotesJob | null>(null);
    const [followed, setFollowed] = useState(false);
    const running = isNotesRunning(job);
    const finish = useRef(onFinish);
    useEffect(() => {
        finish.current = onFinish;
    });

    useEffect(() => {
        let cancelled = false;
        api.GET('/notes')
            .then(({ data, response }) => {
                if (cancelled) {
                    return;
                }
                if (response.status === 503 || response.status === 403) {
                    setAccess('off');
                    return;
                }
                setAccess('ready');
                setJob(response.ok && data ? data : null);
                setFollowed(isNotesRunning(data) || (followFailed && data?.state === 'failed'));
            })
            .catch(() => {
                // The upload will tell about the connection.
                if (!cancelled) {
                    setAccess('ready');
                }
            });
        return () => {
            cancelled = true;
        };
        // The first answer decides; the option does not change on a screen.
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);

    useEffect(() => {
        if (!running) {
            return;
        }
        let cancelled = false;
        const timer = window.setTimeout(() => {
            api.GET('/notes')
                .then(({ data, response }) => {
                    if (cancelled) {
                        return;
                    }
                    const next = response.status === 404 ? NOTES_LOST : response.ok ? data : undefined;
                    if (next) {
                        setJob(next);
                        if (!isNotesRunning(next)) {
                            finish.current?.(next);
                        }
                    } else {
                        // Asked again on the next round.
                        setJob((current) => (current ? { ...current } : current));
                    }
                })
                .catch(() => {
                    if (!cancelled) {
                        setJob((current) => (current ? { ...current } : current));
                    }
                });
        }, POLL_MS);
        return () => {
            cancelled = true;
            window.clearTimeout(timer);
        };
    }, [running, job]);

    /** An aborted upload ends with no error to show. */
    const upload = useCallback(async (files: File[], signal?: AbortSignal): Promise<NotesUpload> => {
        try {
            const { data, response } = await api.POST('/notes', {
                signal,
                // The schema types binary parts as strings.
                body: { files: files as unknown as string[] },
                bodySerializer: (body) => {
                    const form = new FormData();
                    for (const file of body.files as unknown as File[]) {
                        form.append('files', file, file.name);
                    }
                    return form;
                },
            });
            if (response.status === 409) {
                // The previous notes are still being made: follow them.
                const current = await api.GET('/notes');
                setJob(current.data ?? null);
                setFollowed(true);
                // They may have ended in between: no poll would tell then.
                if (current.data && !isNotesRunning(current.data)) {
                    finish.current?.(current.data);
                }
                return { ok: true };
            }
            if (response.status === 413) {
                return { ok: false, error: NOTES_TOO_BIG };
            }
            if (response.status === 403 || response.status === 503) {
                setAccess('off');
                return { ok: false, error: '' };
            }
            if (!response.ok || !data) {
                throw new Error(`notes upload failed: ${response.status}`);
            }
            setJob(data);
            setFollowed(true);
            return { ok: true };
        } catch {
            if (signal?.aborted) {
                return { ok: false, error: '' };
            }
            return { ok: false, error: 'Не получилось отправить конспект. Проверьте соединение и попробуйте ещё раз.' };
        }
    }, []);

    return { access, job, followed, upload };
}
