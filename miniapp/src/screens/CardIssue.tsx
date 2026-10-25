import { useState } from 'react';
import { Button, Flex, Radio, Typography } from '@maxhub/max-ui';
import type { Card, CardIssueReason } from '../types';
import { Screen } from '../components/Screen';
import { ScreenHeader } from '../components/ScreenHeader';
import { StudyContent } from '../components/StudyContent';
import { StudyIllustration } from '../components/StudyIllustration';
import { AutoTextarea } from '../components/AutoTextarea';
import { ISSUE_COMMENT_MAX_LENGTH, ISSUE_REASONS, answerPreview, type CardIssueReport } from '../utils/issues';
import s from './CardIssue.module.css';

/** Each action saves the complaint first and rejects when it was not saved. */
export interface CardIssueProps {
    card: Card;
    setTitle: string;
    isOwner?: boolean;
    onBack: () => void;
    onEdit?: (report: CardIssueReport) => Promise<void>;
    onRemove?: (report: CardIssueReport) => Promise<void>;
    onReport?: (report: CardIssueReport) => Promise<void>;
}

export function CardIssue({ card, setTitle, isOwner = false, onBack, onEdit, onRemove, onReport }: CardIssueProps) {
    const [reason, setReason] = useState<CardIssueReason>('answer');
    const [comment, setComment] = useState('');
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState('');
    // «Другое» tells the author nothing without the reader's own words.
    const needsComment = reason === 'other' && comment.trim() === '';

    const run = async (action?: (report: CardIssueReport) => Promise<void>) => {
        if (!action || busy || needsComment) {
            return;
        }
        setBusy(true);
        setError('');
        try {
            await action({ reason, comment: comment.trim() });
        } catch {
            setError('Не удалось отправить. Попробуйте ещё раз.');
            setBusy(false);
        }
    };

    return (
        <Screen>
            <ScreenHeader title="Что не так с карточкой?" subtitle={setTitle} onBack={onBack} />

            <Flex direction="column" align="stretch" gap={16} className={s.body}>
                <Flex direction="column" align="stretch" gap={4} className={s.preview}>
                    <Typography.Text variant="label" color="tertiary">
                        Карточка
                    </Typography.Text>
                    <Typography.Text variant="body-strong" asChild>
                        <StudyContent className={s.previewText} text={card.question} />
                    </Typography.Text>
                    <StudyIllustration svg={card.questionSvg} alt="Иллюстрация к вопросу" />
                    <Typography.Text variant="description" color="secondary" asChild>
                        <StudyContent className={s.previewText} text={`Ответ: ${answerPreview(card)}`} />
                    </Typography.Text>
                </Flex>

                <fieldset className={s.reasons}>
                    <legend className={s.legend}>Причина</legend>
                    {ISSUE_REASONS.map((item) => (
                        <label key={item.value} className={s.reason}>
                            <Radio
                                name="reason"
                                value={item.value}
                                checked={reason === item.value}
                                onChange={() => setReason(item.value)}
                            />
                            <Typography.Text variant="body">{item.label}</Typography.Text>
                        </label>
                    ))}
                </fieldset>

                <Flex direction="column" align="stretch" gap={4}>
                    <Typography.Text variant="label" color="tertiary" asChild>
                        <label htmlFor="card-issue-comment" className={s.label}>
                            {reason === 'other' ? 'Что не так' : 'Комментарий, если хотите'}
                        </label>
                    </Typography.Text>
                    <AutoTextarea
                        id="card-issue-comment"
                        rows={2}
                        value={comment}
                        maxLength={ISSUE_COMMENT_MAX_LENGTH}
                        className={s.comment}
                        placeholder={ISSUE_REASONS.find((item) => item.value === reason)?.hint}
                        onChange={(event) => setComment(event.target.value)}
                    />
                </Flex>

                <Typography.Text variant="description" color="secondary" className={s.note}>
                    {isOwner
                        ? 'Причина и комментарий сохранятся, даже если карточку исправить или удалить: по ним мы делаем карточки точнее.'
                        : 'Мы передадим причину и комментарий автору набора, без вашего имени. Пока автор не проверит карточку, она вам не попадётся.'}
                </Typography.Text>
            </Flex>

            <Flex direction="column" align="stretch" gap={8}>
                {error && <Typography.Text role="alert">{error}</Typography.Text>}
                {needsComment && !error && (
                    <Typography.Text variant="description" color="tertiary" className={s.note}>
                        Напишите, что не так, чтобы отправить
                    </Typography.Text>
                )}
                {isOwner ? (
                    <>
                        <Button size="medium" variant="primary" stretched disabled={busy || needsComment} onClick={() => run(onEdit)}>
                            Исправить карточку
                        </Button>
                        <Button size="medium" variant="secondary" stretched disabled={busy || needsComment} onClick={() => run(onRemove)}>
                            Удалить из набора
                        </Button>
                    </>
                ) : (
                    <Button size="medium" variant="primary" stretched disabled={busy || needsComment} onClick={() => run(onReport)}>
                        Сообщить об ошибке
                    </Button>
                )}
            </Flex>
        </Screen>
    );
}

export default CardIssue;
