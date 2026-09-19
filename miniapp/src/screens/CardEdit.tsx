import { useState } from 'react';
import { Button, Flex, IconButton, Input, Textarea, Typography } from '@maxhub/max-ui';
import type { Card } from '../types';
import { Screen } from '../components/Screen';
import { IconChevronLeft, IconCross, IconDoc, IconPlus, IconTrash } from '../components/Icons';
import type { CardPatch } from '../utils/cards';
import s from './CardEdit.module.css';

export interface CardEditProps {
    card: Card;
    setTitle: string;
    onBack: () => void;
    onSave: (patch: CardPatch) => void;
    onRemove: () => void;
}

export function CardEdit({ card, setTitle, onBack, onSave, onRemove }: CardEditProps) {
    const [question, setQuestion] = useState(card.question);
    const [answer, setAnswer] = useState(card.answer);
    // Неверные варианты — всё, кроме правильного ответа.
    const [wrongOptions, setWrongOptions] = useState<string[]>(
        (card.options ?? []).filter((option) => option !== card.answer),
    );

    // Варианты есть только у выбора из списка.
    const hasOptions = card.kind === 'choice';
    const canSave = question.trim().length > 0 && answer.trim().length > 0;

    const changeOption = (index: number, value: string) => {
        setWrongOptions((current) => current.map((item, i) => (i === index ? value : item)));
    };

    const removeOption = (index: number) => {
        setWrongOptions((current) => current.filter((_, i) => i !== index));
    };

    const save = () => {
        if (!canSave) {
            return;
        }
        const patch: CardPatch = {
            question: question.trim(),
            answer: answer.trim(),
        };
        if (hasOptions) {
            const cleaned = wrongOptions.map((option) => option.trim()).filter(Boolean);
            patch.options = [answer.trim(), ...cleaned];
        }
        onSave(patch);
    };

    return (
        <Screen>
            <Flex align="center" gap={8} className={s.header}>
                <IconButton size="small" variant="ghost" aria-label="Назад" onClick={onBack}>
                    <IconChevronLeft size={20} />
                </IconButton>
                <Flex direction="column" align="stretch" gap={2}>
                    <Typography.Text variant="subheader" asChild>
                        <h1 className={s.title}>Исправить карточку</h1>
                    </Typography.Text>
                    <Typography.Text variant="description" color="secondary">
                        {setTitle}
                    </Typography.Text>
                </Flex>
            </Flex>

            <Flex direction="column" align="stretch" gap={16} className={s.form}>
                <Flex direction="column" align="stretch" gap={4}>
                    <Typography.Text variant="label" color="tertiary" asChild>
                        <label htmlFor="card-question">Вопрос</label>
                    </Typography.Text>
                    <Textarea
                        id="card-question"
                        rows={2}
                        value={question}
                        className={s.field}
                        onChange={(event) => setQuestion(event.target.value)}
                    />
                </Flex>

                <Flex direction="column" align="stretch" gap={4}>
                    <Typography.Text variant="label" color="tertiary" asChild>
                        <label htmlFor="card-answer">Верный ответ</label>
                    </Typography.Text>
                    <Input
                        id="card-answer"
                        value={answer}
                        className={s.field}
                        onChange={(event) => setAnswer(event.target.value)}
                    />
                </Flex>

                {hasOptions ? (
                    <Flex direction="column" align="stretch" gap={8}>
                        <Typography.Text variant="label" color="tertiary">
                            Неверные варианты
                        </Typography.Text>

                        {wrongOptions.map((option, index) => (
                            <Flex key={index} align="center" gap={8} className={s.optionRow}>
                                <Input
                                    value={option}
                                    aria-label={`Неверный вариант ${index + 1}`}
                                    className={s.optionInput}
                                    onChange={(event) => changeOption(index, event.target.value)}
                                />
                                <IconButton
                                    size="small"
                                    variant="ghost"
                                    aria-label={`Убрать вариант «${option}»`}
                                    onClick={() => removeOption(index)}
                                >
                                    <IconCross size={20} tone="muted" />
                                </IconButton>
                            </Flex>
                        ))}

                        <Button
                            size="small"
                            variant="ghost"
                            iconBefore={<IconPlus size={20} />}
                            onClick={() => setWrongOptions((current) => [...current, ''])}
                        >
                            Добавить вариант
                        </Button>
                    </Flex>
                ) : null}

                <div className={s.divider} />

                <figure className={s.source}>
                    <Flex direction="column" align="stretch" gap={4}>
                        <Typography.Text variant="label-strong" color="tertiary" asChild>
                            <figcaption>
                                <Flex align="center" gap={4}>
                                    <IconDoc size={14} tone="muted" />
                                    Источник
                                    {card.sourceRef ? ` · ${card.sourceRef}` : ''}
                                </Flex>
                            </figcaption>
                        </Typography.Text>
                        <Typography.Text variant="description" color="tertiary" asChild>
                            <blockquote className={s.quote}>«{card.sourceQuote}»</blockquote>
                        </Typography.Text>
                        {/* TODO: выбор другого фрагмента конспекта — нужен экран
                            со списком фрагментов, макета и данных пока нет. */}
                        <button type="button" className={s.linkButton} disabled>
                            <Typography.Text variant="description">
                                Выбрать другой фрагмент конспекта
                            </Typography.Text>
                        </button>
                    </Flex>
                </figure>
            </Flex>

            <Flex direction="column" align="stretch" gap={8}>
                <Button size="medium" variant="primary" stretched disabled={!canSave} onClick={save}>
                    Сохранить
                </Button>
                <Button
                    size="small"
                    variant="ghost"
                    stretched
                    iconBefore={<IconTrash size={16} tone="muted" />}
                    onClick={onRemove}
                >
                    <Typography.Text variant="description" color="tertiary">
                        Удалить карточку
                    </Typography.Text>
                </Button>
            </Flex>
        </Screen>
    );
}

export default CardEdit;
