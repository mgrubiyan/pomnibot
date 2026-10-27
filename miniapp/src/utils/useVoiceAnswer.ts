import { useCallback, useEffect, useRef, useState } from 'react';
import { api } from '../api';
import {
    MAX_SPEECH_SECONDS,
    SPEECH_RATE,
    createAudioContext,
    micProblemOf,
    micProblemText,
    openMicrophone,
    record,
    shiftWave,
    silentWave,
    type Recording,
} from './speech';
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
 * levelRef attaches an element that follows the loudness of the voice and
 * waveRef one whose children are the bars of its recent history, so the
 * reader sees the microphone hears them before the first word comes;
 * waiting tells recognition has taken long enough to show.
 */
export function useVoiceAnswer(onText: (text: string) => void) {
    const [state, setState] = useState<VoiceState>('idle');
    const [seconds, setSeconds] = useState(0);
    const [problem, setProblem] = useState('');
    const [live, setLive] = useState('');
    const [waiting, setWaiting] = useState(false);
    const ringRef = useRef<HTMLElement | null>(null);
    const levelRef = useCallback((element: HTMLElement | null) => {
        ringRef.current = element;
    }, []);
    const barsRef = useRef<HTMLElement | null>(null);
    const waveRef = useCallback((element: HTMLElement | null) => {
        barsRef.current = element;
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
        // The ring rises at once and falls gently: a word lights it, a pause
        // lets it settle. The wave keeps each piece as it was heard. Both are
        // set on the elements themselves, past React: they change every tenth
        // of a second. With reduced motion only brightness changes.
        const moves = motionAllowed();
        let level = 0;
        let wave = silentWave();
        const showLoudness = (heard: number) => {
            level = Math.max(heard, level * 0.75);
            wave = shiftWave(wave, heard);
            const ring = ringRef.current;
            if (ring) {
                if (moves) ring.style.transform = `scale(${1 + level * 0.35})`;
                ring.style.opacity = String(level * 0.6);
            }
            const bars = barsRef.current;
            if (!bars) return;
            if (moves) {
                wave.forEach((value, i) => {
                    const bar = bars.children[i] as HTMLElement | undefined;
                    if (bar) bar.style.transform = `scaleY(${0.15 + 0.85 * value})`;
                });
            } else {
                bars.style.opacity = String(0.4 + 0.6 * level);
            }
        };
        // Flat from the start, whatever the last recording left.
        showLoudness(0);
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

    return { state, seconds, problem, live, waiting, levelRef, waveRef, start, finish, cancel };
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
