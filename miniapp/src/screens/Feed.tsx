import { useCallback, useEffect, useState } from 'react';
import { Button, Flex, Input, Spinner, Typography } from '@maxhub/max-ui';
import type { Card } from '../types';
import { mockCards } from '../mocks';
import { TableColumns, TablePool } from '../components/TableCard';
import { visibleCards, type CardPatch } from '../utils/cards';
import {
    correctCount,
    emptyPlacement,
    placedCount,
    type Placement,
} from '../utils/table';
import { Screen } from '../components/Screen';
import { StatusScreen } from '../components/StatusScreen';
import { cx } from '../utils/cx';
import {
    IconCheck,
    IconCross,
    IconDoc,
    IconFlag,
    IconMic,
    IconOffline,
    IconStop,
} from '../components/Icons';
import s from './Feed.module.css';

type Status = 'loading' | 'error' | 'ready';
type Verdict = 'correct' | 'wrong';

interface Option {
    label: string;
    value: string;
}

const BOOLEAN_OPTIONS: Option[] = [
    { label: 'Верно', value: 'true' },
    { label: 'Неверно', value: 'false' },
];

const LOAD_DELAY = 700;
const VOICE_DELAY = 1500;
const VOICE_SUBMIT_DELAY = 600;

/** Сравниваем ответы мягко: регистр, лишние пробелы и ё роли не играют. */
const norm = (value: string) => value.trim().toLowerCase().replace(/ё/g, 'е').replace(/\s+/g, ' ');

function optionsOf(card: Card): Option[] {
    if (card.kind === 'boolean') {
        return BOOLEAN_OPTIONS;
    }
    if (card.kind === 'choice') {
        return (card.options ?? []).map((option) => ({ label: option, value: option }));
    }
    return [];
}

/** У boolean в данных лежит 'true' / 'false', показывать это нельзя. */
function labelOf(card: Card, value: string): string {
    if (card.kind !== 'boolean') {
        return value;
    }
    return value === 'true' ? 'Верно' : 'Неверно';
}

/**
 * Моки вместо запроса. Реальный эндпоинт подключим позже.
 * Экран ошибки — ?fail, одна карточка по виду или id — ?card=table, ?card=c9
 */
function loadCards(setId?: string): Promise<Card[]> {
    return new Promise((resolve, reject) => {
        window.setTimeout(() => {
            const params = new URLSearchParams(window.location.search);

            if (params.has('fail')) {
                reject(new Error('network'));
                return;
            }

            let next = setId ? mockCards.filter((card) => card.setId === setId) : mockCards;

            const only = params.get('card');
            if (only) {
                next = next.filter((card) => card.id === only || card.kind === only);
            }

            resolve(next);
        }, LOAD_DELAY);
    });
}

/** Заглушка распознавания: движок не подключён, «слышим» верный ответ. */
function mockTranscript(card: Card): string {
    if (card.kind === 'boolean') {
        return card.answer === 'true' ? 'верно' : 'неверно';
    }
    if (card.kind === 'choice') {
        return card.answer.split(' ').slice(0, 4).join(' ').toLowerCase();
    }
    return card.answer.toLowerCase();
}

/** Распознанную фразу приводим к варианту ответа, если он нашёлся. */
function matchTranscript(card: Card, phrase: string): string {
    const options = optionsOf(card);
    if (options.length === 0) {
        return phrase;
    }
    const hit = options.find((option) => norm(option.label).startsWith(norm(phrase)));
    return hit ? hit.value : phrase;
}

export interface FeedProps {
    /** Если задан — проходим только карточки этого набора. */
    setId?: string;
    /** Удалённые и поправленные в этой сессии карточки. */
    removedCardIds: string[];
    cardPatches: Record<string, CardPatch>;
    onExit: () => void;
    onReportCard: (cardId: string) => void;
}

