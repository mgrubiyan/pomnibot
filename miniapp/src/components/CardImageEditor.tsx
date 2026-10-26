import { useEffect, useState } from 'react';
import { Button, Typography } from '@maxhub/max-ui';
import { api } from '../api';
import type { CardImage } from '../types';
import { StudyImages } from './StudyImages';
import { moveImage } from '../utils/imageCache';

export function CardImageEditor({ setId, question, explanation, onQuestion, onExplanation }: {
    setId: string;
    question: CardImage[];
    explanation: CardImage[];
    onQuestion: (images: CardImage[]) => void;
    onExplanation: (images: CardImage[]) => void;
}) {
    const [catalog, setCatalog] = useState<CardImage[]>([]);
    const [status, setStatus] = useState('loading');
    const [attempt, setAttempt] = useState(0);
    const [picker, setPicker] = useState<'question' | 'explanation' | null>(null);
    useEffect(() => {
        let active = true;
        void api.GET('/sets/{setId}/images', { params: { path: { setId } } }).then(({ data, response }) => {
            if (!active) return;
            if (!response.ok || !data) { setStatus('error'); return; }
            setCatalog(data); setStatus('ready');
        }).catch(() => { if (active) setStatus('error'); });
        return () => { active = false; };
    }, [setId, attempt]);
    return <section aria-label="Изображения из PDF">
        {([{ side: 'question', title: 'Изображения вопроса', images: question, set: onQuestion, other: explanation, setOther: onExplanation },
            { side: 'explanation', title: 'Изображения объяснения', images: explanation, set: onExplanation, other: question, setOther: onQuestion }] as const).map(({ side, title, images, set, other, setOther }) => <div key={side}>
            <Typography.Text variant="label-strong" asChild><h3>{title}</h3></Typography.Text>
            {images.map((image, index) => <div key={image.assetId}>
                <StudyImages images={[image]} />
                <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8, margin: '8px 0 16px' }}>
                    <Button size="small" variant="secondary" type="button" aria-label={`Выше: ${image.alt}`} disabled={index === 0} onClick={() => set(moveImage(images, index, -1))}>↑</Button>
                    <Button size="small" variant="secondary" type="button" aria-label={`Ниже: ${image.alt}`} disabled={index === images.length - 1} onClick={() => set(moveImage(images, index, 1))}>↓</Button>
                    <Button size="small" variant="secondary" type="button" onClick={() => { set(images.filter((item) => item.assetId !== image.assetId)); if (!other.some((item) => item.assetId === image.assetId)) setOther([...other, image]); }}>В {side === 'question' ? 'объяснение' : 'вопрос'}</Button>
                    <Button size="small" variant="secondary" type="button" onClick={() => set(images.filter((item) => item.assetId !== image.assetId))}>Убрать</Button>
                </div>
            </div>)}
            <Button size="small" variant="secondary" type="button" onClick={() => setPicker(picker === side ? null : side)}>Выбрать из PDF</Button>
            {picker === side && <div>
                {status === 'loading' ? <p role="status">Загружаем каталог…</p> : status === 'error' ? <p role="alert">Не удалось загрузить каталог. <Button size="small" variant="secondary" type="button" onClick={() => { setStatus('loading'); setAttempt((value) => value + 1); }}>Повторить</Button></p> : catalog.length === 0 ? <p>В этом наборе нет изображений из PDF.</p> : catalog.filter((image) => !images.some((item) => item.assetId === image.assetId)).map((image) => <div key={image.assetId}>
                    <StudyImages images={[image]} />
                    <Button size="small" variant="secondary" type="button" onClick={() => { set([...images, image]); setPicker(null); }}>Добавить в {side === 'question' ? 'вопрос' : 'объяснение'}</Button>
                </div>)}
            </div>}
        </div>)}
    </section>;
}
