import { useEffect, useState } from 'react';
import { Button, CellHeader, CellList, CellSimple, Flex, Spinner, Typography } from '@maxhub/max-ui';
import type { Card, CardSet, TodayData } from '../types';
import { api } from '../api';
import { Screen } from '../components/Screen';
import { StatusScreen } from '../components/StatusScreen';
import { WhyItWorks } from '../components/WhyItWorks';
import { IconDoc, IconOffline } from '../components/Icons';
import { estimateMinutes } from '../utils/estimate';
import { aboutMinutesLabel, cardsLabel } from '../utils/plural';
import { formatAuthorName, formatGreetingName } from '../utils/user';
import { formatNextReviewSubtitle } from '../utils/reviewDate';
import s from './Home.module.css';

type Status = 'loading' | 'error' | 'ready';

async function loadToday(): Promise<TodayData> {
    const { data, error } = await api.GET('/');
    if (error || !data) {
        throw new Error(error?.message ?? 'Failed to load today data');
    }
    return data;
}

async function loadFeed(): Promise<Card[]> {
    const { data, error } = await api.GET('/feed');
    if (error || !data) {
        throw new Error(error?.message ?? 'Failed to load feed');
    }
    return data;
}

/** «24 карточки · 8 на повтор», without the tail when nothing is due. */
function setSummary(set: CardSet, feedDueCount?: number): string {
    const total = cardsLabel(set.cardsTotal);
    const due = feedDueCount !== undefined ? feedDueCount : set.cardsDue;
    return due > 0 ? `${total} · ${due} на повтор` : total;
}

export interface HomeProps {
    /** «Начать» — a feed over every card that is due. */
    onStart: () => void;
    /** Tapping a set opens the set screen first; the feed starts there. */
    onOpenSet: (setId: string) => void;
    onAddNote: () => void;
    onJoinSet: () => void;
    /** Sets deleted during this session: no backend yet, App remembers them. */
    removedSetIds: string[];
    /** Notify parent when today data (including current user) is loaded */
    onTodayLoaded?: (today: TodayData) => void;
}

export function Home({ onStart, onOpenSet, onAddNote, onJoinSet, removedSetIds, onTodayLoaded }: HomeProps) {
    const [status, setStatus] = useState<Status>('loading');
    const [today, setToday] = useState<TodayData | null>(null);
    const [feedCards, setFeedCards] = useState<Card[]>([]);
    const [attempt, setAttempt] = useState(0);

    useEffect(() => {
        let cancelled = false;

        Promise.all([loadToday(), loadFeed()])
            .then(([nextToday, nextFeed]) => {
                if (cancelled) {
                    return;
                }
                setToday(nextToday);
                setFeedCards(nextFeed);
                onTodayLoaded?.(nextToday);
                setStatus('ready');
            })
            .catch(() => {
                if (!cancelled) {
                    setStatus('error');
                }
            });

        return () => {
            cancelled = true;
        };
    }, [attempt, onTodayLoaded]);

    const retry = () => {
        setStatus('loading');
        setAttempt((current) => current + 1);
    };

    if (status === 'loading') {
        return (
            <StatusScreen
                icon={<Spinner size={32} appearance="neutral-themed" />}
                title="Смотрим, что на сегодня"
                text="Собираем ваши наборы"
            />
        );
    }

    if (status === 'error' || !today) {
        return (
            <StatusScreen
                icon={<IconOffline size={48} tone="muted" />}
                title="Нет соединения"
                text="Проверьте интернет и попробуйте ещё раз"
                action={
                    <Button size="medium" variant="primary" stretched onClick={retry}>
                        Повторить
                    </Button>
                }
            />
        );
    }

    const sets = today.sets.filter((set) => !removedSetIds.includes(set.id));
    const hasSets = sets.length > 0;
    const setDueMap = new Map<string, number>();
    for (const card of feedCards) {
        setDueMap.set(card.setId, (setDueMap.get(card.setId) ?? 0) + 1);
    }
    const dueCount = feedCards.filter((card) => !removedSetIds.includes(card.setId)).length;
    const nextReviewSubtitle = formatNextReviewSubtitle(today.nextReviewAt);

    const bottomActions = (
        <Flex direction="column" align="stretch" gap={8}>
            <Button size="medium" variant="primary" stretched onClick={onAddNote}>
                Добавить конспект
            </Button>
            <Button size="medium" variant="ghost" stretched onClick={onJoinSet}>
                Ввести код набора
            </Button>
        </Flex>
    );

    return (
        <Screen>
            <Flex justify="space-between" align="baseline" gap={12} className={s.header}>
                <Typography.Text variant="subheader" asChild>
                    <h1 className={s.greeting}>Привет, {formatGreetingName(today.user)}</h1>
                </Typography.Text>
            </Flex>

            <Flex direction="column" align="stretch" gap={16} className={s.body}>
                {hasSets ? (
                    <>
                        <Flex direction="column" align="stretch" gap={4} className={s.todayCard}>
                            {dueCount > 0 ? (
                                <>
                                    <Typography.Text variant="subheader">На сегодня</Typography.Text>
                                    <Typography.Text variant="body" color="secondary">
                                        {cardsLabel(dueCount)} · {aboutMinutesLabel(estimateMinutes(dueCount))}
                                    </Typography.Text>
                                    <Button
                                        size="medium"
                                        variant="primary"
                                        stretched
                                        className={s.startButton}
                                        onClick={onStart}
                                    >
                                        Начать
                                    </Button>
                                </>
                            ) : (
                                <>
                                    <Typography.Text variant="subheader">На сегодня всё</Typography.Text>
                                    {nextReviewSubtitle ? (
                                        <Typography.Text variant="body" color="secondary">
                                            {nextReviewSubtitle}
                                        </Typography.Text>
                                    ) : null}
                                </>
                            )}
                        </Flex>

                        <Flex direction="column" align="stretch" gap={8} className={s.setsSection}>
                            <CellHeader>Мои наборы</CellHeader>
                            <CellList mode="island" filled className={s.setsList}>
                                {sets.map((set, index) => (
                                    <CellSimple
                                        key={set.id}
                                        as="button"
                                        separator={index > 0}
                                        showChevron
                                        onClick={() => onOpenSet(set.id)}
                                        title={<span className={s.setTitle}>{set.title}</span>}
                                        subtitle={
                                            <Flex direction="column" align="stretch">
                                                <Typography.Text variant="description" color="secondary">
                                                    {setSummary(set, setDueMap.get(set.id))}
                                                </Typography.Text>
                                                {set.author && set.author.id !== today.user.id ? (
                                                    <Typography.Text variant="label" color="tertiary">
                                                        Автор: {formatAuthorName(set.author)}
                                                    </Typography.Text>
                                                ) : null}
                                            </Flex>
                                        }
                                    />
                                ))}
                            </CellList>
                        </Flex>
                    </>
                ) : (
                    <Flex direction="column" align="center" justify="center" gap={12} className={s.empty}>
                        <IconDoc size={48} tone="muted" />
                        <Typography.Text variant="body" color="secondary" className={s.emptyText}>
                            Загрузите конспект, и мы сделаем из него карточки
                        </Typography.Text>
                    </Flex>
                )}

                <WhyItWorks />
            </Flex>

            {bottomActions}
        </Screen>
    );
}

export default Home;
