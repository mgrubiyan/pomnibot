import type { CardImage } from '../types';

type Entry = { status: 'loading' | 'ready' | 'error'; url?: string; bytes: number; used: number; promise?: Promise<void> };

/** Authorized blobs stay in memory while visible; eviction revokes their URLs. */
export function createImageCache(load: (assetId: string) => Promise<Blob>, decode: (url: string) => Promise<void>, limit = 40 * 1024 * 1024) {
    const entries = new Map<string, Entry>();
    const listeners = new Map<string, Set<() => void>>();
    let generation = 0;
    const notify = (id: string) => listeners.get(id)?.forEach((fn) => fn());
    const remove = (id: string) => { const entry = entries.get(id); if (entry?.url) URL.revokeObjectURL(entry.url); entries.delete(id); };
    const evict = () => {
        let bytes = [...entries.values()].reduce((sum, entry) => sum + entry.bytes, 0);
        for (const [id, entry] of [...entries].sort((a, b) => a[1].used - b[1].used)) {
            if (bytes <= limit && entries.size <= 120) break;
            if (listeners.get(id)?.size || entry.status === 'loading') continue;
            bytes -= entry.bytes; remove(id);
        }
    };
    return {
        status: (id: string) => entries.get(id)?.status ?? 'loading',
        url: (id: string) => entries.get(id)?.url,
        subscribe(id: string, fn: () => void) {
            const set = listeners.get(id) ?? new Set(); set.add(fn); listeners.set(id, set);
            return () => { set.delete(fn); if (!set.size) listeners.delete(id); evict(); };
        },
        async ensure(id: string, retry = false): Promise<void> {
            const existing = entries.get(id);
            if (existing && !retry) { existing.used = Date.now(); return existing.promise; }
            if (existing?.status === 'loading') return existing.promise;
            remove(id);
            const currentGeneration = generation;
            const entry: Entry = { status: 'loading', bytes: 0, used: Date.now() }; entries.set(id, entry); notify(id);
            entry.promise = (async () => {
                let url: string | undefined;
                try {
                    const blob = await load(id); url = URL.createObjectURL(blob);
                    await decode(url);
                    if (generation !== currentGeneration || entries.get(id) !== entry) { URL.revokeObjectURL(url); return; }
                    entry.url = url; entry.bytes = blob.size; entry.status = 'ready';
                } catch {
                    if (url) URL.revokeObjectURL(url);
                    if (generation !== currentGeneration || entries.get(id) !== entry) return;
                    entry.status = 'error';
                }
                notify(id); evict();
            })();
            return entry.promise;
        },
        clear() { generation++; for (const id of entries.keys()) { remove(id); notify(id); } },
    };
}

export function imageIDs(images?: CardImage[]): string[] { return (images ?? []).map((image) => image.assetId); }
export function moveImage(images: CardImage[], index: number, direction: -1 | 1): CardImage[] {
    const target = index + direction;
    if (target < 0 || target >= images.length) return images;
    const result = [...images]; [result[index], result[target]] = [result[target], result[index]]; return result;
}
