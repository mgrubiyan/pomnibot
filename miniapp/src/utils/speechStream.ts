import { miniAppAuthorization } from '../platform';

/**
 * Live recognition over /api/speech/stream: the recording goes to the
 * server while the reader speaks, and the text heard so far comes back.
 * The WebSocket signs in with its first message, as browsers set no
 * headers on one.
 */
export interface LiveSpeech {
    /** Passes on a piece of the recording; held until the socket opens. */
    send: (pcm: Uint8Array) => void;
    /**
     * Ends the recording and waits for the fixed text. Rejects when the
     * stream broke or took longer than waitMs: the recording then goes the
     * slow way.
     */
    finish: (waitMs: number) => Promise<string>;
    /** Drops the session. */
    close: () => void;
}

export interface LiveSpeechOptions {
    url?: string;
    authorization?: string;
    WebSocket?: typeof WebSocket;
}

export function speechStreamURL(location: Pick<Location, 'protocol' | 'host'> = window.location): string {
    return `${location.protocol === 'https:' ? 'wss' : 'ws'}://${location.host}/api/speech/stream`;
}

interface Reply {
    text?: string;
    done?: boolean;
    error?: string;
}

export function openLiveSpeech(onText: (text: string) => void, options: LiveSpeechOptions = {}): LiveSpeech {
    const Socket = options.WebSocket ?? WebSocket;
    const socket = new Socket(options.url ?? speechStreamURL());
    socket.binaryType = 'arraybuffer';

    const queue: (Uint8Array | string)[] = [];
    // Over: the stream broke or was dropped; fixed: the final text came.
    let over = false;
    let fixed: string | undefined;
    let settle: { resolve: (text: string) => void; reject: (error: Error) => void } | undefined;

    const fail = (reason: string) => {
        if (over || fixed !== undefined) return;
        over = true;
        settle?.reject(new Error(`speech stream: ${reason}`));
        settle = undefined;
        socket.close();
    };
    const put = (message: Uint8Array | string) => {
        if (over || fixed !== undefined) return;
        if (socket.readyState === Socket.OPEN) socket.send(message as Uint8Array<ArrayBuffer> | string);
        else if (socket.readyState === Socket.CONNECTING) queue.push(message);
    };

    socket.onopen = () => {
        socket.send(JSON.stringify({ authorization: options.authorization ?? miniAppAuthorization(window) ?? '' }));
        for (const message of queue.splice(0)) socket.send(message as Uint8Array<ArrayBuffer> | string);
    };
    socket.onmessage = (event: MessageEvent) => {
        let reply: Reply;
        try {
            reply = JSON.parse(String(event.data)) as Reply;
        } catch {
            return;
        }
        if (reply.error) {
            fail(reply.error);
            return;
        }
        if (over || fixed !== undefined) return;
        if (reply.done) {
            fixed = reply.text ?? '';
            settle?.resolve(fixed);
            settle = undefined;
            socket.close();
            return;
        }
        if (typeof reply.text === 'string') onText(reply.text);
    };
    socket.onerror = () => fail('error');
    socket.onclose = () => fail('closed');

    return {
        send: put,
        finish: (waitMs) => {
            if (fixed !== undefined) return Promise.resolve(fixed);
            if (over) return Promise.reject(new Error('speech stream: over'));
            put(JSON.stringify({ type: 'end' }));
            return new Promise<string>((resolve, reject) => {
                const timer = setTimeout(() => fail('too slow'), waitMs);
                settle = {
                    resolve: (text) => {
                        clearTimeout(timer);
                        resolve(text);
                    },
                    reject: (error) => {
                        clearTimeout(timer);
                        reject(error);
                    },
                };
            });
        },
        close: () => {
            over = true;
            settle = undefined;
            socket.close();
        },
    };
}
