import { useEffect, useState } from 'react';
import { Button, CellHeader, CellList, CellSimple, Flex, IconButton, Spinner, Typography } from '@maxhub/max-ui';
import type { Card, CardSet, User } from '../types';
import { api } from '../api';
import { Screen } from '../components/Screen';
import { StatusScreen } from '../components/StatusScreen';
import { IconChevronLeft, IconOffline, IconTrash } from '../components/Icons';
import { estimateMinutes } from '../utils/estimate';
import { aboutMinutesLabel, cardsLabel } from '../utils/plural';
import { formatAuthorName } from '../utils/user';
import s from './SetScreen.module.css';

type Status = 'loading' | 'error' | 'ready';

interface Fact {
    title: string;
    text: string;
    source: string;
}

/** Screen copy rather than user data, so it belongs in the code. */
const FACTS: Fact[] = [
    {
        title: 'Вспоминать полезнее, чем перечитывать',
        text: 'Через неделю студенты, которые проверяли себя, вспомнили 61% текста, а те, кто перечитывал, — 40%.',
        source: 'Roediger, Karpicke · Psychological Science, 2006',
    },
    {
        title: 'Паузы важнее количества',
        text: 'Разнесённые по дням повторения запоминаются лучше, чем подряд. Чем дальше экзамен, тем длиннее могут быть паузы.',
        source: 'Cepeda и др. · обзор 317 экспериментов, 2006',
    },
    {
        title: 'Повтор — когда начинаете забывать',
        text: 'Карточка возвращается, когда вероятность её вспомнить падает примерно до 90%. Так каждое повторение укрепляет память сильнее.',
        source: 'Модель FSRS · Open Spaced Repetition',
    },
];

async function loadSet(setId: string): Promise<CardSet> {
    const { data, error } = await api.GET('/sets/{setId}', {
        params: { path: { setId } },
    });
    if (error || !data) {
        throw new Error(error?.message ?? 'not found');
    }
    return data;
}

async function loadCards(setId: string): Promise<Card[]> {
    const { data, error } = await api.GET('/sets/{setId}/cards', {
        params: { path: { setId } },
    });
    if (error || !data) {
        throw new Error(error?.message ?? 'not found');
    }
    return data;
}

export interface SetScreenProps {
    setId: string;
    /** Cards of the set, optional if fetched internally */
    cards?: Card[];
    /** Outcome of the last action on a card */
    toast?: { kind: 'removed' | 'edited' | 'reported' } | null;
    currentUser?: User | null;
    onBack: () => void;
    onStart: (setId: string) => void;
    onRemove: (setId: string) => void;
    onOpenCard: (card: Card, isOwner: boolean, setTitle: string) => void;
    onOpenLeaderboard?: (setId: string, setTitle: string) => void;
    onUndoRemoveCard?: () => void;
}

export function SetScreen({
    setId,
    cards: initialCards,
    toast,
    currentUser,
    onBack,
    onStart,
    onRemove,
    onOpenCard,
    onOpenLeaderboard,
    onUndoRemoveCard,
}: SetScreenProps) {
    const [status, setStatus] = useState<Status>('loading');
    const [set, setSet] = useState<CardSet | null>(null);
    const [cards, setCards] = useState<Card[]>(initialCards ?? []);
    const [attempt, setAttempt] = useState(0);
    const [confirmingRemove, setConfirmingRemove] = useState(false);

    useEffect(() => {
        let cancelled = false;

        Promise.all([loadSet(setId), loadCards(setId)])
            .then(([nextSet, nextCards]) => {
                if (cancelled) {
                    return;
                }
                setSet(nextSet);
                setCards(nextCards);
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

    const hasDue = set.cardsDue > 0;
    // Only the author can manage the set and its cards; members have shared access.
    const isOwner = Boolean(currentUser && set.author && set.author.id === currentUser.id);
    const shared = !isOwner;

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
                        {hasDue ? ` · ${set.cardsDue} на повтор` : ''}
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

                {isOwner && onOpenLeaderboard ? (
                    <CellList mode="island" filled className={s.factsList}>
                        <CellSimple
                            as="button"
                            showChevron
                            onClick={() => onOpenLeaderboard(set.id, set.title)}
                            title="Таблица лидеров"
                            subtitle="Рейтинг участников"
                        />
                    </CellList>
                ) : null}

                <Flex direction="column" align="stretch" gap={8}>
                    <Flex justify="space-between" align="baseline" gap={8} className={s.cardsHeader}>
                        <Typography.Text variant="label" color="tertiary">
                            Карточки · {cards.length}
                        </Typography.Text>
                    </Flex>
                    <CellList mode="island" filled className={s.factsList}>
                        {cards.map((card, index) => (
                            <CellSimple
                                key={card.id}
                                as="button"
                                separator={index > 0}
                                showChevron
                                onClick={() => onOpenCard(card, isOwner, set.title)}
                                title={<span className={s.cardTitle}>{card.question}</span>}
                                subtitle={card.topic}
                            />
                        ))}
                    </CellList>
                </Flex>

                <Flex direction="column" align="stretch" gap={8}>
                    <CellHeader>Почему это работает</CellHeader>
                    <CellList mode="island" filled className={s.factsList}>
                        {FACTS.map((fact, index) => (
                            <CellSimple
                                key={fact.title}
                                separator={index > 0}
                                title={fact.title}
                                subtitle={
                                    <Flex direction="column" align="stretch" gap={4}>
                                        <Typography.Text
                                            variant="description"
                                            color="secondary"
                                            className={s.factText}
                                        >
                                            {fact.text}
                                        </Typography.Text>
                                        <Typography.Text variant="label" color="tertiary">
                                            {fact.source}
                                        </Typography.Text>
                                    </Flex>
                                }
                            />
                        ))}
                    </CellList>
                </Flex>
            </Flex>

            <Flex direction="column" align="stretch" gap={8}>
                {toast ? (
                    <div className={s.undo} role="status">
                        <Typography.Text variant="detail">
                            {toast.kind === 'removed'
                                ? 'Карточка удалена'
                                : toast.kind === 'edited'
                                  ? 'Карточка исправлена'
                                  : 'Сообщение отправлено'}
                        </Typography.Text>
                        {toast.kind === 'removed' && onUndoRemoveCard ? (
                            <button type="button" className={s.undoButton} onClick={onUndoRemoveCard}>
                                <Typography.Text variant="detail">Вернуть</Typography.Text>
                            </button>
                        ) : null}
                    </div>
                ) : null}

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
                            <Button size="medium" variant="primary" stretched onClick={() => onStart(set.id)}>
                                Повторить {cardsLabel(set.cardsDue)} · {aboutMinutesLabel(estimateMinutes(set.cardsDue))}
                            </Button>
                        ) : (
                            // The mockup has no such case: when nothing is due,
                            // we offer to go through the whole set.
                            <Button size="medium" variant="secondary" stretched onClick={() => onStart(set.id)}>
                                Пройти набор целиком
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
