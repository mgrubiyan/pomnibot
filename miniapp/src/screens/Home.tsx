import { useEffect, useState } from 'react';
import {
    Avatar,
    Button,
    CellHeader,
    CellList,
    CellSimple,
    Counter,
    Flex,
    Spinner,
    Typography,
    type AvatarTextGradient,
} from '@maxhub/max-ui';
import type { CardSet, FeedCard, TodayData } from '../types';
import { api } from '../api';
import { Screen } from '../components/Screen';
import { StatusScreen } from '../components/StatusScreen';
import { IconCheck, IconDoc, IconOffline } from '../components/Icons';
import { estimateMinutes } from '../utils/estimate';
import { aboutMinutesLabel, cardsLabel, cardsWord } from '../utils/plural';
import { formatTodayDate } from '../utils/today';
import { formatAuthorName, formatGreetingName } from '../utils/user';
import { UserAvatar } from '../components/UserAvatar';
import { formatNextReviewSubtitle, isNextReviewToday } from '../utils/reviewDate';
import { cx } from '../utils/cx';
import { useMoreBelow } from '../utils/useMoreBelow';
import s from './Home.module.css';

type Status = 'loading' | 'error' | 'ready';

async function loadToday(): Promise<TodayData> {
    const { data, error } = await api.GET('/');
    if (error || !data) {
        throw new Error(error?.message ?? 'Failed to load today data');
    }
    return data;
}

async function loadFeed(): Promise<FeedCard[]> {
    const { data, error } = await api.GET('/feed');
    if (error || !data) {
        throw new Error(error?.message ?? 'Failed to load feed');
    }
    return data;
}


const avatarGradients: AvatarTextGradient[] = ['blue', 'green', 'purple', 'orange', 'red'];

/**
 * A set looks like a chat in MAX: the first letter of its title on one of the
 * avatar colors, the same for a set every time.
 */
function SetAvatar({ set }: { set: CardSet }) {
    const letter = (set.title.match(/[\p{L}\p{N}]/u)?.[0] ?? '·').toUpperCase();
    let hash = 0;
    for (const ch of set.id) {
        hash = (hash * 31 + ch.charCodeAt(0)) >>> 0;
    }
    return (
        <Avatar.Container size={44} form="squircle">
            <Avatar.Text gradient={avatarGradients[hash % avatarGradients.length]}>{letter}</Avatar.Text>
        </Avatar.Container>
    );
}

export interface HomeProps {
    /** «Начать» — a feed over every card that is due. */
    onStart: () => void;
    /** Tapping a set opens the set screen first; the feed starts there. */
    onOpenSet: (setId: string) => void;
    onAddNote: () => void;
    onJoinSet: () => void;
    /** The research behind the reviews, on its own screen. */
    onOpenWhy: () => void;
    /** Sets deleted during this session: no backend yet, App remembers them. */
    removedSetIds: string[];
    /** Notify parent when today data (including current user) is loaded */
    onTodayLoaded?: (today: TodayData) => void;
    /** Every complaint across all sets; the row is there for admins only. */
    onOpenAdminIssues?: () => void;
    /** The avatar in the corner opens the profile once it has loaded. */
    onOpenProfile?: () => void;
}

