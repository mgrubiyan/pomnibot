import { useEffect, useState } from 'react';
import { Button, Flex, Spinner, Typography } from '@maxhub/max-ui';
import type { Card, CardSet } from '../types';
import { api } from '../api';
import { Screen } from '../components/Screen';
import { ScreenHeader } from '../components/ScreenHeader';
import { StatusScreen } from '../components/StatusScreen';
import { IconCheck, IconOffline } from '../components/Icons';
import { loadCards, loadSet, loadSetIssues } from '../utils/sets';
import { answerPreview, cardsWithIssues, issueReasonLabel, type CardIssues } from '../utils/issues';
import s from './SetIssues.module.css';

type Status = 'loading' | 'error' | 'ready';

export interface SetIssuesProps {
    setId: string;
    onBack: () => void;
    onEditCard: (card: Card, setTitle: string) => void;
}

/** What readers found wrong in the author's cards. Fixing a card closes its
    complaints, and so does marking it correct. */
export function SetIssues({ setId, onBack, onEditCard }: SetIssuesProps) {
    const [status, setStatus] = useState<Status>('loading');
    const [set, setSet] = useState<CardSet | null>(null);
    const [items, setItems] = useState<CardIssues[]>([]);
    const [attempt, setAttempt] = useState(0);
    const [keeping, setKeeping] = useState<string | null>(null);
    const [error, setError] = useState('');

    useEffect(() => {
        let cancelled = false;

        Promise.all([loadSet(setId), loadCards(setId), loadSetIssues(setId)])
            .then(([nextSet, cards, issues]) => {
                if (cancelled) {
                    return;
                }
                setSet(nextSet);
                setItems(cardsWithIssues(cards, issues));
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

    const keep = async (cardId: string) => {
        setKeeping(cardId);
        setError('');
        try {
            const { response } = await api.POST('/cards/{cardId}/issue/resolve', {
                params: { path: { cardId } },
            });
            if (!response.ok) {
                throw new Error(`Failed to resolve card issues: ${response.status}`);
            }
            setItems((current) => current.filter((item) => item.card.id !== cardId));
        } catch {
            setError('Не удалось сохранить. Попробуйте ещё раз.');
        } finally {
            setKeeping(null);
        }
    };

    if (status === 'loading') {
        return (
            <StatusScreen
                icon={<Spinner size={32} appearance="neutral-themed" />}
                title="Загружаем жалобы"
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

    if (items.length === 0) {
        return (
            <StatusScreen
                icon={<IconCheck size={48} tone="positive" />}
                title="Жалоб нет"
                text="Когда кто-то отметит ошибку в карточке, она появится здесь"
                action={
                    <Button size="medium" variant="secondary" stretched onClick={onBack}>
                        К набору
                    </Button>
                }
            />
        );
    }

    return (
        <Screen>
            <ScreenHeader title={`Жалобы · ${items.length}`} subtitle={set.title} onBack={onBack} />

            <Flex direction="column" align="stretch" gap={12} className={s.body}>
                <Typography.Text variant="description" color="tertiary" className={s.hint}>
                    Исправленная карточка уходит из списка. Если ошибки нет, нажмите «Всё верно»
                </Typography.Text>
                {error && <Typography.Text role="alert" className={s.hint}>{error}</Typography.Text>}

                {items.map(({ card, reasons }) => (
                    <Flex key={card.id} direction="column" align="stretch" gap={12} className={s.card}>
                        <Flex direction="column" align="stretch" gap={4}>
                            <Typography.Text variant="body-strong" className={s.text}>
                                {card.question}
                            </Typography.Text>
                            <Typography.Text variant="description" color="secondary" className={s.text}>
                                Ответ: {answerPreview(card)}
                            </Typography.Text>
                        </Flex>
                        <ul className={s.reasons}>
                            {reasons.map(({ reason, count }) => (
                                <li key={reason}>
                                    <Typography.Text variant="description" color="secondary">
                                        {issueReasonLabel(reason)}
                                        {count > 1 ? ` · ${count}` : ''}
                                    </Typography.Text>
                                </li>
                            ))}
                        </ul>
                        <Flex gap={8} align="stretch">
                            <Button
                                size="small"
                                variant="secondary"
                                stretched
                                disabled={keeping !== null}
                                onClick={() => keep(card.id)}
                            >
                                Всё верно
                            </Button>
                            <Button
                                size="small"
                                variant="primary"
                                stretched
                                disabled={keeping !== null}
                                onClick={() => onEditCard(card, set.title)}
                            >
                                Исправить
                            </Button>
                        </Flex>
                    </Flex>
                ))}
            </Flex>
        </Screen>
    );
}

export default SetIssues;
