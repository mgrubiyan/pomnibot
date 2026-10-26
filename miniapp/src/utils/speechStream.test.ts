import { describe, expect, it } from 'bun:test';
import { openLiveSpeech, speechStreamURL } from './speechStream';

/** A WebSocket the test opens, answers and closes by hand. */
class FakeSocket {
    static readonly CONNECTING = 0;
    static readonly OPEN = 1;
    static readonly CLOSED = 3;
    static last: FakeSocket;

    readyState = FakeSocket.CONNECTING;
    binaryType = 'blob';
    sent: (string | Uint8Array)[] = [];
    onopen: (() => void) | null = null;
    onmessage: ((event: { data: string }) => void) | null = null;
    onerror: (() => void) | null = null;
    onclose: (() => void) | null = null;

    constructor(readonly url: string) {
        FakeSocket.last = this;
    }

    send(message: string | Uint8Array) {
        if (this.readyState !== FakeSocket.OPEN) throw new Error('send on a socket not open');
        this.sent.push(message);
    }

    close() {
        if (this.readyState === FakeSocket.CLOSED) return;
        this.readyState = FakeSocket.CLOSED;
        this.onclose?.();
    }

    open() {
        this.readyState = FakeSocket.OPEN;
        this.onopen?.();
    }

    reply(value: object) {
        this.onmessage?.({ data: JSON.stringify(value) });
    }
}

function open(heard: string[] = []) {
    const live = openLiveSpeech((text) => heard.push(text), {
        url: 'ws://test/api/speech/stream',
        authorization: 'max signed',
        WebSocket: FakeSocket as unknown as typeof WebSocket,
    });
    return { live, socket: FakeSocket.last, heard };
}

describe('openLiveSpeech', () => {
    it('signs in first, then sends the audio held while connecting', () => {
        const { live, socket } = open();
        live.send(new Uint8Array([1, 0]));
        socket.open();
        live.send(new Uint8Array([2, 0]));
        expect(socket.binaryType).toBe('arraybuffer');
        expect(socket.sent).toEqual([JSON.stringify({ authorization: 'max signed' }), new Uint8Array([1, 0]), new Uint8Array([2, 0])]);
    });

    it('tells the text as it comes and gives the fixed one at the end', async () => {
        const { live, socket, heard } = open();
        socket.open();
        socket.reply({ text: 'первичный' });
        socket.reply({ text: 'первичный ключ' });
        const fixed = live.finish(1000);
        expect(socket.sent.at(-1)).toBe(JSON.stringify({ type: 'end' }));
        socket.reply({ text: 'Первичный ключ.' });
        socket.reply({ text: 'Первичный ключ.', done: true });
        expect(await fixed).toBe('Первичный ключ.');
        expect(heard).toEqual(['первичный', 'первичный ключ', 'Первичный ключ.']);
        expect(socket.readyState).toBe(FakeSocket.CLOSED);
    });

    it('fails the end when the server refuses', async () => {
        const { live, socket } = open();
        socket.open();
        const fixed = live.finish(1000);
        socket.reply({ error: 'forbidden' });
        await expect(fixed).rejects.toThrow('forbidden');
    });

    it('fails the end when the connection drops, and says nothing after', async () => {
        const { live, socket, heard } = open();
        socket.open();
        socket.close();
        socket.reply({ text: 'поздно' });
        await expect(live.finish(1000)).rejects.toThrow();
        expect(heard).toEqual([]);
    });

    it('gives up on a server that does not end in time', async () => {
        const { live, socket } = open();
        socket.open();
        await expect(live.finish(10)).rejects.toThrow('too slow');
        expect(socket.readyState).toBe(FakeSocket.CLOSED);
    });

    it('drops the session on close', () => {
        const { live, socket, heard } = open();
        socket.open();
        live.close();
        live.send(new Uint8Array([1, 0]));
        expect(socket.readyState).toBe(FakeSocket.CLOSED);
        expect(socket.sent.length).toBe(1);
        expect(heard).toEqual([]);
    });
});

describe('speechStreamURL', () => {
    it('keeps the page secure', () => {
        expect(speechStreamURL({ protocol: 'https:', host: 'pomni.example' })).toBe('wss://pomni.example/api/speech/stream');
        expect(speechStreamURL({ protocol: 'http:', host: 'localhost:5280' })).toBe('ws://localhost:5280/api/speech/stream');
    });
});
