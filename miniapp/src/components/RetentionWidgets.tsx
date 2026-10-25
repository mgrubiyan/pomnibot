import { useEffect, useId, useState } from 'react';
import { Button } from '@maxhub/max-ui';
import { api } from '../api';
import type { SetRetention } from '../types';
import { retentionGraph, retentionPercent } from '../utils/retention';
import s from './RetentionWidgets.module.css';

export function RetentionWidgets({ setId }: { setId: string }) {
    const [data, setData] = useState<SetRetention | null>(null);
    const [error, setError] = useState(false);
    const [attempt, setAttempt] = useState(0);
    const [showAssumption, setShowAssumption] = useState(false);
    const assumptionId = useId();

    useEffect(() => {
        const controller = new AbortController();
        api.GET('/sets/{setId}/retention', {
            params: { path: { setId } }, signal: controller.signal,
        }).then(({ data, error }) => {
            if (controller.signal.aborted) return;
            if (error || !data) setError(true);
            else setData(data);
        }).catch(() => {
            if (!controller.signal.aborted) setError(true);
        });
        return () => controller.abort();
    }, [setId, attempt]);

    if (error) return (
        <section className={s.card} role="status">
            <p className={s.error}>Не удалось загрузить прогноз памяти</p>
            <Button size="small" variant="secondary" onClick={() => {
                setError(false);
                setAttempt((value) => value + 1);
            }}>Повторить загрузку</Button>
        </section>
    );

    if (!data) return (
        <div className={s.loading} role="status" aria-label="Считаем прогноз памяти" aria-busy="true">
            <div className={`${s.card} ${s.graphCard} ${s.graphSkeleton}`} aria-hidden="true">
                <div className={s.heading}>Следующие 13 дней</div>
                <div className={s.empty} />
                <div className={s.axis}><span>сегодня</span><span>через 13 дн</span></div>
                <div className={s.legend}>
                    <span><i className={s.reviewKey} />будешь повторять</span>
                    <span><i className={s.pauseKey} />сделаешь паузу</span>
                    <span><i className={s.targetKey} />цель</span>
                </div>
            </div>
            <div className={`${s.card} ${s.ringSkeleton}`} />
            <div className={`${s.card} ${s.groupsSkeleton}`} />
        </div>
    );

    const graph = retentionGraph(data);
    const empty = data.totalFacts === 0;
    const percent = (value: number) => empty ? '—' : retentionPercent(value);
    const circumference = 2 * Math.PI * 44;
    const groups = [data.groups.below70, data.groups.from70To90, data.groups.above90];
    return (
        <>
            <section className={`${s.card} ${s.graphCard}`} aria-label="Прогноз памяти">
                <h2 className={s.heading}>
                    <button type="button" className={s.headingButton} aria-expanded={showAssumption}
                        aria-controls={assumptionId} onClick={() => setShowAssumption((value) => !value)}>
                        Следующие {data.horizonDays} дней
                    </button>
                </h2>
                {showAssumption && <p id={assumptionId} className={s.assumption}>Прогноз при успешных ответах точно в срок</p>}
                {empty ? <div className={s.empty}>Нет фактов</div> : (
                    <svg viewBox="0 0 342 140" className={s.chart} role="img"
                        aria-label={`Через ${data.horizonDays} дней: с повторами ${percent(data.withReviews)}, с паузой ${percent(data.withoutReviews)}`}>
                        <line x1="0" y1={graph.targetY} x2="342" y2={graph.targetY} className={s.targetLine} />
                        <path d={graph.area} className={s.area} />
                        <path d={graph.pause} className={s.pauseLine} />
                        <path d={graph.reviews} className={s.reviewLine} />
                        <circle cx="0" cy={graph.startY} r="5" className={s.start} />
                        <text x="336" y={graph.reviewLabelY} textAnchor="end" className={s.reviewText}>с повторами: {percent(data.withReviews)}</text>
                        <text x="336" y={graph.pauseLabelY} textAnchor="end" className={s.pauseText}>пауза: {percent(data.withoutReviews)}</text>
                    </svg>
                )}
                <div className={s.axis}><span>сегодня</span><span>через {data.horizonDays} дн</span></div>
                <div className={s.legend} aria-hidden="true">
                    <span><i className={s.reviewKey} />будешь повторять</span>
                    <span><i className={s.pauseKey} />сделаешь паузу</span>
                    <span><i className={s.targetKey} />цель {retentionPercent(data.target)}</span>
                </div>
            </section>
            <section className={`${s.card} ${s.summary}`} aria-label="Текущая память">
                <div className={s.ring}>
                    <svg width="108" height="108" viewBox="0 0 108 108" aria-hidden="true">
                        <circle cx="54" cy="54" r="44" className={s.ringTrack} />
                        {!empty && data.current > 0 && <circle cx="54" cy="54" r="44" className={s.ringFill}
                            strokeDasharray={`${data.current * circumference} ${circumference}`} transform="rotate(-90 54 54)" />}
                    </svg>
                    <div className={s.ringLabel}>
                        <span className={s.current}>{percent(data.current)}</span>
                        <span className={s.remember}>ты помнишь</span>
                    </div>
                </div>
                <div className={s.comparison}>
                    <div>{empty ? 'Нет фактов' : `Через ${data.horizonDays} дн: с повторами ${percent(data.withReviews)}, с паузой ${percent(data.withoutReviews)}.`}</div>
                    <div className={s.targetBadge}>Цель: {retentionPercent(data.target)}</div>
                </div>
            </section>
            <section className={`${s.card} ${s.groups}`} aria-label="Группы фактов по вероятности вспоминания">
                <h2 className={s.heading}>Как помнятся факты</h2>
                <div className={s.bar} aria-hidden="true">
                    {groups.map((count, index) => count > 0 && <div key={index} className={s[`group${index}`]} style={{ flex: `${count} 1 0%` }} />)}
                </div>
                <div className={s.columns}>
                    {['ниже 70%', '70–90%', 'выше 90%'].map((label, index) => (
                        <div key={label}><strong>{groups[index]}</strong><span>{label}</span></div>
                    ))}
                </div>
            </section>
        </>
    );
}
