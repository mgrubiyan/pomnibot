import { useEffect, useState } from 'react';
import { Button, CellHeader, CellList, CellSimple, Flex, Spinner, Typography } from '@maxhub/max-ui';
import type { CardSet, TodayData } from '../types';
import { api } from '../api';
import { Screen } from '../components/Screen';
import { StatusScreen } from '../components/StatusScreen';
import { IconDoc, IconOffline } from '../components/Icons';
import { estimateMinutes } from '../utils/estimate';
import { aboutMinutesLabel, cardsLabel, daysLabel } from '../utils/plural';
import s from './Home.module.css';

type Status = 'loading' | 'error' | 'ready';

async function loadToday(): Promise<TodayData> {
    const { data, error } = await api.GET('/');
    if (error || !data) {
        throw new Error(error?.message ?? 'Failed to load today data');
    }
    return data;
}

/** «24 карточки · 8 на повтор», without the tail when nothing is due. */
function setSummary(set: CardSet): string {
    const total = cardsLabel(set.cardsTotal);
    return set.cardsDue > 0 ? `${total} · ${set.cardsDue} на повтор` : total;
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
}

export function Home({ onStart, onOpenSet, onAddNote, onJoinSet, removedSetIds }: HomeProps) {
    const [status, setStatus] = useState<Status>('loading');
    const [today, setToday] = useState<TodayData | null>(null);
    const [attempt, setAttempt] = useState(0);

    useEffect(() => {
        let cancelled = false;

        loadToday()
            .then((next) => {
                if (cancelled) {
                    return;
                }
                setToday(next);
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
    }, [attempt]);

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
    // The counters are summed over the remaining sets: otherwise «на сегодня»
    // would keep promising cards that a deletion has already taken away.
    const dueCount = sets.reduce((total, set) => total + set.cardsDue, 0);

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
                    <h1 className={s.greeting}>Привет, {today.userName}</h1>
                </Typography.Text>
                {/* «0 дней из 7» on the very first day reads as a reproach,
                    so the counter waits for the first session. */}
                {hasSets && today.activeDays > 0 ? (
                    <Typography.Text variant="label" color="secondary" className={s.days}>
                        Занимались {daysLabel(today.activeDays)} из 7
                    </Typography.Text>
                ) : null}
            </Flex>

            {hasSets ? (
                <>
                    <Flex direction="column" align="stretch" gap={4} className={s.todayCard}>
                        <Typography.Text variant="subheader">На сегодня</Typography.Text>

                        {dueCount > 0 ? (
                            <>
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
                            <Typography.Text variant="body" color="secondary">
                                На сегодня всё, вернёмся завтра
                            </Typography.Text>
                        )}
                    </Flex>

                    <Flex direction="column" align="stretch" gap={8} className={s.setsSection}>
                        <CellHeader>Мои наборы</CellHeader>
                        <div className={s.setsScroll}>
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
                                                    {setSummary(set)}
                                                </Typography.Text>
                                                {set.authorName ? (
                                                    <Typography.Text variant="label" color="tertiary">
                                                        Автор: {set.authorName}
                                                    </Typography.Text>
                                                ) : null}
                                            </Flex>
                                        }
                                    />
                                ))}
                            </CellList>
                        </div>
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

            {bottomActions}
        </Screen>
    );
}

export default Home;