export function Feed({ setId, removedCardIds, cardPatches, onExit, onReportCard }: FeedProps) {
    const [status, setStatus] = useState<Status>('loading');
    const [cards, setCards] = useState<Card[]>([]);
    const [index, setIndex] = useState(0);

    const [given, setGiven] = useState<string | null>(null);
    const [verdict, setVerdict] = useState<Verdict | null>(null);
    const [revealed, setRevealed] = useState(false);
    const [draft, setDraft] = useState('');
    const [listening, setListening] = useState(false);
    const [transcript, setTranscript] = useState('');
    // Раскладку храним по id карточки, а не эффектом на смену карточки:
    // так она заводится сама и не требует setState в эффекте.
    const [placements, setPlacements] = useState<Record<string, Placement>>({});
    const [picked, setPicked] = useState<{ cardId: string; index: number } | null>(null);

    const [attempt, setAttempt] = useState(0);

    const card = cards[index];

    // Перезапуск загрузки — через счётчик попыток: статус переключает
    // обработчик кнопки, эффект только ходит за данными.
    useEffect(() => {
        let cancelled = false;

        loadCards(setId)
            .then((next) => {
                if (cancelled) {
                    return;
                }
                setCards(visibleCards(next, undefined, removedCardIds, cardPatches));
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
    }, [attempt, setId, removedCardIds, cardPatches]);

    const retry = () => {
        setStatus('loading');
        setAttempt((current) => current + 1);
    };

    const submit = useCallback(
        (value: string) => {
            if (!card) {
                return;
            }
            setGiven(value);
            setVerdict(norm(value) === norm(card.answer) ? 'correct' : 'wrong');
        },
        [card],
    );

    // Заглушка голосового ответа: пауза «слушаю», потом фраза, потом ответ.
    useEffect(() => {
        if (!listening || !card) {
            return;
        }
        const phrase = mockTranscript(card);
        const showPhrase = window.setTimeout(() => setTranscript(phrase), VOICE_DELAY);
        const answer = window.setTimeout(() => {
            setListening(false);
            submit(matchTranscript(card, phrase));
        }, VOICE_DELAY + VOICE_SUBMIT_DELAY);

        return () => {
            window.clearTimeout(showPhrase);
            window.clearTimeout(answer);
        };
    }, [listening, card, submit]);

    const reset = () => {
        setGiven(null);
        setVerdict(null);
        setRevealed(false);
        setDraft('');
        setListening(false);
        setTranscript('');
        setPicked(null);
    };

    const layout = card?.table;
    const placement: Placement =
        layout && card ? (placements[card.id] ?? emptyPlacement(layout)) : [];
    const pickedIndex = picked && card && picked.cardId === card.id ? picked.index : null;

    const putInColumn = (column: string) => {
        if (!card || pickedIndex === null) {
            return;
        }
        const next = [...placement];
        next[pickedIndex] = column;
        setPlacements((current) => ({ ...current, [card.id]: next }));
        setPicked(null);
    };

    const takeBack = (index: number) => {
        if (!card) {
            return;
        }
        const next = [...placement];
        next[index] = null;
        setPlacements((current) => ({ ...current, [card.id]: next }));
    };

    const checkTable = () => {
        if (!layout) {
            return;
        }
        setRevealed(true);
        setVerdict(correctCount(layout, placement) === layout.items.length ? 'correct' : 'wrong');
    };

    const goNext = () => {
        setIndex((current) => current + 1);
        reset();
    };

    if (status === 'loading') {
        return (
            <StatusScreen
                icon={<Spinner size={32} appearance="neutral-themed" />}
                title="Собираем карточки"
                text="Готовим вопросы по вашему конспекту"
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
                    <Button size="medium" variant="primary" stretched onClick={retry}>
                        Повторить
                    </Button>
                }
            />
        );
    }

    if (!card) {
        return (
            <StatusScreen
                icon={<IconCheck size={48} tone="muted" />}
                title="На сегодня всё"
                text="Сложные карточки вернутся через 2 дня"
                action={
                    <Button size="medium" variant="secondary" stretched onClick={onExit}>
                        На главную
                    </Button>
                }
            />
        );
    }

    const options = optionsOf(card);
    const showAnswer = given !== null || revealed;
    const canVoice = card.kind !== 'flip';
    // У flip своего ответа нет, поэтому верный показываем всегда —
    // иначе после самооценки «Знал» на экране не осталось бы ответа.
    const hasGiven = given !== null && card.kind !== 'flip';
    const showRightAnswer = !hasGiven || verdict === 'wrong';

    return (
        <Screen>
            <Flex direction="column" align="stretch" gap={8}>
                <Flex justify="space-between" align="center" gap={8}>
                    <Typography.Text variant="label" color="secondary">
                        {card.topic}
                    </Typography.Text>
                    <Typography.Text variant="label" color="secondary">
                        {index + 1} / {cards.length}
                    </Typography.Text>
                </Flex>
                <div className={s.progressTrack} aria-hidden="true">
                    <div
                        className={s.progressFill}
                        style={{ width: `${((index + 1) / cards.length) * 100}%` }}
                    />
                </div>
            </Flex>

            {showAnswer ? (
                <Flex
                    direction="column"
                    align="stretch"
                    gap={16}
                    aria-live="polite"
                    className={cx(
                        s.card,
                        !layout && verdict === 'correct' && s.cardCorrect,
                        !layout && verdict === 'wrong' && s.cardWrong,
                    )}
                >
                    {layout ? (
                        <Typography.Text variant="subheader">
                            {correctCount(layout, placement)} из {layout.items.length} на своих местах
                        </Typography.Text>
                    ) : verdict ? (
                        <Flex align="center" gap={8}>
                            {verdict === 'correct' ? (
                                <IconCheck size={28} tone="positive" />
                            ) : (
                                <IconCross size={28} tone="negative" />
                            )}
                            <Typography.Text variant="subheader">
                                {verdict === 'correct' ? 'Верно' : 'Неверно'}
                            </Typography.Text>
                        </Flex>
                    ) : (
                        <Typography.Text variant="subheader">Ответ</Typography.Text>
                    )}

                    <Typography.Text variant="body-strong" color="secondary">
                        {card.question}
                    </Typography.Text>

                    {layout ? (
                        <TableColumns
                            layout={layout}
                            placement={placement}
                            checked
                            hasSelection={false}
                            onDropTo={putInColumn}
                            onTakeBack={takeBack}
                        />
                    ) : null}

                    <Flex direction="column" align="stretch" gap={12}>
                        {!layout && hasGiven ? (
                            <Flex align="center" gap={12} className={s.answerRow}>
                                {verdict === 'correct' ? (
                                    <IconCheck tone="positive" />
                                ) : (
                                    <IconCross tone="negative" />
                                )}
                                <Flex direction="column">
                                    <Typography.Text variant="label" color="tertiary">
                                        Ваш ответ
                                    </Typography.Text>
                                    <Typography.Text variant="body" className={s.answerText}>
                                        {labelOf(card, given)}
                                    </Typography.Text>
                                </Flex>
                            </Flex>
                        ) : null}

                        {!layout && showRightAnswer ? (
                            <Flex align="center" gap={12} className={s.answerRow}>
                                <IconCheck tone="positive" />
                                <Flex direction="column">
                                    <Typography.Text variant="label" color="tertiary">
                                        Верный ответ
                                    </Typography.Text>
                                    <Typography.Text variant="body" className={s.answerText}>
                                        {labelOf(card, card.answer)}
                                    </Typography.Text>
                                </Flex>
                            </Flex>
                        ) : null}
                    </Flex>

                    <div className={s.divider} />

                    <Typography.Text variant="body" asChild>
                        <p className={s.explanation}>{card.explanation}</p>
                    </Typography.Text>

                    <figure className={s.source}>
                        <Flex direction="column" align="stretch" gap={4}>
                            <Typography.Text variant="label-strong" color="tertiary" asChild>
                                <figcaption>
                                    <Flex align="center" gap={4}>
                                        <IconDoc size={14} tone="muted" />
                                        Из вашего конспекта
                                        {card.sourceRef ? ` · ${card.sourceRef}` : ''}
                                    </Flex>
                                </figcaption>
                            </Typography.Text>
                            <Typography.Text variant="description" color="tertiary" asChild>
                                <blockquote className={s.quote}>«{card.sourceQuote}»</blockquote>
                            </Typography.Text>
                        </Flex>
                    </figure>
                </Flex>
            ) : (
                <Flex
                    direction="column"
                    align="stretch"
                    justify="center"
                    gap={24}
                    className={cx(s.card, s.cardCentered)}
                >
                    <Typography.Text variant="subheader" asChild>
                        <h1 className={s.question}>{card.question}</h1>
                    </Typography.Text>

                    {layout ? (
                        <TableColumns
                            layout={layout}
                            placement={placement}
                            checked={false}
                            hasSelection={pickedIndex !== null}
                            onDropTo={putInColumn}
                            onTakeBack={takeBack}
                        />
                    ) : null}

                    {listening ? (
                        <Flex direction="column" align="stretch" gap={4} aria-live="polite" className={s.voice}>
                            <Flex align="center" gap={4}>
                                <IconMic size={14} tone="muted" />
                                <Typography.Text variant="label" color="tertiary">
                                    Слушаю…
                                </Typography.Text>
                            </Flex>
                            {transcript ? (
                                <Typography.Text variant="body" color="secondary" className={s.transcript}>
                                    «{transcript}»
                                </Typography.Text>
                            ) : null}
                        </Flex>
                    ) : null}
                </Flex>
            )}

            {showAnswer ? (
                <Flex direction="column" align="stretch" gap={8}>
                    {verdict === null ? (
                        <Flex gap={8} align="stretch">
                            <Button
                                size="medium"
                                variant="secondary"
                                stretched
                                iconBefore={<IconCross size={20} tone="negative" />}
                                onClick={() => setVerdict('wrong')}
                            >
                                Не знал
                            </Button>
                            <Button
                                size="medium"
                                variant="secondary"
                                stretched
                                iconBefore={<IconCheck size={20} tone="positive" />}
                                onClick={() => setVerdict('correct')}
                            >
                                Знал
                            </Button>
                        </Flex>
                    ) : (
                        <Button size="medium" variant="primary" stretched onClick={goNext}>
                            Дальше
                        </Button>
                    )}

                    <Button
                        size="small"
                        variant="ghost"
                        stretched
                        iconBefore={<IconFlag size={16} tone="muted" />}
                        onClick={() => onReportCard(card.id)}
                    >
                        <Typography.Text variant="description" color="tertiary">
                            Карточка неверная
                        </Typography.Text>
                    </Button>
                </Flex>
            ) : layout ? (
                <Flex direction="column" align="stretch" gap={8}>
                    <TablePool
                        layout={layout}
                        placement={placement}
                        selected={pickedIndex}
                        onSelect={(itemIndex) =>
                            setPicked({ cardId: card.id, index: itemIndex })
                        }
                    />
                    <Button
                        size="medium"
                        variant="secondary"
                        stretched
                        disabled={placedCount(placement) < layout.items.length}
                        onClick={checkTable}
                    >
                        Проверить
                    </Button>
                </Flex>
            ) : (
                <Flex direction="column" align="stretch" gap={8}>
                    {options.map((option) => (
                        <button
                            key={option.value}
                            type="button"
                            className={s.option}
                            disabled={listening}
                            onClick={() => submit(option.value)}
                        >
                            <Typography.Text variant="body">{option.label}</Typography.Text>
                        </button>
                    ))}

                    {card.kind === 'input' ? (
                        <>
                            <Input
                                size="large"
                                value={draft}
                                placeholder="Ваш ответ"
                                disabled={listening}
                                onChange={(event) => setDraft(event.target.value)}
                            />
                            <Button
                                size="medium"
                                variant="secondary"
                                stretched
                                disabled={draft.trim().length === 0}
                                onClick={() => submit(draft)}
                            >
                                Ответить
                            </Button>
                        </>
                    ) : null}

                    {card.kind === 'flip' ? (
                        <Button
                            size="medium"
                            variant="secondary"
                            stretched
                            onClick={() => setRevealed(true)}
                        >
                            Показать ответ
                        </Button>
                    ) : null}

                    {canVoice ? (
                        listening ? (
                            <Button
                                size="medium"
                                variant="primary"
                                stretched
                                iconBefore={<IconStop size={20} />}
                                onClick={() => {
                                    setListening(false);
                                    setTranscript('');
                                }}
                            >
                                Остановить
                            </Button>
                        ) : (
                            <Button
                                size="medium"
                                variant="ghost"
                                stretched
                                iconBefore={<IconMic size={20} />}
                                onClick={() => setListening(true)}
                            >
                                Ответить голосом
                            </Button>
                        )
                    ) : null}
                </Flex>
            )}
        </Screen>
    );
}

export default Feed;
