import { useState } from 'react';
import { Button, Flex, Radio, Typography } from '@maxhub/max-ui';
import type { Card, CardIssueReason } from '../types';
import { Screen } from '../components/Screen';
import { ScreenHeader } from '../components/ScreenHeader';
import s from './CardIssue.module.css';

const REASONS: { value: CardIssueReason; label: string }[] = [
    { value: 'answer', label: 'Ответ не совпадает с конспектом' },
    { value: 'wording', label: 'Вопрос непонятно сформулирован' },
    { value: 'not-in-notes', label: 'Этого нет в моём конспекте' },
    { value: 'other', label: 'Другое' },
];

/** Each action saves the complaint first and rejects when it was not saved. */
export interface CardIssueProps {
    card: Card;
    setTitle: string;
    isOwner?: boolean;
    onBack: () => void;
    onEdit?: (reason: CardIssueReason) => Promise<void>;
    onRemove?: (reason: CardIssueReason) => Promise<void>;
    onReport?: (reason: CardIssueReason) => Promise<void>;
}

function answerOf(card: Card): string {
    if (typeof card.answer === 'boolean') {
        return card.answer ? 'Верно' : 'Неверно';
    }
    if (typeof card.answer === 'object' && card.answer !== null) {
        return 'Таблица';
    }
    return String(card.answer ?? '');
}

export function CardIssue({ card, setTitle, isOwner = false, onBack, onEdit, onRemove, onReport }: CardIssueProps) {
    const [reason, setReason] = useState<CardIssueReason>('answer');
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState('');

    const run = async (action?: (reason: CardIssueReason) => Promise<void>) => {
        if (!action || busy) {
            return;
        }
        setBusy(true);
        setError('');
        try {
            await action(reason);
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
                    <Typography.Text variant="body-strong" className={s.previewText}>
                        {card.question}
                    </Typography.Text>
                    <Typography.Text variant="description" color="secondary" className={s.previewText}>
                        Ответ: {answerOf(card)}
                    </Typography.Text>
                </Flex>

                <fieldset className={s.reasons}>
                    <legend className={s.legend}>Причина</legend>
                    {REASONS.map((item) => (
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

                <Typography.Text variant="description" color="secondary" className={s.note}>
                    {isOwner
                        ? 'Причина сохранится, даже если карточку исправить или удалить: по ней мы делаем карточки точнее.'
                        : 'Мы сохраним причину, чтобы делать карточки точнее. Эта карточка больше не попадётся в этой тренировке.'}
                </Typography.Text>
            </Flex>

            <Flex direction="column" align="stretch" gap={8}>
                {error && <Typography.Text role="alert">{error}</Typography.Text>}
                {isOwner ? (
                    <>
                        <Button size="medium" variant="primary" stretched disabled={busy} onClick={() => run(onEdit)}>
                            Исправить карточку
                        </Button>
                        <Button size="medium" variant="secondary" stretched disabled={busy} onClick={() => run(onRemove)}>
                            Удалить из набора
                        </Button>
                    </>
                ) : (
                    <Button size="medium" variant="primary" stretched disabled={busy} onClick={() => run(onReport)}>
                        Сообщить об ошибке
                    </Button>
                )}
            </Flex>
        </Screen>
    );
}

export default CardIssue;
