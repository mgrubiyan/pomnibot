import { useCallback, useEffect, useRef, useState } from 'react';
import { api } from '../api';
import { MAX_SPEECH_SECONDS, SPEECH_RATE, createAudioContext, micProblemOf, micProblemText, openMicrophone, record, type Recording } from './speech';
import { motionAllowed } from './motion';
import { openLiveSpeech, type LiveSpeech } from './speechStream';

/**
 * idle → starting (asking for the microphone) → recording → recognizing →
 * idle. Recording and recognizing hold the field; starting does not.
 */
export type VoiceState = 'idle' | 'starting' | 'recording' | 'recognizing';

/** Shorter than this is a slip of the finger, not an answer. */
const MIN_SPEECH_BYTES = Math.round(0.3 * SPEECH_RATE) * 2;

/** The server gives recognition 10 seconds; a dropped connection gives up here. */
const SPEECH_TIMEOUT_MS = 20_000;

/**
 * The live stream fixes the last phrase in about a second after the end;
 * past this the recording is sent whole instead.
 */
const LIVE_FINISH_MS = 3_000;

/**
 * The live stream fixes the text in about 0.2 s: a spinner shown for that
 * long would only flicker. Recognition shows as waiting after this.
 */
const WAITING_AFTER_MS = 300;

export const NOTHING_HEARD = 'Ничего не слышно. Попробуйте ещё раз ближе к микрофону.';

/**
 * Records a spoken answer and hands its text to onText; the reader checks
 * it and sends it like a typed one. A new card, or leaving, cancels it.
 * While the reader speaks, live is the text heard so far; when the live
 * stream fails, the whole recording is recognized at the end.
 * levelRef attaches an element that follows the loudness of the voice, so the
 * reader sees the microphone hears them before the first word comes;
 * waiting tells recognition has taken long enough to show; tried, that the
 * reader has used the microphone on this card.
 */
export function useVoiceAnswer(onText: (text: string) => void) {
    const [state, setState] = useState<VoiceState>('idle');
    const [seconds, setSeconds] = useState(0);
    const [problem, setProblem] = useState('');
    const [live, setLive] = useState('');
    const [waiting, setWaiting] = useState(false);
    const [tried, setTried] = useState(false);
    const ringRef = useRef<HTMLElement | null>(null);
    const levelRef = useCallback((element: HTMLElement | null) => {
        ringRef.current = element;
    }, []);

    // Every run gets a number: a microphone or recognition that answers
    // after a cancel is dropped.
    const runRef = useRef(0);
    const recordingRef = useRef<Recording | null>(null);
    const liveRef = useRef<LiveSpeech | null>(null);
    const timerRef = useRef<number | undefined>(undefined);
    const waitingRef = useRef<number | undefined>(undefined);
    const onTextRef = useRef(onText);
    useEffect(() => {
        onTextRef.current = onText;
    }, [onText]);

    const stopTimer = () => {
        window.clearInterval(timerRef.current);
        timerRef.current = undefined;
    };
    const stopWaiting = () => {
        window.clearTimeout(waitingRef.current);
        waitingRef.current = undefined;
        setWaiting(false);
    };

    const cancel = useCallback(() => {
        runRef.current += 1;
        stopTimer();
        recordingRef.current?.cancel();
        recordingRef.current = null;
        liveRef.current?.close();
        liveRef.current = null;
        stopWaiting();
        setState('idle');
        setSeconds(0);
        setProblem('');
        setLive('');
        setTried(false);
    }, []);

    useEffect(() => cancel, [cancel]);

    const finish = useCallback(async () => {
        const recording = recordingRef.current;
        if (!recording) return;
        recordingRef.current = null;
        const liveSpeech = liveRef.current;
        liveRef.current = null;
        stopTimer();
        const run = runRef.current;
        const pcm = recording.stop();
        setSeconds(0);
        if (pcm.length < MIN_SPEECH_BYTES) {
            liveSpeech?.close();
            setState('idle');
            setLive('');
            setProblem(NOTHING_HEARD);
            return;
        }
        setState('recognizing');
        waitingRef.current = window.setTimeout(() => setWaiting(true), WAITING_AFTER_MS);
        let result: Recognized;
        try {
            if (!liveSpeech) throw new Error('no live stream');
            const text = (await liveSpeech.finish(LIVE_FINISH_MS)).trim();
            result = text ? { text, failure: '' } : { text: '', failure: NOTHING_HEARD };
        } catch {
            if (run !== runRef.current) return;
            result = await recognizeWhole(pcm);
        }
        if (run !== runRef.current) return;
        stopWaiting();
        setState('idle');
        setLive('');
        setProblem(result.failure);
        if (result.text) onTextRef.current(result.text);
    }, []);

    const start = useCallback(async () => {
        const run = ++runRef.current;
        setProblem('');
        setTried(true);
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
        // The stream opens with the microphone, not before: a permission
        // prompt left waiting would keep a paid session open.
        const liveSpeech = openLiveSpeech((text) => {
            if (run === runRef.current) setLive(text);
        });
        liveRef.current = liveSpeech;
        // Rises at once and falls gently: a word lights the ring, a pause
        // lets it settle. Set on the element itself, past React: it changes
        // every tenth of a second.
        const moves = motionAllowed();
        let level = 0;
        const showLoudness = (heard: number) => {
            level = Math.max(heard, level * 0.75);
            const ring = ringRef.current;
            if (!ring) return;
            if (moves) ring.style.transform = `scale(${1 + level * 0.35})`;
            ring.style.opacity = String(level * 0.6);
        };
        recordingRef.current = record(context, stream, liveSpeech.send, showLoudness);
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

    return { state, seconds, problem, live, waiting, tried, levelRef, toggle, cancel };
}

interface Recognized {
    text: string;
    /** The line under the field when there is no text. */
    failure: string;
}

/** Recognizes a whole recording at once, when the live stream could not. */
async function recognizeWhole(pcm: Uint8Array): Promise<Recognized> {
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
            const text = data.text.trim();
            return text ? { text, failure: '' } : { text: '', failure: NOTHING_HEARD };
        }
        if (response.status === 403 || response.status === 503) {
            return { text: '', failure: 'Голосовой ответ здесь недоступен. Введите ответ текстом.' };
        }
        return { text: '', failure: 'Не получилось распознать. Попробуйте ещё раз или введите ответ.' };
    } catch {
        return { text: '', failure: 'Нет связи. Попробуйте ещё раз или введите ответ.' };
    } finally {
        window.clearTimeout(timeout);
    }
}
