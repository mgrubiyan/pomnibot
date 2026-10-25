import { useEffect, useState } from 'react';
import { Button, CellList, CellSimple, Flex, Spinner, Typography } from '@maxhub/max-ui';
import type { Card, CardSet, FeedCard, User } from '../types';
import { api } from '../api';
import { Screen } from '../components/Screen';
import { ScreenHeader } from '../components/ScreenHeader';
import { StatusScreen } from '../components/StatusScreen';
import { IconCheck, IconOffline, IconShare, IconTrash } from '../components/Icons';
import { loadCards, loadSet, loadSetIssues } from '../utils/sets';
import { cardsLabel } from '../utils/plural';
import { formatAuthorName } from '../utils/user';
import { classifyError } from '../utils/errors';
import { cx } from '../utils/cx';
import { useMoreBelow } from '../utils/useMoreBelow';
import s from './SetScreen.module.css';

type Status = 'loading' | 'error' | 'ready';


async function loadFeed(setId?: string): Promise<FeedCard[]> {
    const { data, error } = await api.GET('/feed', {
        params: { query: { setId } },
    });
    if (error || !data) {
        throw new Error(error?.message ?? 'Failed to load feed');
    }
    return data;
}

export interface SetScreenProps {
    setId: string;
    currentUser?: User | null;
    onBack: () => void;
    onStart: (setId: string, setTitle?: string, early?: boolean) => void;
    onRemove: (setId: string) => void;
    /** The list of cards lives on its own screen, managed from there. */
    onOpenCards: (setId: string) => void;
    onShare: (setId: string, setTitle: string) => void;
    onOpenWarnings?: (setId: string) => void;
    /** Complaints about the cards; only the author gets there. */
    onOpenIssues?: (setId: string) => void;
    onOpenLeaderboard?: (setId: string, setTitle: string) => void;
    /** Where the members go wrong; only the author gets there. */
    onOpenMistakes?: (setId: string, setTitle: string) => void;
}

