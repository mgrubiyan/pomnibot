import { useEffect, useState } from 'react';
import { Button, CellHeader, CellList, CellSimple, Flex, IconButton, Spinner, Typography } from '@maxhub/max-ui';
import type { LeaderboardEntry } from '../types';
import { api } from '../api';
import { Screen } from '../components/Screen';
import { StatusScreen } from '../components/StatusScreen';
import { IconChevronLeft, IconOffline } from '../components/Icons';
import { useBackButton } from '../max/useBackButton';
import { formatAuthorName } from '../utils/user';
import s from './Leaderboard.module.css';

type Status = 'loading' | 'error' | 'forbidden' | 'ready';

export interface LeaderboardProps {
    setId: string;
    setTitle?: string;
    onBack: () => void;
}

export function Leaderboard({ setId, setTitle, onBack }: LeaderboardProps) {
    const [status, setStatus] = useState<Status>('loading');
    const [items, setItems] = useState<LeaderboardEntry[]>([]);
    const [attempt, setAttempt] = useState(0);

    useBackButton(onBack);

    useEffect(() => {
        let cancelled = false;

        api.GET('/sets/{setId}/leaderboard', {
            params: { path: { setId } },
        })
            .then(({ data, response }) => {
                if (cancelled) {
                    return;
                }
                if (response.status === 403) {
                    setStatus('forbidden');
                    return;
                }
                if (!data) {
                    setStatus('error');
                    return;
                }
                setItems(data.items);
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
                title="Загрузка рейтинга"
                text="Получаем данные участников"
            />
        );
    }

    if (status === 'forbidden') {
        return (
            <StatusScreen
                icon={<IconOffline size={48} tone="muted" />}
                title="Доступ ограничен"
                text="Таблица лидеров доступна только автору набора"
                action={
                    <Button size="medium" variant="primary" stretched onClick={onBack}>
                        Назад
                    </Button>
                }
            />
        );
    }

    if (status === 'error') {
        return (
            <StatusScreen
                icon={<IconOffline size={48} tone="muted" />}
                title="Не удалось загрузить"
                text="Проверьте соединение и попробуйте снова"
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

    return (
        <Screen>
            <Flex align="center" gap={8} className={s.header}>
                <IconButton size="small" variant="ghost" aria-label="Назад" onClick={onBack}>
                    <IconChevronLeft size={20} />
                </IconButton>
                <Flex direction="column" align="stretch" gap={2} className={s.headerText}>
                    <Typography.Text variant="subheader" asChild>
                        <h1 className={s.title}>Таблица лидеров</h1>
                    </Typography.Text>
                    {setTitle ? (
                        <Typography.Text variant="description" color="secondary">
                            {setTitle}
                        </Typography.Text>
                    ) : null}
                </Flex>
            </Flex>

            <Flex direction="column" align="stretch" gap={16} className={s.body}>
                <Flex direction="column" align="stretch" gap={8}>
                    <CellHeader>Участники · {items.length}</CellHeader>
                    {items.length === 0 ? (
                        <div className={s.empty}>
                            <Typography.Text color="secondary">
                                Пока нет участников
                            </Typography.Text>
                        </div>
                    ) : (
                        <CellList mode="island" filled className={s.list}>
                            {items.map((item, index) => {
                                const rankClass =
                                    item.rank === 1
                                        ? s.rankTop1
                                        : item.rank === 2
                                          ? s.rankTop2
                                          : item.rank === 3
                                            ? s.rankTop3
                                            : '';
                                return (
                                    <CellSimple
                                        key={item.user.id}
                                        separator={index > 0}
                                        before={
                                            <div className={`${s.rankBadge} ${rankClass}`}>
                                                {item.rank}
                                            </div>
                                        }
                                        title={formatAuthorName(item.user)}
                                        after={
                                            <span className={s.percentileText}>
                                                {item.percentile}%
                                            </span>
                                        }
                                    />
                                );
                            })}
                        </CellList>
                    )}
                </Flex>
            </Flex>
        </Screen>
    );
}

export default Leaderboard;
