import { useEffect, useState } from 'react';
import { Button, CellList, CellSimple, Flex, IconButton, Spinner, Typography } from '@maxhub/max-ui';
import type { Card, CardSet, User } from '../types';
import { Screen } from '../components/Screen';
import { StatusScreen } from '../components/StatusScreen';
import { IconChevronLeft, IconOffline } from '../components/Icons';
import { loadCards, loadSet } from '../utils/sets';
import s from './SetCards.module.css';

type Status = 'loading' | 'error' | 'ready';

export interface SetCardsProps {
    setId: string;
    currentUser?: User | null;
    /** What happened to a card on the way back here. */
    toast?: { kind: 'removed' | 'edited' } | null;
    onBack: () => void;
    onOpenCard: (card: Card, setTitle: string) => void;
}

/** Cards of a set. The author opens one to edit or delete it; members only read. */
export function SetCards({ setId, currentUser, toast, onBack, onOpenCard }: SetCardsProps) {
    const [status, setStatus] = useState<Status>('loading');
    const [set, setSet] = useState<CardSet | null>(null);
    const [cards, setCards] = useState<Card[]>([]);
    const [attempt, setAttempt] = useState(0);

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
                title="Загружаем карточки"
                text="Это займёт пару секунд"
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
                            Назад
                        </Button>
                    </Flex>
                }
            />
        );
    }

    // Only the author may change the cards; members have read access.
    const isOwner = Boolean(currentUser && set.author && set.author.id === currentUser.id);

    return (
        <Screen>
            <Flex align="center" gap={8} className={s.header}>
                <IconButton size="small" variant="ghost" aria-label="Назад" onClick={onBack}>
                    <IconChevronLeft size={20} />
                </IconButton>
                <Flex direction="column" align="stretch" gap={2} className={s.headerText}>
                    <Typography.Text variant="subheader" asChild>
                        <h1 className={s.title}>Карточки · {cards.length}</h1>
                    </Typography.Text>
                    <Typography.Text variant="description" color="secondary" className={s.setTitle}>
                        {set.title}
                    </Typography.Text>
                </Flex>
            </Flex>

            <Flex direction="column" align="stretch" gap={8} className={s.body}>
                {isOwner ? (
                    <Typography.Text variant="description" color="tertiary" className={s.hint}>
                        Нажмите на карточку, чтобы исправить или удалить её
                    </Typography.Text>
                ) : null}
                <CellList mode="island" filled className={s.list}>
                    {cards.map((card, index) => (
                        <CellSimple
                            key={card.id}
                            as={isOwner ? 'button' : undefined}
                            separator={index > 0}
                            showChevron={isOwner}
                            onClick={isOwner ? () => onOpenCard(card, set.title) : undefined}
                            title={<span className={s.cardTitle}>{card.question}</span>}
                            subtitle={card.topic}
                        />
                    ))}
                </CellList>
            </Flex>

            {toast ? (
                <div className={s.notice} role="status">
                    <Typography.Text variant="detail">
                        {toast.kind === 'removed' ? 'Карточка удалена' : 'Карточка исправлена'}
                    </Typography.Text>
                </div>
            ) : null}
        </Screen>
    );
}

export default SetCards;
