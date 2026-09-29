import { useEffect, useState } from 'react';
import { Button, CellList, CellSimple, Flex, IconButton, Spinner, Typography } from '@maxhub/max-ui';
import type { Card, CardSet, User } from '../types';
import { api } from '../api';
import { Screen } from '../components/Screen';
import { StatusScreen } from '../components/StatusScreen';
import { IconChevronLeft, IconOffline, IconShare, IconTrash } from '../components/Icons';
import { estimateMinutes } from '../utils/estimate';
import { loadCards, loadSet } from '../utils/sets';
import { aboutMinutesLabel, cardsLabel } from '../utils/plural';
import { formatAuthorName } from '../utils/user';
import s from './SetScreen.module.css';

type Status = 'loading' | 'error' | 'ready';


async function loadFeed(): Promise<Card[]> {
    const { data, error } = await api.GET('/feed');
    if (error || !data) {
        throw new Error(error?.message ?? 'Failed to load feed');
    }
    return data;
}

export interface SetScreenProps {
    setId: string;
    currentUser?: User | null;
    onBack: () => void;
    onStart: (setId: string, setTitle?: string) => void;
    onRemove: (setId: string) => void;
    /** The list of cards lives on its own screen, managed from there. */
    onOpenCards: (setId: string) => void;
    onShare: (setId: string, setTitle: string) => void;
    onOpenLeaderboard?: (setId: string, setTitle: string) => void;
}

