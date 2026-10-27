/**
 * Answering a card by voice: the microphone, a recording in the format the
 * server's recognizer takes, and why it may not work.
 *
 * The recording is raw 16-bit PCM at 16 kHz: a browser makes it with Web
 * Audio alone, while MediaRecorder gives WebM or MP4 that the recognizer
 * does not read.
 */

export const SPEECH_RATE = 16000;
export const MAX_SPEECH_SECONDS = 30;
/** As much as the server takes: 30 seconds of the PCM. */
export const MAX_SPEECH_BYTES = MAX_SPEECH_SECONDS * SPEECH_RATE * 2;

/**
 * What MAX tells about a permission. Its script starts every flag false and
 * lays over what the client sends, so a false grant means no only once MAX
 * has asked that level.
 */
interface PermissionInfo {
    appAccessGranted?: boolean;
    appAccessRequested?: boolean;
    systemAccessGranted?: boolean;
    systemAccessRequested?: boolean;
}

/** window.WebApp.PermissionsManager: Android and the web client only. */
export interface MaxPermissions {
    isInited: boolean;
    init: () => Promise<unknown>;
    requestAccess: (what: ('mic' | 'camera')[], reason?: string) => Promise<{ mic?: PermissionInfo } | undefined>;
}

/** The parts of window that voice input uses, so tests can play them. */
export interface SpeechSDK {
    navigator: { mediaDevices?: { getUserMedia?: (constraints: MediaStreamConstraints) => Promise<MediaStream> } };
    AudioContext?: typeof AudioContext;
    webkitAudioContext?: typeof AudioContext;
    WebApp?: { initData?: string; platform?: string; PermissionsManager?: MaxPermissions };
}

const browser = () => window as unknown as SpeechSDK;

/** The MAX client the mini app runs in, if it does. */
function maxApp(sdk: SpeechSDK) {
    return sdk.WebApp?.initData ? sdk.WebApp : undefined;
}

/**
 * Whether the mic button can work here. MAX lets a mini app use the
 * microphone only on Android and on the web, through its permissions.
 */
export function voiceInputAvailable(sdk: SpeechSDK = browser()): boolean {
    if (!sdk.navigator.mediaDevices?.getUserMedia || !(sdk.AudioContext ?? sdk.webkitAudioContext)) {
        return false;
    }
    const max = maxApp(sdk);
    if (max && ((max.platform !== 'android' && max.platform !== 'web') || !max.PermissionsManager)) {
        return false;
    }
    return true;
}

/**
 * Why the microphone did not open: MAX refused it to the mini app, the
 * phone or browser refused it to MAX or the page, there is none, or else.
 */
export type MicProblem = 'denied-max' | 'denied-system' | 'unavailable' | 'failed';

export class MicError extends Error {
    readonly problem: MicProblem;

    constructor(problem: MicProblem, options?: { cause?: unknown }) {
        super(`microphone: ${problem}`, options);
        this.problem = problem;
    }
}

/** Reads a MAX bridge error ({ error: { code } }) or a getUserMedia one. */
export function micProblemOf(error: unknown): MicProblem {
    if (error instanceof MicError) {
        return error.problem;
    }
    const code = (error as { error?: { code?: unknown } } | undefined)?.error?.code;
    if (typeof code === 'string') {
        const reason = code.slice(code.lastIndexOf('.') + 1);
        if (reason === 'app_permission_denied' || reason === 'mic_app_permission_denied') return 'denied-max';
        if (reason === 'system_permission_denied' || reason === 'mic_system_permission_denied') return 'denied-system';
        if (reason === 'mic_not_supported') return 'unavailable';
        return 'failed';
    }
    const name = (error as { name?: unknown } | undefined)?.name;
    if (name === 'NotAllowedError' || name === 'SecurityError') return 'denied-system';
    if (name === 'NotFoundError' || name === 'NotReadableError' || name === 'OverconstrainedError') return 'unavailable';
    return 'failed';
}

/**
 * Opens the microphone: in MAX it first asks for the mini app's permission,
 * and never sends the reader to the settings, since declining that closes
 * the mini app. MAX tells a fresh refusal by throwing, and an old one by
 * the flags of a level it has asked; the web client has no system level.
 * Past MAX, getUserMedia decides. Throws a MicError.
 */
