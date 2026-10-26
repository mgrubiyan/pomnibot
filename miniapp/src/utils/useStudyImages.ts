import { useCallback, useEffect, useMemo, useSyncExternalStore } from 'react';
import { api } from '../api';
import type { CardImage } from '../types';
import { createImageCache } from './imageCache';

export const studyImageCache = createImageCache(async (assetId) => {
    const { data, response } = await api.GET('/images/{assetId}', { params: { path: { assetId } }, parseAs: 'blob' });
    if (!response.ok || !data) throw new Error('Image unavailable');
    return data;
}, async (url) => {
    const image = new Image(); image.src = url; await image.decode();
});

/** Subscribe before answering, so neither missing nor failed figures can be skipped. */
export function useStudyImagesReady(images?: CardImage[]) {
    const key = (images ?? []).map((image) => image.assetId).join(',');
    const ids = useMemo(() => key.split(',').filter(Boolean), [key]);
    const state = useSyncExternalStore(
        useCallback((fn: () => void) => { const cleanups = ids.map((id) => studyImageCache.subscribe(id, fn)); return () => cleanups.forEach((cleanup) => cleanup()); }, [ids]),
        () => ids.map((id) => studyImageCache.status(id)).join(','),
    );
    useEffect(() => { for (const id of key.split(',').filter(Boolean)) void studyImageCache.ensure(id); }, [key]);
    return state.split(',').every((status) => status === '' || status === 'ready');
}