export function SetScreen({
    setId,
    currentUser,
    onBack,
    onStart,
    onRemove,
    onOpenCards,
    onShare,
    onOpenLeaderboard,
}: SetScreenProps) {
    const [status, setStatus] = useState<Status>('loading');
    const [set, setSet] = useState<CardSet | null>(null);
    const [cards, setCards] = useState<Card[]>([]);
    const [feedCards, setFeedCards] = useState<Card[]>([]);
    const [attempt, setAttempt] = useState(0);
    const [confirmingRemove, setConfirmingRemove] = useState(false);

    useEffect(() => {
        let cancelled = false;

        Promise.all([loadSet(setId), loadCards(setId), loadFeed()])
            .then(([nextSet, nextCards, nextFeed]) => {
                if (cancelled) {
                    return;
                }
                setSet(nextSet);
                setCards(nextCards);
                setFeedCards(nextFeed.filter((c) => c.setId === setId));
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
    }, [attempt, setId]);

    const retry = () => {
        setStatus('loading');
        setAttempt((current) => current + 1);
    };

    if (status === 'loading') {
        return (
            <StatusScreen
                icon={<Spinner size={32} appearance="neutral-themed" />}
                title="Открываем набор"
                text="Смотрим, что нужно повторить"
            />
        );
    }

    if (status === 'error' || !set) {
        return (
            <StatusScreen
                icon={<IconOffline size={48} tone="muted" />}
                title="Нет соединения"
                text="Проверьте интернет и попробуйте ещё раз"
                action={
                    <Flex direction="column" align="stretch" gap={8}>
                        <Button size="medium" variant="primary" stretched onClick={retry}>
                            Повторить
                        </Button>
                        <Button size="medium" variant="ghost" stretched onClick={onBack}>
                            На главную
                        </Button>
                    </Flex>
                }
            />
        );
    }

    const dueCount = feedCards.length;
    const hasDue = dueCount > 0;
    // Only the author can manage the set and its cards; members have shared access.
    const isOwner = Boolean(currentUser && set.author && set.author.id === currentUser.id);
    const shared = !isOwner;
    const showLeaderboard = isOwner && Boolean(onOpenLeaderboard);

    return (
        <Screen>
            <Flex align="center" gap={8} className={s.header}>
                <IconButton size="small" variant="ghost" aria-label="Назад" onClick={onBack}>
                    <IconChevronLeft size={20} />
                </IconButton>
                <Flex direction="column" align="stretch" gap={2} className={s.headerText}>
                    <Typography.Text variant="subheader" asChild>
                        <h1 className={s.title}>{set.title}</h1>
                    </Typography.Text>
                    <Typography.Text variant="description" color="secondary">
                        {cardsLabel(cards.length)}
                        {hasDue ? ` · ${dueCount} на повтор` : ''}
                        {shared && set.author ? ` · автор: ${formatAuthorName(set.author)}` : ''}
                    </Typography.Text>
                </Flex>
            </Flex>

            <Flex direction="column" align="stretch" gap={16} className={s.body}>
                <Flex direction="column" align="stretch" gap={12} className={s.ratingCard}>
                    <Flex justify="space-between" align="baseline">
                        <Typography.Text variant="body" color="secondary">
                            Прогресс в наборе
                        </Typography.Text>
                        <Typography.Text variant="title">
                            {set.userPercentile ?? 0}%
                        </Typography.Text>
                    </Flex>
                    <div className={s.progressBarTrack}>
                        <div
                            className={s.progressBarFill}
                            style={{ width: `${Math.min(100, Math.max(0, set.userPercentile ?? 0))}%` }}
                        />
                    </div>
                    <Flex justify="space-between" align="center">
                        <Typography.Text variant="description" color="secondary">
                            Место в рейтинге
                        </Typography.Text>
                        <Typography.Text variant="label" color="primary">
                            {set.userRank ? `${set.userRank} место` : '—'}
                        </Typography.Text>
                    </Flex>
                </Flex>

                <CellList mode="island" filled className={s.factsList}>
                    {showLeaderboard ? (
                        <CellSimple
                            as="button"
                            showChevron
                            onClick={() => onOpenLeaderboard?.(set.id, set.title)}
                            title="Таблица лидеров"
                            subtitle="Рейтинг участников"
                        />
                    ) : null}
                    <CellSimple
                        as="button"
                        showChevron
                        separator={showLeaderboard}
                        onClick={() => onOpenCards(set.id)}
                        title={`Карточки · ${cards.length}`}
                        subtitle={isOwner ? 'Просмотр и правка' : 'Просмотр'}
                    />
                </CellList>

                <Button
                    size="medium"
                    variant="secondary"
                    stretched
                    iconBefore={<IconShare size={20} />}
                    onClick={() => onShare(set.id, set.title)}
                >
                    Поделиться
                </Button>

            </Flex>

            <Flex direction="column" align="stretch" gap={8}>
                {confirmingRemove ? (
                    <>
                        <Typography.Text variant="description" color="secondary" className={s.confirmText}>
                            {shared
                                ? 'Набор пропадёт из вашего списка. У автора он останется.'
                                : 'Набор и все его карточки удалятся. Вернуть их будет нельзя.'}
                        </Typography.Text>
                        <Flex gap={8} align="stretch">
                            <Button
                                size="medium"
                                variant="secondary"
                                stretched
                                onClick={() => setConfirmingRemove(false)}
                            >
                                Отмена
                            </Button>
                            <Button
                                size="medium"
                                variant="destructive"
                                stretched
                                onClick={() => onRemove(set.id)}
                            >
                                {shared ? 'Убрать' : 'Удалить'}
                            </Button>
                        </Flex>
                    </>
                ) : (
                    <>
                        {hasDue ? (
                            <Button size="medium" variant="primary" stretched onClick={() => onStart(set.id, set.title)}>
                                Повторить {cardsLabel(dueCount)} · {aboutMinutesLabel(estimateMinutes(dueCount))}
                            </Button>
                        ) : (
                            <Button size="medium" variant="secondary" stretched disabled>
                                Все карточки повторены
                            </Button>
                        )}

                        <Button
                            size="small"
                            variant="ghost"
                            stretched
                            iconBefore={<IconTrash size={16} tone="muted" />}
                            onClick={() => setConfirmingRemove(true)}
                        >
                            <Typography.Text variant="description" color="tertiary">
                                {shared ? 'Убрать набор' : 'Удалить набор'}
                            </Typography.Text>
                        </Button>
                    </>
                )}
            </Flex>
        </Screen>
    );
}

export default SetScreen;
