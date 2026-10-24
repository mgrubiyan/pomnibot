import { useEffect, useState } from 'react';
import { Button, Flex, Spinner, Typography } from '@maxhub/max-ui';
import type { CardSet, User } from '../types';
import { Screen } from '../components/Screen';
import { ScreenHeader } from '../components/ScreenHeader';
import { StatusScreen } from '../components/StatusScreen';
import { IconAlert, IconCheck, IconOffline } from '../components/Icons';
import { loadSet } from '../utils/sets';
import s from './SetWarnings.module.css';

interface SetWarningsProps {
    setId: string;
    currentUser?: User | null;
    onBack: () => void;
}

export default function SetWarnings({ setId, currentUser, onBack }: SetWarningsProps) {
    const [status, setStatus] = useState<'loading' | 'error' | 'ready'>('loading');
    const [set, setSet] = useState<CardSet | null>(null);
    const [attempt, setAttempt] = useState(0);

    useEffect(() => {
        let cancelled = false;
        loadSet(setId)
            .then((nextSet) => {
                if (!cancelled) {
                    setSet(nextSet);
                    setStatus('ready');
                }
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

    if (status === 'loading') {
        return (
            <StatusScreen
                icon={<Spinner size={32} appearance="neutral-themed" />}
                title="Загружаем замечания"
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
                        <Button size="medium" variant="primary" stretched onClick={() => {
                            setStatus('loading');
                            setAttempt((value) => value + 1);
                        }}>
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

    const isOwner = Boolean(currentUser && set.author.id === currentUser.id);
    const warnings = isOwner ? set.warnings ?? [] : [];

    return (
        <Screen>
            <ScreenHeader title="Замечания" subtitle={set.title} onBack={onBack} />
            <Flex direction="column" align="stretch" gap={12} className={s.body}>
                {isOwner ? (
                    <>
                        <Typography.Text variant="description" color="tertiary" className={s.hint}>
                            Особенности распознавания исходных файлов и формирования карточек
                        </Typography.Text>
                        {warnings.length > 0 ? (
                            <ul className={s.list}>
                                {warnings.map((warning, index) => (
                                    <li key={index} className={s.card}>
                                        <IconAlert size={24} tone="muted" className={s.icon} />
                                        <Typography.Text variant="body" className={s.text}>
                                            {warning}
                                        </Typography.Text>
                                    </li>
                                ))}
                            </ul>
                        ) : (
                            <Flex align="center" gap={8} className={s.card}>
                                <IconCheck size={24} tone="positive" />
                                <Typography.Text>Замечаний нет</Typography.Text>
                            </Flex>
                        )}
                    </>
                ) : (
                    <Typography.Text color="secondary" className={s.hint}>
                        Замечания доступны только автору набора
                    </Typography.Text>
                )}
            </Flex>
        </Screen>
    );
}
