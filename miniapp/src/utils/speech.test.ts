import { describe, expect, it } from 'bun:test';
import { MAX_SPEECH_BYTES, PCMEncoder, SPEECH_RATE, micProblemOf, openMicrophone, toPCM16, voiceInputAvailable, type SpeechSDK } from './speech';

const samples = (bytes: Uint8Array) => {
    const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
    return Array.from({ length: bytes.length / 2 }, (_, i) => view.getInt16(i * 2, true));
};

describe('toPCM16', () => {
    it('writes 16-bit little-endian samples, clipped to full scale', () => {
        const pcm = toPCM16([new Float32Array([0, 0.5, -0.5, 1, -1, 2, -2])], SPEECH_RATE);
        expect(samples(pcm)).toEqual([0, 16384, -16384, 32767, -32768, 32767, -32768]);
    });

    it('takes 16 kHz out of 48 kHz, averaging each three samples', () => {
        const pcm = toPCM16([new Float32Array([0.3, 0.3, 0.3, -0.6, -0.6, -0.6])], 48000);
        expect(samples(pcm)).toEqual([Math.round(0.3 * 32767), Math.round(-0.6 * 32768)]);
    });

    it('keeps the length across chunks and odd rates', () => {
        const second = Array.from({ length: 441 }, () => new Float32Array(100));
        expect(toPCM16(second, 44100).length).toBe(SPEECH_RATE * 2);
    });

    it('stretches a slower rate', () => {
        expect(toPCM16([new Float32Array(8000)], 8000).length).toBe(SPEECH_RATE * 2);
    });

    it('stops at 30 seconds, as much as the server takes', () => {
        const pcm = toPCM16([new Float32Array(SPEECH_RATE * 31)], SPEECH_RATE);
        expect(pcm.length).toBe(MAX_SPEECH_BYTES);
    });
});

/** The whole-recording encoder the mini app had before it streamed. */
function referencePCM(input: Float32Array, rate: number): number[] {
    const ratio = rate / SPEECH_RATE;
    const length = Math.min(Math.floor(input.length / ratio), MAX_SPEECH_BYTES / 2);
    return Array.from({ length }, (_, i) => {
        let value: number;
        if (ratio <= 1) {
            value = input[Math.floor(i * ratio)];
        } else {
            const from = Math.floor(i * ratio);
            const to = Math.max(from + 1, Math.min(input.length, Math.floor((i + 1) * ratio)));
            let sum = 0;
            for (let j = from; j < to; j++) sum += input[j];
            value = sum / (to - from);
        }
        const clipped = Math.max(-1, Math.min(1, value));
        return Math.round(clipped < 0 ? clipped * 0x8000 : clipped * 0x7fff);
    });
}

describe('PCMEncoder', () => {
    // Pieces sent while the reader speaks are what the whole recording
    // would be: the fallback upload and the stream hear the same.
    for (const rate of [48000, 44100, 22050, 16000, 8000]) {
        it(`gives the same PCM in pieces of any length at ${rate} Hz`, () => {
            let seed = rate;
            const random = () => (seed = (seed * 1103515245 + 12345) % 2 ** 31) / 2 ** 31;
            const input = Float32Array.from({ length: rate }, () => random() * 2.2 - 1.1);
            const encoder = new PCMEncoder(rate);
            const out: number[] = [];
            for (let at = 0; at < input.length;) {
                const size = Math.floor(random() * 5000);
                out.push(...samples(encoder.push(input.subarray(at, at + size))));
                at += size;
            }
            expect(out).toEqual(referencePCM(input, rate));
        });
    }

    it('gives nothing past 30 seconds', () => {
        const encoder = new PCMEncoder(SPEECH_RATE);
        expect(encoder.push(new Float32Array(SPEECH_RATE * 30)).length).toBe(MAX_SPEECH_BYTES);
        expect(encoder.push(new Float32Array(SPEECH_RATE)).length).toBe(0);
    });
});

const media = { mediaDevices: { getUserMedia: async () => ({}) as MediaStream } };
const audio = { AudioContext: class {} as unknown as typeof AudioContext };

