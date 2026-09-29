import { useEffect, useState } from 'react';
import { Button, Flex, IconButton, Spinner, Typography } from '@maxhub/max-ui';
import type { CardSet } from '../types';
import { api } from '../api';
import { Screen } from '../components/Screen';
import { StatusScreen } from '../components/StatusScreen';
import { IconCheck, IconChevronLeft, IconCopy, IconOffline } from '../components/Icons';
import { useBackButton } from '../max/useBackButton';
import { copyText } from '../utils/clipboard';
import { formatCode } from '../utils/code';
import { cardsLabel } from '../utils/plural';
import s from './Share.module.css';

type Status = 'loading' | 'error' | 'ready';
type CopyState = 'idle' | 'copied' | 'failed';

/** How long the button reports the outcome before it resets. */
const COPY_NOTICE_MS = 2000;

async function loadShare(setId: string): Promise<CardSet> {
    const { data, error } = await api.GET('/sets/{setId}/share', {
        params: {
            path: { setId },
        },
    });
    if (error || !data?.shareCode) {
        throw new Error(error?.message ?? 'no code');
    }
    return data;
}

/**
 * What lands in the group chat. An invite link needs the bot's address,
 * which the app does not know yet, so the message carries the code and
 * says where to type it in.
 */
const inviteText = (title: string, code: string) =>
    `«${title}» в Помниботе — код ${formatCode(code)}. ` +
    'Откройте Помнибот и нажмите «Ввести код набора».';

export interface ShareProps {
    setId: string;
    setTitle?: string;
    cardsCount?: number;
    onBack: () => void;
}

export function Share({ setId, setTitle: initialTitle, cardsCount: initialCardsCount, onBack }: ShareProps) {
    const [status, setStatus] = useState<Status>('loading');
    const [set, setSet] = useState<CardSet | null>(null);
    const [attempt, setAttempt] = useState(0);
    const [copyState, setCopyState] = useState<CopyState>('idle');

    useBackButton(onBack);

    const code = set?.shareCode ?? '';
    const title = set?.title ?? initialTitle ?? 'Набор';
    const cardsCount = set?.cardsTotal ?? initialCardsCount ?? 0;

    useEffect(() => {
        let cancelled = false;

        loadShare(setId)
            .then((next) => {
                if (cancelled) {
                    return;
                }
                setSet(next);
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

    useEffect(() => {
        if (copyState === 'idle') {
            return;
        }
        const timer = window.setTimeout(() => setCopyState('idle'), COPY_NOTICE_MS);

        return () => window.clearTimeout(timer);
    }, [copyState]);

    const retry = () => {
        setStatus('loading');
        setAttempt((current) => current + 1);
    };

    const copyInvite = () => {
        copyText(inviteText(title, code)).then((copied) =>
            setCopyState(copied ? 'copied' : 'failed'),
        );
    };

    if (status === 'loading') {
        return (
            <StatusScreen
                icon={<Spinner size={32} appearance="neutral-themed" />}
                title="Готовим код набора"
                text="Это займёт пару секунд"
            />
        );
    }

    if (status === 'error') {
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

    return (
        <Screen>
            <Flex align="center" className={s.header}>
                <IconButton size="small" variant="ghost" aria-label="Назад" onClick={onBack}>
                    <IconChevronLeft size={20} />
                </IconButton>
            </Flex>

            <Flex direction="column" justify="center" className={s.body}>
                <Flex direction="column" align="center" gap={8} className={s.codeCard}>
                    <Typography.Text variant="body" color="secondary">
                        Код набора
                    </Typography.Text>
                    <Typography.Text variant="hero" asChild>
                        <p className={s.code}>{formatCode(code)}</p>
                    </Typography.Text>
                    <Typography.Text variant="description" color="tertiary" className={s.meta}>
                        {title} · {cardsLabel(cardsCount)}
                    </Typography.Text>
                </Flex>
            </Flex>

            <Flex direction="column" align="stretch">
                <Button
                    size="medium"
                    variant="primary"
                    stretched
                    iconBefore={copyState === 'copied' ? <IconCheck size={20} /> : <IconCopy size={20} />}
                    onClick={copyInvite}
                >
                    {copyState === 'copied'
                        ? 'Скопировано'
                        : copyState === 'failed'
                          ? 'Не вышло — выделите код'
                          : 'Скопировать приглашение'}
                </Button>
            </Flex>
        </Screen>
    );
}

export default Share;
