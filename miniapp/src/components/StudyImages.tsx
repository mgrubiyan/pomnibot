import { useCallback, useEffect, useRef, useState, useSyncExternalStore } from 'react';
import { Button } from '@maxhub/max-ui';
import type { CardImage } from '../types';
import { studyImageCache } from '../utils/useStudyImages';
import s from './StudyImages.module.css';

function ImageZoom({ image, url, onClose }: { image: CardImage; url: string; onClose: () => void }) {
    const ref = useRef<HTMLDialogElement>(null);
    const [zoom, setZoom] = useState(1);
    useEffect(() => { const dialog = ref.current; dialog?.showModal(); return () => dialog?.close(); }, []);
    return <dialog ref={ref} className={s.dialog} aria-label={image.alt || 'Изображение'} onClose={onClose} onClick={(event) => { event.stopPropagation(); if (event.target === event.currentTarget) onClose(); }}>
        <div className={s.toolbar}>
            <label>Масштаб <input type="range" min="1" max="4" step="0.1" value={zoom} onChange={(event) => setZoom(Number(event.target.value))} /></label>
            <Button size="small" variant="secondary" type="button" onClick={onClose} autoFocus>Закрыть</Button>
        </div>
        <div className={s.zoomScroll}><img src={url} alt={image.alt} style={{ width: `${zoom * 100}%`, maxWidth: 'none' }} /></div>
        <p>{image.sourceName} · стр. {image.sourcePage}</p>
    </dialog>;
}

function StudyImage({ image }: { image: CardImage }) {
    const [expanded, setExpanded] = useState(false);
    const state = useSyncExternalStore(useCallback((fn: () => void) => studyImageCache.subscribe(image.assetId, fn), [image.assetId]), () => studyImageCache.status(image.assetId));
    useEffect(() => { void studyImageCache.ensure(image.assetId); }, [image.assetId]);
    const url = studyImageCache.url(image.assetId);
    return <figure className={s.figure} onClick={(event) => event.stopPropagation()}>
        {state === 'ready' && url ? <button type="button" className={s.open} onClick={() => setExpanded(true)} aria-label={`Увеличить: ${image.alt}`}>
            <img src={url} alt={image.alt} width={image.width} height={image.height} />
        </button> : state === 'error' ? <div role="alert">Не удалось загрузить изображение. <Button size="small" variant="secondary" type="button" onClick={() => void studyImageCache.ensure(image.assetId, true)}>Повторить</Button></div> : <div role="status">Загружаем изображение…</div>}
        <figcaption>{image.sourceName} · стр. {image.sourcePage}</figcaption>
        {expanded && url && <ImageZoom image={image} url={url} onClose={() => setExpanded(false)} />}
    </figure>;
}

export function StudyImages({ images }: { images?: CardImage[] }) {
    return images?.length ? <div className={s.images}>{images.map((image) => <StudyImage key={image.assetId} image={image} />)}</div> : null;
}
