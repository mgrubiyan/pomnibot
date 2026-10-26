import { describe, expect, it } from 'bun:test';
import { createImageCache, imageIDs, moveImage } from './imageCache';
import type { CardImage } from '../types';

const image = (assetId: string): CardImage => ({ assetId, url: `/api/images/${assetId}`, alt: 'Figure', width: 100, height: 50, sourceName: 'book.pdf', sourcePage: 2 });
describe('PDF image cache', () => {
    it('shares a single authorized load, waits for decoding and revokes on cleanup', async () => {
        let calls = 0; let decoded = false;
        const cache = createImageCache(async (id) => { calls++; expect(id).toBe('a'); return new Blob(['png']); }, async () => { await Promise.resolve(); decoded = true; });
        const first = cache.ensure('a'); const second = cache.ensure('a');
        expect(cache.status('a')).toBe('loading');
        await Promise.all([first, second]);
        expect(calls).toBe(1); expect(decoded).toBe(true); expect(cache.status('a')).toBe('ready');
        const url = cache.url('a'); expect(url).toStartWith('blob:');
        cache.clear(); expect(cache.url('a')).toBeUndefined();
        await expect(fetch(url!)).rejects.toThrow();
    });
    it('keeps a failure visible and retries only on request', async () => {
        let calls = 0;
        const cache = createImageCache(async () => { if (++calls === 1) throw new Error('offline'); return new Blob(['png']); }, async () => {});
        await cache.ensure('a'); expect(cache.status('a')).toBe('error');
        await cache.ensure('a'); expect(calls).toBe(1);
        await cache.ensure('a', true); expect(cache.status('a')).toBe('ready'); expect(calls).toBe(2); cache.clear();
    });
    it('never restores an in-flight blob after cleanup', async () => {
        let finish: (() => void) | undefined;
        const cache = createImageCache(async () => new Blob(['png']), () => new Promise<void>((resolve) => { finish = resolve; }));
        const pending = cache.ensure('a'); await Promise.resolve(); cache.clear(); finish?.(); await pending;
        expect(cache.url('a')).toBeUndefined();
    });
    it('evicts unused blobs but retains subscribed images', async () => {
        const cache = createImageCache(async () => new Blob(['data']), async () => {}, 4);
        const unsubscribe = cache.subscribe('a', () => {});
        await cache.ensure('a'); await cache.ensure('b');
        expect(cache.url('a')).toBeDefined(); expect(cache.url('b')).toBeUndefined(); unsubscribe(); cache.clear();
    });
});
it('preserves attachment order and accepts missing legacy arrays', () => {
    expect(imageIDs()).toEqual([]);
    const images = [image('a'), image('b'), image('c')];
    expect(imageIDs(moveImage(images, 1, -1))).toEqual(['b', 'a', 'c']);
    expect(imageIDs(moveImage(images, 1, 1))).toEqual(['a', 'c', 'b']);
    expect(imageIDs(images)).toEqual(['a', 'b', 'c']);
});
