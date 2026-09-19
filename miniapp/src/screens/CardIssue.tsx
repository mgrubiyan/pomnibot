import { useState } from 'react';
import { Button, Flex, IconButton, Radio, Typography } from '@maxhub/max-ui';
import type { Card, CardIssueReason } from '../types';
import { Screen } from '../components/Screen';
import { IconChevronLeft } from '../components/Icons';
import s from './CardIssue.module.css';

const REASONS: { value: CardIssueReason; label: string }[] = [
    { value: 'answer', label: 'Ответ не совпадает с конспектом' },
    { value: 'wording', label: 'Вопрос непонятно сформулирован' },
    { value: 'not-in-notes', label: 'Этого нет в моём конспекте' },
    { value: 'other', label: 'Другое' },
];

export interface CardIssueProps {
    card: Card;
    setTitle: string;
    onBack: () => void;
    onEdit: (reason: CardIssueReason) => void;
    onRemove: (reason: CardIssueReason) => void;
}

export function CardIssue({ card, setTitle, onBack, onEdit, onRemove }: CardIssueProps) {
    const [reason, setReason] = useState<CardIssueReason>('answer');

    return (
        <Screen>
            <Flex align="center" gap={8} className={s.header}>
                <IconButton size="small" variant="ghost" aria-label="Назад" onClick={onBack}>
                    <IconChevronLeft size={20} />
                </IconButton>
                <Flex direction="column" align="stretch" gap={4}>
                    <Typography.Text variant="subheader" asChild>
                        <h1 className={s.title}>Что не так с карточкой?</h1>
                    </Typography.Text>
                    <Typography.Text variant="description" color="secondary">
                        {setTitle}
                    </Typography.Text>
                </Flex>
            </Flex>

            <Flex direction="column" align="stretch" gap={16} className={s.body}>
                <Flex direction="column" align="stretch" gap={4} className={s.preview}>
                    <Typography.Text variant="label" color="tertiary">
                        Карточка
                    </Typography.Text>
                    <Typography.Text variant="body-strong" className={s.previewText}>
                        {card.question}
                    </Typography.Text>
                    <Typography.Text variant="description" color="secondary" className={s.previewText}>
                        Ответ: {card.answer}
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
                    Карточку можно поправить сразу или убрать из набора. Причина поможет делать
                    карточки точнее.
                </Typography.Text>
            </Flex>

            <Flex direction="column" align="stretch" gap={8}>
                <Button size="medium" variant="primary" stretched onClick={() => onEdit(reason)}>
                    Исправить карточку
                </Button>
                <Button size="medium" variant="secondary" stretched onClick={() => onRemove(reason)}>
                    Удалить из набора
                </Button>
            </Flex>
        </Screen>
    );
}

export default CardIssue;
