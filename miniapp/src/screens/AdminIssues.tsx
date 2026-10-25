import { useEffect, useState } from 'react';
import { Button, Flex, Spinner, Typography } from '@maxhub/max-ui';
import type { AdminCardIssue, CardIssueReason } from '../types';
import { api } from '../api';
import { Screen } from '../components/Screen';
import { ScreenHeader } from '../components/ScreenHeader';
import { StatusScreen } from '../components/StatusScreen';
import { IconCheck, IconOffline } from '../components/Icons';
import { REASON_FILTERS, STATUS_LABELS, adminAnswer, issuesTotal, reasonCounts } from '../utils/adminIssues';
import { issueReasonLabel } from '../utils/issues';
import { formatAuthorName } from '../utils/user';
import { StudyContent } from '../components/StudyContent';
import s from './AdminIssues.module.css';

type Status = 'loading' | 'error' | 'ready';

export interface AdminIssuesProps {
    onBack: () => void;
}

const reportedAt = (iso: string) =>
    new Date(iso).toLocaleString('ru-RU', { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' });

/** Every complaint across all sets, with the copy of the card as it was
    reported and what became of it. Only admins get here. */
export function AdminIssues({ onBack }: AdminIssuesProps) {
    const [status, setStatus] = useState<Status>('loading');
    const [issues, setIssues] = useState<AdminCardIssue[]>([]);
    const [reason, setReason] = useState<CardIssueReason | null>(null);
    const [attempt, setAttempt] = useState(0);

    useEffect(() => {
        let cancelled = false;
        api.GET('/admin/issues')
            .then(({ data, response }) => {
                if (cancelled) {
                    return;
                }
                if (!response.ok || !data) {
                    setStatus('error');
                    return;
                }
                setIssues(data);
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
                title="Загружаем жалобы"
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

    if (issues.length === 0) {
        return (
            <StatusScreen
                icon={<IconCheck size={48} tone="positive" />}
                title="Жалоб нет"
                text="Когда кто-то отметит ошибку в карточке, она появится здесь"
                action={
                    <Button size="medium" variant="secondary" stretched onClick={onBack}>
                        На главную
                    </Button>
                }
            />
        );
    }

    const shown = reason ? issues.filter((issue) => issue.reason === reason) : issues;

    return (
        <Screen>
            <ScreenHeader title="Все жалобы" subtitle={issuesTotal(issues.length)} onBack={onBack} />

            <Flex direction="column" align="stretch" gap={12} className={s.body}>
                <Flex gap={8} wrap="wrap" className={s.filters}>
                    <Button size="small" variant={reason === null ? 'primary' : 'secondary'} onClick={() => setReason(null)}>
                        Все · {issues.length}
                    </Button>
                    {reasonCounts(issues).map((item) => (
                        <Button
                            key={item.reason}
                            size="small"
                            variant={reason === item.reason ? 'primary' : 'secondary'}
                            onClick={() => setReason(item.reason)}
                        >
                            {REASON_FILTERS[item.reason]} · {item.count}
                        </Button>
                    ))}
                </Flex>

                {shown.map((issue) => {
                    const answer = adminAnswer(issue);
                    return (
                        <Flex key={issue.id} direction="column" align="stretch" gap={8} className={s.issue}>
                            <Typography.Text variant="label" color="tertiary" className={s.text}>
                                {issue.setTitle ?? 'Набор удалён'} · {STATUS_LABELS[issue.status]}
                            </Typography.Text>
                            <Typography.Text variant="body-strong" asChild>
                                <StudyContent className={s.text} text={issue.question} />
                            </Typography.Text>
                            {answer ? (
                                <Typography.Text variant="description" color="secondary" asChild>
                                    <StudyContent className={s.text} text={`Ответ: ${answer}`} />
                                </Typography.Text>
                            ) : null}
                            {issue.sourceQuote ? (
                                <Typography.Text variant="description" color="tertiary" asChild>
                                    <blockquote className={s.quote}>{issue.sourceQuote}</blockquote>
                                </Typography.Text>
                            ) : null}
                            <Flex direction="column" gap={4} className={s.footer}>
                                <Typography.Text variant="description">{issueReasonLabel(issue.reason)}</Typography.Text>
                                {issue.comment ? (
                                    <Typography.Text variant="description" color="secondary" className={s.comment}>
                                        {issue.comment}
                                    </Typography.Text>
                                ) : null}
                                <Typography.Text variant="label" color="tertiary" className={s.text}>
                                    {formatAuthorName(issue.reporter)} · {reportedAt(issue.createdAt)}
                                </Typography.Text>
                            </Flex>
                        </Flex>
                    );
                })}
            </Flex>
        </Screen>
    );
}

export default AdminIssues;