export function Home({ onStart, onOpenSet, onAddNote, onJoinSet, onOpenWhy, removedSetIds, onTodayLoaded, onOpenAdminIssues, onOpenProfile }: HomeProps) {
    const [status, setStatus] = useState<Status>('loading');
    const [today, setToday] = useState<TodayData | null>(null);
    const [feedCards, setFeedCards] = useState<FeedCard[]>([]);
    const [attempt, setAttempt] = useState(0);
    const [now, setNow] = useState(() => new Date());
    const [bodyRef, moreBelow] = useMoreBelow<HTMLDivElement>(status === 'ready');

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
                    // A refresh in the background keeps what is on screen.
                    setStatus((current) => (current === 'ready' ? current : 'error'));
                }
            });

        return () => {
            cancelled = true;
        };
    }, [attempt, onTodayLoaded]);

    // FSRS brings cards back within minutes, so the screen must not stay on
    // «Пока всё» once they are due: the countdown ticks while the next review
    // is today, and the data reloads when it comes or the app is reopened.
    const nextReviewAt = today?.nextReviewAt;
    useEffect(() => {
        const target = nextReviewAt ? new Date(nextReviewAt).getTime() : NaN;
        if (status !== 'ready' || isNaN(target)) {
            return;
        }
        const tick = () => {
            const current = new Date();
            setNow(current);
            if (current.getTime() >= target) {
                setAttempt((value) => value + 1);
            }
        };
        const onVisible = () => {
            if (document.visibilityState === 'visible') {
                tick();
            }
        };
        const timer = isNextReviewToday(nextReviewAt) ? window.setInterval(tick, 30_000) : undefined;
        document.addEventListener('visibilitychange', onVisible);
        return () => {
            window.clearInterval(timer);
            document.removeEventListener('visibilitychange', onVisible);
        };
    }, [status, nextReviewAt]);

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
    const nextReviewSubtitle = formatNextReviewSubtitle(today.nextReviewAt, now);

    const bottomActions = (
        <Flex direction="column" align="stretch" gap={8}>
            {/* With «Начать» on screen it is the main action of the day; without it
                this one is. */}
            <Button size="medium" variant={hasSets && dueCount > 0 ? 'secondary' : 'primary'} stretched onClick={onAddNote}>
                Создать набор
            </Button>
            <Button size="medium" variant="ghost" stretched onClick={onJoinSet}>
                Ввести код набора
            </Button>
        </Flex>
    );

    return (
        <Screen>
            <Flex justify="space-between" align="center" gap={12} className={s.header}>
                <Flex direction="column" align="flex-start" gap={2} className={s.headerText}>
                    <Typography.Text variant="description" color="secondary" asChild>
                        <h1 className={s.greeting}>Привет, {formatGreetingName(today.user)}</h1>
                    </Typography.Text>
                    <Typography.Text variant="description" color="tertiary" className={s.date}>
                        {formatTodayDate()}
                    </Typography.Text>
                </Flex>
                {onOpenProfile ? (
                    <button type="button" className={s.profileButton} onClick={onOpenProfile} aria-label="Профиль">
                        <UserAvatar user={today.user} size={36} />
                    </button>
                ) : null}
            </Flex>

            <Flex ref={bodyRef} direction="column" align="stretch" gap={20} className={cx(s.body, moreBelow && s.moreBelow)}>
                {hasSets ? (
                    <>
                        <Flex direction="column" align="stretch" gap={4} className={s.todayCard}>
                            {dueCount > 0 ? (
                                <>
                                    <Typography.Text variant="subheader">На сегодня</Typography.Text>
                                    <Flex align="baseline" gap={8} wrap="wrap">
                                        <Typography.Text variant="hero" className={s.dueNumber}>
                                            {dueCount}
                                        </Typography.Text>
                                        <Typography.Text variant="body" color="secondary">
                                            {cardsWord(dueCount)} · {aboutMinutesLabel(estimateMinutes(dueCount))}
                                        </Typography.Text>
                                    </Flex>
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
                                    <Flex align="center" gap={8}>
                                        <IconCheck size={24} tone="positive" />
                                        <Typography.Text variant="subheader">
                                            {/* «На сегодня всё» would be untrue while cards still come back today. */}
                                            {isNextReviewToday(today.nextReviewAt, now) ? 'Пока всё' : 'На сегодня всё'}
                                        </Typography.Text>
                                    </Flex>
                                    {nextReviewSubtitle ? (
                                        <Typography.Text variant="body" color="secondary">
                                            {nextReviewSubtitle}
                                        </Typography.Text>
                                    ) : null}
                                </>
                            )}
                        </Flex>

                        <Flex direction="column" align="stretch" gap={8} className={s.setsSection}>
                            <CellHeader titleStyle="normal">Мои наборы</CellHeader>
                            <CellList mode="island" filled className={s.setsList}>
                                {sets.map((set, index) => {
                                    // The feed is loaded by now; a set it has no cards of has nothing
                                    // due, whatever the set's own count says (it counts facts, not cards).
                                    const due = setDueMap.get(set.id) ?? 0;
                                    return (
                                        <CellSimple
                                            key={set.id}
                                            as="button"
                                            separator={index > 0}
                                            showChevron
                                            className={s.setRow}
                                            onClick={() => onOpenSet(set.id)}
                                            before={<SetAvatar set={set} />}
                                            innerClassNames={{ before: s.setAvatar }}
                                            after={due > 0 ? <Counter value={due} variant="primary" className={s.dueCounter} aria-label={`${cardsLabel(due)} на повтор`} /> : null}
                                            title={<span className={s.oneLine}>{set.title}</span>}
                                            subtitle={
                                                <span className={s.oneLine}>
                                                    {cardsLabel(set.cardsTotal)}
                                                    {set.author && set.author.id !== today.user.id
                                                        ? ` · ${formatAuthorName(set.author)}`
                                                        : ''}
                                                </span>
                                            }
                                        />
                                    );
                                })}
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

                {today.isAdmin && onOpenAdminIssues ? (
                    <CellList mode="island" filled className={s.setsList}>
                        <CellSimple
                            as="button"
                            showChevron
                            onClick={onOpenAdminIssues}
                            title="Все жалобы"
                            subtitle="Ошибки в карточках по всем наборам"
                        />
                    </CellList>
                ) : null}

                <button type="button" className={s.why} onClick={onOpenWhy}>
                    <Typography.Text variant="label" color="tertiary">
                        Карточка возвращается, когда вы начинаете её забывать: так запоминается
                        надолго.{' '}
                        <span className={s.whyMore}>Почему это работает</span>
                    </Typography.Text>
                </button>
            </Flex>

            {bottomActions}
        </Screen>
    );
}

export default Home;