export async function openMicrophone(sdk: SpeechSDK = browser()): Promise<MediaStream> {
    try {
        const permissions = maxApp(sdk)?.PermissionsManager;
        if (permissions) {
            if (!permissions.isInited) {
                await permissions.init();
            }
            const mic = (await permissions.requestAccess(['mic'], 'Чтобы отвечать голосом'))?.mic;
            if (mic?.appAccessRequested && !mic.appAccessGranted) {
                throw new MicError('denied-max');
            }
            if (maxApp(sdk)?.platform !== 'web' && mic?.systemAccessRequested && !mic.systemAccessGranted) {
                throw new MicError('denied-system');
            }
        }
        const getUserMedia = sdk.navigator.mediaDevices?.getUserMedia;
        if (!getUserMedia) {
            throw new MicError('unavailable');
        }
        return await getUserMedia.call(sdk.navigator.mediaDevices, { audio: true });
    } catch (error) {
        // Which refusal it was shows only here: the line under the field
        // tells less.
        console.warn('[voice] the microphone did not open', error);
        throw error instanceof MicError ? error : new MicError(micProblemOf(error), { cause: error });
    }
}

/** The line under the answer field for a microphone that did not open. */
export function micProblemText(problem: MicProblem, sdk: SpeechSDK = browser()): string {
    const max = maxApp(sdk);
    switch (problem) {
        case 'denied-max':
            return 'Нет доступа к микрофону. Разрешите его в настройках MAX: Безопасность → Мини-приложения.';
        case 'denied-system':
            if (!max) return 'Нет доступа к микрофону. Разрешите его в настройках браузера.';
            return max.platform === 'web'
                ? 'Нет доступа к микрофону. Разрешите его сайту MAX в настройках браузера и нажмите ещё раз.'
                : 'Нет доступа к микрофону. Разрешите его MAX в настройках телефона.';
        case 'unavailable':
            return 'Микрофон не найден.';
        case 'failed':
            return 'Не получилось включить микрофон. Попробуйте ещё раз.';
    }
}

/**
 * Mono samples at any rate → 16-bit little-endian PCM at 16 kHz, as they
 * are recorded, at most 30 seconds. Going down, each output sample is the
 * mean of the input ones it covers, which keeps the high hiss out of
 * speech; going up, the nearest earlier sample. An output sample is given
 * once all its input has come, so the pieces add up to toPCM16 of it all.
 */
export class PCMEncoder {
    private readonly ratio: number;
    /** The input not yet used up; its first sample has index base. */
    private pending = new Float32Array(0);
    private base = 0;
    private received = 0;
    private next = 0;

    constructor(rate: number) {
        this.ratio = rate / SPEECH_RATE;
    }

    /** The PCM the new samples complete; empty past 30 seconds. */
    push(samples: Float32Array): Uint8Array {
        if (this.next >= MAX_SPEECH_BYTES / 2) return new Uint8Array(0);
        const joined = new Float32Array(this.pending.length + samples.length);
        joined.set(this.pending);
        joined.set(samples, this.pending.length);
        this.pending = joined;
        this.received += samples.length;

        const { ratio } = this;
        const stop = Math.min(Math.floor(this.received / ratio), MAX_SPEECH_BYTES / 2);
        const out = new DataView(new ArrayBuffer(Math.max(0, stop - this.next) * 2));
        for (let i = this.next; i < stop; i++) {
            let value: number;
            if (ratio <= 1) {
                value = this.sample(Math.floor(i * ratio));
            } else {
                const from = Math.floor(i * ratio);
                const to = Math.max(from + 1, Math.min(this.received, Math.floor((i + 1) * ratio)));
                let sum = 0;
                for (let j = from; j < to; j++) sum += this.sample(j);
                value = sum / (to - from);
            }
            const clipped = Math.max(-1, Math.min(1, value));
            out.setInt16((i - this.next) * 2, Math.round(clipped < 0 ? clipped * 0x8000 : clipped * 0x7fff), true);
        }
        this.next = Math.max(this.next, stop);
        // The next output sample starts here; what lies before is done.
        const keep = Math.min(Math.floor(this.next * ratio), this.received);
        this.pending = this.pending.slice(keep - this.base);
        this.base = keep;
        return new Uint8Array(out.buffer);
    }