export function SetScreen({
    setId,
    currentUser,
    onBack,
    onStart,
    onRemove,
    onOpenCards,
    onShare,
    onOpenIssues,
    onOpenWarnings,
    onOpenLeaderboard,
    onOpenMistakes,
}: SetScreenProps) {
    const [status, setStatus] = useState<Status>('loading');
    const [set, setSet] = useState<CardSet | null>(null);
    const [cards, setCards] = useState<Card[]>([]);
    const [feedCards, setFeedCards] = useState<FeedCard[]>([]);
    const [loadError, setLoadError] = useState<unknown>(null);
    const [attempt, setAttempt] = useState(0);
    const [confirmingRemove, setConfirmingRemove] = useState(false);
    // Cards with open complaints. Only the author can see them, and the set
    // opens without them: a failed request leaves the row out.
    const [issueCards, setIssueCards] = useState(0);
    const [bodyRef, moreBelow] = useMoreBelow<HTMLDivElement>(status === 'ready');

    useEffect(() => {
        let cancelled = false;

        loadSet(setId)
            .then(async (nextSet) => {
                if (cancelled) return;
                setSet(nextSet);
                const owner = Boolean(currentUser && nextSet.author && nextSet.author.id === currentUser.id);
                const [nextCards, nextFeed] = await Promise.all([
                    owner ? loadCards(setId).catch(() => []) : Promise.resolve([]),
                    loadFeed(setId).catch(() => []),
                ]);
                if (cancelled) return;
                setCards(nextCards);
                setFeedCards(nextFeed);
                setStatus('ready');
            })
            .catch((err) => {
                if (!cancelled) {
                    setLoadError(err);
                    setStatus('error');
                }
            });

        return () => {
            cancelled = true;
        };
    }, [attempt, currentUser, setId]);

    // Only the author can manage the set and its cards; members have shared access.
    const isOwner = Boolean(currentUser && set?.author && set.author.id === currentUser.id);

    useEffect(() => {
        if (!isOwner) {
            return;
        }
        let cancelled = false;
        loadSetIssues(setId)
            .then((issues) => {
                if (!cancelled) {
                    setIssueCards(new Set(issues.map((issue) => issue.cardId)).size);
                }
            })
            .catch(() => {
                // The set screen works without the complaints.
            });
        return () => {
            cancelled = true;
        };
    }, [isOwner, setId]);

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
        const errorInfo = classifyError(loadError);
        return (
            <StatusScreen
                icon={errorInfo.kind === 'offline' ? <IconOffline size={48} tone="muted" /> : undefined}
                title={errorInfo.title}
                text={errorInfo.text}
                action={
                    <Flex direction="column" align="stretch" gap={8}>
                        {errorInfo.canRetry && (
                            <Button size="medium" variant="primary" stretched onClick={retry}>
                                Повторить
                            </Button>
                        )}
                        <Button size="medium" variant="ghost" stretched onClick={onBack}>
                            На главную
                        </Button>
                    </Flex>
                }
            />
        );
    }

    const totalCards = isOwner && cards.length > 0 ? cards.length : (set.cardsTotal ?? cards.length);
    const dueCount = feedCards.length;
    const hasDue = dueCount > 0;
    const shared = !isOwner;
    const showLeaderboard = isOwner && Boolean(onOpenLeaderboard);
    const showMistakes = isOwner && Boolean(onOpenMistakes);
    const showWarnings = isOwner && Boolean(set.warnings?.length) && Boolean(onOpenWarnings);
    const showIssues = isOwner && issueCards > 0 && Boolean(onOpenIssues);

    return (
        <Screen>
            <ScreenHeader
                title={set.title}
                subtitle={<>
                    {cardsLabel(totalCards)}
                    {hasDue ? ` · ${dueCount} на повтор` : ''}
                    {shared && set.author ? ` · автор: ${formatAuthorName(set.author)}` : ''}
                </>}
                onBack={onBack}
            />

            <Flex ref={bodyRef} direction="column" align="stretch" gap={16} className={cx(s.body, moreBelow && s.moreBelow)}>
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
                    {showIssues ? (
                        <CellSimple
                            as="button"
                            showChevron
                            onClick={() => onOpenIssues?.(set.id)}
                            title={`Жалобы · ${issueCards}`}
                            subtitle="Неточности, которые нашли в карточках"
                        />
                    ) : null}
                    {showWarnings ? (
                        <CellSimple
                            as="button"
                            showChevron
                            separator={showIssues}
                            onClick={() => onOpenWarnings?.(set.id)}
                            title={`Замечания · ${set.warnings?.length}`}
                            subtitle="Нюансы распознавания и структуры"
                        />
                    ) : null}
                    {showMistakes ? (
                        <CellSimple
                            as="button"
                            showChevron
                            separator={showIssues || showWarnings}
                            onClick={() => onOpenMistakes?.(set.id, set.title)}
                            title="Ошибки участников"
                            subtitle="На каких фактах ошибаются"
                        />
                    ) : null}
                    {showLeaderboard ? (
                        <CellSimple
                            as="button"
                            showChevron
                            separator={showIssues || showWarnings || showMistakes}
                            onClick={() => onOpenLeaderboard?.(set.id, set.title)}
                            title="Таблица лидеров"
                            subtitle="Рейтинг участников"
                        />
                    ) : null}
                    {isOwner ? (
                        <CellSimple
                            as="button"
                            showChevron
                            separator={showIssues || showWarnings || showLeaderboard || showMistakes}
                            onClick={() => onOpenCards(set.id)}
                            title={`Карточки · ${totalCards}`}
                            subtitle="Просмотр и правка"
                        />
                    ) : null}
                </CellList>
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
                            <Button size="medium" variant="primary" stretched onClick={() => onStart(set.id, set.title, false)}>
                                {/* The minutes do not fit a phone-wide button: «…около 3 ми…» */}
                                Повторить {cardsLabel(dueCount)}
                            </Button>
                        ) : (
                            <>
                                <Flex align="center" justify="center" gap={8} className={s.done}>
                                    <IconCheck size={20} tone="positive" />
                                    <Typography.Text variant="body" color="secondary">
                                        Все карточки повторены
                                    </Typography.Text>
                                </Flex>
                                {cards.length > 0 ? (
                                    <Button
                                        size="medium"
                                        variant="primary"
                                        stretched
                                        onClick={() => onStart(set.id, set.title, true)}
                                    >
                                        Повторить заранее
                                    </Button>
                                ) : null}
                            </>
                        )}

                        {/* With nothing left to review, sharing is what is left to do. */}
                        <Button
                            size="medium"
                            variant="secondary"
                            stretched
                            iconBefore={<IconShare size={20} />}
                            onClick={() => onShare(set.id, set.title)}
                        >
                            Поделиться
                        </Button>

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