describe('voiceInputAvailable', () => {
    it('needs a microphone and Web Audio', () => {
        expect(voiceInputAvailable({ navigator: media, ...audio })).toBe(true);
        expect(voiceInputAvailable({ navigator: {}, ...audio })).toBe(false);
        expect(voiceInputAvailable({ navigator: media })).toBe(false);
    });

    it('in MAX, only where its permissions manager lets the microphone in', () => {
        const manager = { init: async () => {}, isInited: false, requestAccess: async () => ({}) };
        expect(voiceInputAvailable({ navigator: media, ...audio, WebApp: { initData: 'x', platform: 'android', PermissionsManager: manager } })).toBe(true);
        expect(voiceInputAvailable({ navigator: media, ...audio, WebApp: { initData: 'x', platform: 'web', PermissionsManager: manager } })).toBe(true);
        // MAX gives no microphone on iOS yet, nor in an older client.
        expect(voiceInputAvailable({ navigator: media, ...audio, WebApp: { initData: 'x', platform: 'ios', PermissionsManager: manager } })).toBe(false);
        expect(voiceInputAvailable({ navigator: media, ...audio, WebApp: { initData: 'x', platform: 'android' } })).toBe(false);
        // The MAX script outside MAX leaves the browser as it is.
        expect(voiceInputAvailable({ navigator: media, ...audio, WebApp: { initData: '' } })).toBe(true);
    });
});

describe('micProblemOf', () => {
    it('reads MAX permission codes', () => {
        const max = (code: string) => ({ error: { code: `client.permissions_request_access.${code}` } });
        expect(micProblemOf(max('mic_app_permission_denied'))).toBe('denied-max');
        expect(micProblemOf(max('app_permission_denied'))).toBe('denied-max');
        expect(micProblemOf(max('mic_system_permission_denied'))).toBe('denied-system');
        expect(micProblemOf(max('system_permission_denied'))).toBe('denied-system');
        expect(micProblemOf(max('mic_not_supported'))).toBe('unavailable');
        expect(micProblemOf(max('not_found'))).toBe('failed');
    });

    it('reads browser errors', () => {
        expect(micProblemOf(new DOMException('', 'NotAllowedError'))).toBe('denied-system');
        expect(micProblemOf(new DOMException('', 'NotFoundError'))).toBe('unavailable');
        expect(micProblemOf(new DOMException('', 'NotReadableError'))).toBe('unavailable');
        expect(micProblemOf(new Error('boom'))).toBe('failed');
        expect(micProblemOf(undefined)).toBe('failed');
    });
});

describe('openMicrophone', () => {
    const stream = {} as MediaStream;

    it('asks MAX first, then the browser', async () => {
        const calls: string[] = [];
        const sdk: SpeechSDK = {
            navigator: { mediaDevices: { getUserMedia: async (c) => { calls.push(`getUserMedia ${JSON.stringify(c)}`); return stream; } } },
            WebApp: {
                initData: 'x',
                PermissionsManager: {
                    isInited: false,
                    init: async () => { calls.push('init'); },
                    requestAccess: async (what, reason) => { calls.push(`requestAccess ${what.join()} ${reason}`); return { mic: { appAccessGranted: true, systemAccessGranted: true } }; },
                },
            },
        };
        expect(await openMicrophone(sdk)).toBe(stream);
        expect(calls).toEqual(['init', 'requestAccess mic Чтобы отвечать голосом', 'getUserMedia {"audio":true}']);
    });

    it('stops at a refusal MAX reports without throwing', async () => {
        let asked = false;
        const sdk: SpeechSDK = {
            navigator: { mediaDevices: { getUserMedia: async () => { asked = true; return stream; } } },
            WebApp: {
                initData: 'x',
                PermissionsManager: { isInited: true, init: async () => {}, requestAccess: async () => ({ mic: { appAccessGranted: false } }) },
            },
        };
        await expect(openMicrophone(sdk)).rejects.toMatchObject({ problem: 'denied-max' });
        expect(asked).toBe(false);
    });

    it('turns a browser refusal into a problem', async () => {
        const sdk: SpeechSDK = { navigator: { mediaDevices: { getUserMedia: async () => { throw new DOMException('', 'NotAllowedError'); } } } };
        await expect(openMicrophone(sdk)).rejects.toMatchObject({ problem: 'denied-system' });
    });
});