    private sample(index: number): number {
        return this.pending[index - this.base];
    }
}

/** The PCM of a whole recording: see PCMEncoder. */
export function toPCM16(chunks: readonly Float32Array[], rate: number): Uint8Array {
    const encoder = new PCMEncoder(rate);
    return joinBytes(chunks.map((chunk) => encoder.push(chunk)));
}

export function joinBytes(pieces: readonly Uint8Array[]): Uint8Array {
    const out = new Uint8Array(pieces.reduce((sum, piece) => sum + piece.length, 0));
    let offset = 0;
    for (const piece of pieces) {
        out.set(piece, offset);
        offset += piece.length;
    }
    return out;
}

/**
 * How loud a piece of the recording is, from 0 (silence, -50 dBFS and
 * under) to 1 (a raised voice, -10 dBFS and over).
 */
export function loudness(samples: Float32Array): number {
    if (samples.length === 0) return 0;
    let sum = 0;
    for (const sample of samples) sum += sample * sample;
    const rms = Math.sqrt(sum / samples.length);
    const db = 20 * Math.log10(Math.max(rms, 1e-9));
    return Math.max(0, Math.min(1, (db + 50) / 40));
}

/** Bars of the waveform shown while the reader speaks. */
export const WAVE_BARS = 25;

/**
 * The waveform after one more piece of the recording: the newest loudness
 * comes in on the right and the oldest leaves on the left.
 */
export function shiftWave(wave: readonly number[], level: number): number[] {
    return [...wave.slice(1), level];
}

/** A waveform of silence, for a recording that has not begun. */
export const silentWave = (): number[] => new Array<number>(WAVE_BARS).fill(0);

/** A recording in progress; onPCM of record gets it as it is heard. */
export interface Recording {
    /** Stops and gives the PCM; the microphone is released. */
    stop: () => Uint8Array;
    /** Stops and drops what was heard. */
    cancel: () => void;
}

/** A context made in the tap itself, before any await: browsers start one made later suspended. */
export function createAudioContext(sdk: SpeechSDK = browser()): AudioContext {
    const Context = sdk.AudioContext ?? sdk.webkitAudioContext;
    if (!Context) {
        throw new MicError('unavailable');
    }
    return new Context();
}

/**
 * Records the stream through Web Audio. ScriptProcessorNode is deprecated,
 * but it is in every webview, while an AudioWorklet needs a module the
 * page's policy may not load.
 */
export function record(
    context: AudioContext,
    stream: MediaStream,
    onPCM?: (pcm: Uint8Array) => void,
    onLoudness?: (level: number) => void,
): Recording {
    const encoder = new PCMEncoder(context.sampleRate);
    const pieces: Uint8Array[] = [];
    const source = context.createMediaStreamSource(stream);
    const processor = context.createScriptProcessor(4096, 1, 1);
    // The processor runs only while connected to the output; silent, so the
    // reader does not hear themselves.
    const mute = context.createGain();
    mute.gain.value = 0;
    processor.onaudioprocess = (event) => {
        const samples = event.inputBuffer.getChannelData(0);
        onLoudness?.(loudness(samples));
        const pcm = encoder.push(samples);
        if (pcm.length === 0) return;
        pieces.push(pcm);
        onPCM?.(pcm);
    };
    source.connect(processor);
    processor.connect(mute);
    mute.connect(context.destination);
    void context.resume().catch(() => {});

    let open = true;
    const release = () => {
        if (!open) return;
        open = false;
        processor.onaudioprocess = null;
        source.disconnect();
        processor.disconnect();
        mute.disconnect();
        stream.getTracks().forEach((track) => track.stop());
        void context.close().catch(() => {});
    };
    return {
        stop: () => {
            release();
            return joinBytes(pieces);
        },
        cancel: release,
    };
}
