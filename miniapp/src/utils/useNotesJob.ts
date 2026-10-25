import { useCallback, useEffect, useRef, useState } from 'react';
import { api } from '../api';
import type { NotesJob } from '../types';
import { NOTES_LOST, isNotesRunning } from './notes';

/** How often to ask how the notes go; the bot edits its message as often. */
const POLL_MS = 2000;

/**
 * ready: the mini app takes notes. off: here they go to the bot, as on a
 * server without models or outside MAX and Telegram.
 */
export type NotesAccess = 'loading' | 'ready' | 'off';

export type NotesUpload = { ok: true } | { ok: false; error: string };

/**
 * The user's latest notes on the server, followed while they are made.
 * The job outlives the screen: it is the server's, kept for an hour.
 * `followed` is whether this screen saw it running, sent it or found it
 * failed; onFinish hears when a job seen running ends.
 */
export function useNotesJob(onFinish?: (job: NotesJob) => void) {
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
                // A failure within the hour is still news: it says why.
                setFollowed(isNotesRunning(data) || data?.state === 'failed');
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

    const upload = useCallback(async (files: File[]): Promise<NotesUpload> => {
        try {
            const { data, response } = await api.POST('/notes', {
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
                return { ok: true };
            }
            if (response.status === 413) {
                return { ok: false, error: 'Файлы больше 20 МБ вместе. Выберите меньше страниц или разделите конспект на части.' };
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
            return { ok: false, error: 'Не получилось отправить конспект. Проверьте соединение и попробуйте ещё раз.' };
        }
    }, []);

    return { access, job, followed, upload };
}
