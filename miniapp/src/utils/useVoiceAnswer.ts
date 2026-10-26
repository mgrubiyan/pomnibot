import { useCallback, useEffect, useRef, useState } from 'react';
import { api } from '../api';
import { MAX_SPEECH_SECONDS, SPEECH_RATE, createAudioContext, micProblemOf, micProblemText, openMicrophone, record, type Recording } from './speech';

/**
 * idle → starting (asking for the microphone) → recording → recognizing →
 * idle. Recording and recognizing hold the field; starting does not.
 */
export type VoiceState = 'idle' | 'starting' | 'recording' | 'recognizing';

/** Shorter than this is a slip of the finger, not an answer. */
const MIN_SPEECH_BYTES = Math.round(0.3 * SPEECH_RATE) * 2;

/** The server gives recognition 10 seconds; a dropped connection gives up here. */
const SPEECH_TIMEOUT_MS = 20_000;

export const NOTHING_HEARD = 'Ничего не слышно. Попробуйте ещё раз ближе к микрофону.';

/**
 * Records a spoken answer and hands its text to onText; the reader checks
 * it and sends it like a typed one. A new card, or leaving, cancels it.
 */
export function useVoiceAnswer(onText: (text: string) => void) {
    const [state, setState] = useState<VoiceState>('idle');
    const [seconds, setSeconds] = useState(0);
    const [problem, setProblem] = useState('');

    // Every run gets a number: a microphone or recognition that answers
    // after a cancel is dropped.
    const runRef = useRef(0);
    const recordingRef = useRef<Recording | null>(null);
    const timerRef = useRef<number | undefined>(undefined);
    const onTextRef = useRef(onText);
    useEffect(() => {
        onTextRef.current = onText;
    }, [onText]);

    const stopTimer = () => {
        window.clearInterval(timerRef.current);
        timerRef.current = undefined;
    };

    const cancel = useCallback(() => {
        runRef.current += 1;
        stopTimer();
        recordingRef.current?.cancel();
        recordingRef.current = null;
        setState('idle');
        setSeconds(0);
        setProblem('');
    }, []);

    useEffect(() => cancel, [cancel]);

    const finish = useCallback(async () => {
        const recording = recordingRef.current;
        if (!recording) return;
        recordingRef.current = null;
        stopTimer();
        const run = runRef.current;
        const pcm = recording.stop();
        setSeconds(0);
        if (pcm.length < MIN_SPEECH_BYTES) {
            setState('idle');
            setProblem(NOTHING_HEARD);
            return;
        }
        setState('recognizing');
        let text = '';
        let failure = '';
        const abort = new AbortController();
        const timeout = window.setTimeout(() => abort.abort(), SPEECH_TIMEOUT_MS);
        try {
            const { data, response } = await api.POST('/speech', {
                signal: abort.signal,
                // The schema types binary parts as strings.
                body: { audio: new Blob([pcm as BlobPart], { type: 'application/octet-stream' }) as unknown as string },
                bodySerializer: (body) => {
                    const form = new FormData();
                    form.append('audio', body.audio as unknown as Blob, 'answer.pcm');
                    return form;
                },
            });
            if (response.ok && data) {
                text = data.text.trim();
                if (!text) failure = NOTHING_HEARD;
            } else if (response.status === 403 || response.status === 503) {
                failure = 'Голосовой ответ здесь недоступен. Введите ответ текстом.';
            } else {
                failure = 'Не получилось распознать. Попробуйте ещё раз или введите ответ.';
            }
        } catch {
            failure = 'Нет связи. Попробуйте ещё раз или введите ответ.';
        } finally {
            window.clearTimeout(timeout);
        }
        if (run !== runRef.current) return;
        setState('idle');
        setProblem(failure);
        if (text) onTextRef.current(text);
    }, []);

    const start = useCallback(async () => {
        const run = ++runRef.current;
        setProblem('');
        setState('starting');
        let context: AudioContext;
        try {
            // Before any await: a context made later may start suspended.
            context = createAudioContext();
        } catch (error) {
            setState('idle');
            setProblem(micProblemText(micProblemOf(error)));
            return;
        }
        let stream: MediaStream;
        try {
            stream = await openMicrophone();
        } catch (error) {
            void context.close().catch(() => {});
            if (run !== runRef.current) return;
            setState('idle');
            setProblem(micProblemText(micProblemOf(error)));
            return;
        }
        if (run !== runRef.current) {
            stream.getTracks().forEach((track) => track.stop());
            void context.close().catch(() => {});
            return;
        }
        recordingRef.current = record(context, stream);
        const startedAt = Date.now();
        setSeconds(0);
        setState('recording');
        timerRef.current = window.setInterval(() => {
            const elapsed = (Date.now() - startedAt) / 1000;
            setSeconds(Math.floor(elapsed));
            // The server takes 30 seconds: the recording ends there itself.
            if (elapsed >= MAX_SPEECH_SECONDS) void finish();
        }, 250);
    }, [finish]);

    // While the microphone opens, a permission prompt may wait on the
    // reader for long: a tap gives up on it, and typing still works.
    const toggle = useCallback(() => {
        if (state === 'idle') void start();
        else if (state === 'starting') cancel();
        else if (state === 'recording') void finish();
    }, [state, start, finish, cancel]);

    return { state, seconds, problem, toggle, cancel };
}
