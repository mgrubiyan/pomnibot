import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Button, Flex, IconButton, Input, Spinner, Typography } from '@maxhub/max-ui';
import type { AnswerResult, Card, CheckMethod } from '../types';
import { api } from '../api';
import { TableColumns, TablePool } from '../components/TableCard';
import { visibleCards, type CardPatch } from '../utils/cards';
import { AnswerCheck, type CheckStage } from '../components/AnswerCheck';
import {
    correctCount,
    emptyPlacement,
    placedCount,
    type Placement,
} from '../utils/table';
import { Screen } from '../components/Screen';
import { StatusScreen } from '../components/StatusScreen';
import { Result } from './Result';
import { cx } from '../utils/cx';
import {
    IconCheck,
    IconChevronLeft,
    IconCross,
    IconDoc,
    IconOffline,
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

/** Matches the transform duration in Feed.module.css. */
const FLIP_MS = 350;
/** Matches the cardOut animation in Feed.module.css. */
const CARD_OUT_MS = 150;

/** With reduced motion the card changes without turning or sliding. */
const motionAllowed = () => !window.matchMedia('(prefers-reduced-motion: reduce)').matches;

/** Answers are compared loosely: case, extra spaces and ё do not matter. */
const norm = (value: string) => value.trim().toLowerCase().replace(/ё/g, 'е').replace(/\s+/g, ' ');

/** After this long the check gives up and the reader compares by hand. */
const CHECK_TIMEOUT_MS = 20_000;
/** Until then waiting is normal and the card says nothing about it. */
const CHECK_SLOW_MS = 4_000;

type Checked =
    | { ok: true; correct: boolean; reason: string | null; method: CheckMethod }
    | { ok: false };

/**
 * A typed answer in other words than the card's goes to the backend, which
 * forgives word forms and typos and asks a model about the meaning. A check
 * that never answers is not a wrong answer: the card then shows the excerpt
 * from the notes and lets the reader judge.
 */
async function checkAnswer(cardId: string, answer: string): Promise<Checked> {
    const controller = new AbortController();
    const timer = window.setTimeout(() => controller.abort(), CHECK_TIMEOUT_MS);
    try {
        const { data } = await api.POST('/cards/{cardId}/check', {
            params: { path: { cardId } },
            body: { answer },
            signal: controller.signal,
        });
        return data
            ? { ok: true, correct: data.isCorrect, reason: data.reason ?? null, method: data.method }
            : { ok: false };
    } catch {
        return { ok: false };
    } finally {
        window.clearTimeout(timer);
    }
}

function shuffle<T>(items: readonly T[]): T[] {
    const next = [...items];
    for (let i = next.length - 1; i > 0; i--) {
        const j = Math.floor(Math.random() * (i + 1));
        [next[i], next[j]] = [next[j], next[i]];
    }
    return next;
}

function optionsOf(card: Card): Option[] {
    if (card.kind === 'boolean') {
        return BOOLEAN_OPTIONS;
    }
    if (card.kind === 'choice') {
        return shuffle(
            (card.options ?? []).map((option) => ({ label: option, value: option })),
        );
    }
    return [];
}

function answerTextOf(card: Card): string {
    if (typeof card.answer === 'boolean') {
        return card.answer ? 'true' : 'false';
    }
    if (typeof card.answer === 'number' && card.options) {
        return card.options[card.answer] ?? String(card.answer);
    }
    if (typeof card.answer === 'object' && card.answer !== null) {
        return '';
    }
    return String(card.answer ?? '');
}

/** Boolean cards store 'true' / 'false', which must never reach the screen. */
function labelOf(card: Card, value: string): string {
    if (card.kind !== 'boolean') {
        return value;
    }
    return value === 'true' ? 'Верно' : 'Неверно';
}

async function loadCards(setId?: string): Promise<Card[]> {
    const { data, error } = await api.GET('/feed');
    if (error || !data) {
        throw new Error(error?.message ?? 'Failed to load feed');
    }
    if (setId) {
        return data.filter((card) => card.setId === setId);
    }
    return data;
}


export interface FeedProps {
    /** When set, only the cards of this set are shown. */
    setId?: string;
    /** Title of that set, for the result screen. */
    setTitle?: string;
    /** Cards deleted and edited during this session. */
    removedCardIds: string[];
    cardPatches: Record<string, CardPatch>;
    initialIndex?: number;
    initialResults?: AnswerResult[];
    onExit: () => void;
    onBack?: () => void;
    /** Opens sharing from the result screen; absent for the daily mix. */
    onShare?: () => void;
}

export function Feed({
    setId,
    setTitle,
    removedCardIds,
    cardPatches,
    initialIndex = 0,
    initialResults = [],
    onExit,
    onBack,
    onShare,
}: FeedProps) {
    const [status, setStatus] = useState<Status>('loading');
    const [cards, setCards] = useState<Card[]>([]);
    const [index, setIndex] = useState(initialIndex);
    const [results, setResults] = useState<AnswerResult[]>(initialResults);

    const [given, setGiven] = useState<string | null>(null);
    const [verdict, setVerdict] = useState<Verdict | null>(null);
    // A typed answer being checked by meaning, and why it counts or not.
    const [check, setCheck] = useState<{ stage: CheckStage; answer: string } | null>(null);
    const [reason, setReason] = useState<string | null>(null);
    const [method, setMethod] = useState<CheckMethod | null>(null);
    // Every attempt gets a number, so a late answer from an abandoned one
    // — the reader stopped waiting, moved on or retried — is dropped.
    const checkRunRef = useRef(0);
    const [checkRun, setCheckRun] = useState(0);
    const [revealed, setRevealed] = useState(false);
    const [draft, setDraft] = useState('');
    // The layout is keyed by card id instead of being reset by an effect:
    // that way it appears on its own and needs no setState in an effect.
    const [placements, setPlacements] = useState<Record<string, Placement>>({});
    const [picked, setPicked] = useState<{ cardId: string; index: number } | null>(null);

    // While the card is turning the buttons stay disabled: the answer
    // must not run ahead of the animation.
    const [flipping, setFlipping] = useState(false);
    // The card on its way out is still the current one, so the index
    // only moves on once it has left.
    const [leaving, setLeaving] = useState(false);
    // One entry per card the user moved on from — the result screen
    // is built from it, and later the backend will save each one.

    const [attempt, setAttempt] = useState(0);

    const card = cards[index];
    const options = useMemo(() => (card ? optionsOf(card) : []), [card]);

    // Reloading goes through an attempt counter: the button handler flips
    // the status, the effect only fetches the data.
    useEffect(() => {
        let cancelled = false;

        loadCards(setId)
            .then((next) => {
                if (cancelled) {
                    return;
                }
                setCards(visibleCards(next, setId, removedCardIds, cardPatches));
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

    const startFlip = useCallback(() => setFlipping(motionAllowed()), []);

    useEffect(() => {
        if (!flipping) {
            return;
        }
        const timer = window.setTimeout(() => setFlipping(false), FLIP_MS);

        return () => window.clearTimeout(timer);
    }, [flipping]);

    const askCheck = useCallback(
        (cardId: string, answer: string) => {
            const run = checkRunRef.current + 1;
            checkRunRef.current = run;
            setCheck({ stage: 'checking', answer });
            setCheckRun(run);

            void checkAnswer(cardId, answer).then((result) => {
                if (checkRunRef.current !== run) {
                    return; // the reader stopped waiting or moved on
                }
                // A silent model is not a wrong answer: on fallback the backend
                // reports the local check, which was unsure — «Неверно» here
                // would be undeserved, so the reader compares by hand.
                if (!result.ok || (result.method === 'fallback' && !result.correct)) {
                    setCheck((current) => (current ? { ...current, stage: 'failed' } : current));
                    return;
                }
                setCheck(null);
                setReason(result.reason);
                setMethod(result.method);
                setGiven(answer);
                setVerdict(result.correct ? 'correct' : 'wrong');
                startFlip();
            });
        },
        [startFlip],
    );

    // Waiting is silent at first; past CHECK_SLOW_MS the card admits it.
    useEffect(() => {
        if (checkRun === 0) {
            return;
        }
        const timer = window.setTimeout(
            () =>
                setCheck((current) =>
                    current?.stage === 'checking' ? { ...current, stage: 'slow' } : current,
                ),
            CHECK_SLOW_MS,
        );

        return () => window.clearTimeout(timer);
    }, [checkRun]);

    /** The reader stops waiting: the excerpt and the two buttons instead. */
    const showReference = () => {
        checkRunRef.current += 1;
        setCheck((current) => (current ? { ...current, stage: 'shown' } : current));
    };

    /** Nobody judged the answer, so the verdict comes from the reader. */
    const selfCheck = (correct: boolean) => {
        if (!check) {
            return;
        }
        checkRunRef.current += 1;
        setCheck(null);
        setReason(null);
        setMethod(null);
        setGiven(check.answer);
        setVerdict(correct ? 'correct' : 'wrong');
        startFlip();
    };

    const submit = useCallback(
        (value: string) => {
            if (!card) {
                return;
            }
            const exact = norm(value) === norm(answerTextOf(card));
            if (card.kind !== 'input' || exact) {
                setGiven(value);
                setVerdict(exact ? 'correct' : 'wrong');
                startFlip();
                return;
            }
            // The card stays face up while the answer is being checked:
            // the breakdown has nothing to show until the verdict is in.
            // Other words: the card turns once the backend has judged them.
            askCheck(card.id, value);
        },
        [card, askCheck, startFlip],
    );

    const reset = useCallback(() => {
        checkRunRef.current += 1;
        setCheck(null);
        setReason(null);
        setMethod(null);
        setGiven(null);
        setVerdict(null);
        setRevealed(false);
        setDraft('');
        setPicked(null);
        setFlipping(false);
    }, []);

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

    const reveal = () => {
        setRevealed(true);
        startFlip();
    };

    const checkTable = () => {
        if (!layout) {
            return;
        }
        setRevealed(true);
        setVerdict(correctCount(layout, placement) === layout.items.length ? 'correct' : 'wrong');
        startFlip();
    };

    const showNext = useCallback(() => {
        setIndex((current) => current + 1);
        reset();
    }, [reset]);

    const goNext = () => {
        if (card && verdict) {
            const result: AnswerResult = {
                cardId: card.id,
                correct: verdict === 'correct',
                answeredAt: new Date().toISOString(),
            };
            setResults((current) => [...current, result]);
        }
        if (!motionAllowed()) {
            showNext();
            return;
        }
        setLeaving(true);
    };

    useEffect(() => {
        if (!leaving) {
            return;
        }
        const timer = window.setTimeout(() => {
            setLeaving(false);
            showNext();
        }, CARD_OUT_MS);

        return () => window.clearTimeout(timer);
    }, [leaving, showNext]);

    const submittedCardIdsRef = useRef<Set<string>>(new Set());
    const inFlightRef = useRef<Promise<void> | null>(null);
    const [isLeaving, setIsLeaving] = useState(false);

    const submitResults = useCallback(
        async (currentResults: AnswerResult[]): Promise<void> => {
            if (inFlightRef.current) {
                try {
                    await inFlightRef.current;
                } catch {
                    // ignore
                }
            }
            const unsubmitted = currentResults.filter(
                (r) => !submittedCardIdsRef.current.has(r.cardId),
            );
            if (unsubmitted.length === 0) {
                return;
            }
            const validResults = setId
                ? unsubmitted.filter((r) => cards.some((c) => c.id === r.cardId))
                : unsubmitted;
            if (validResults.length === 0) {
                return;
            }
            for (const r of validResults) {
                submittedCardIdsRef.current.add(r.cardId);
            }
            const promise = api
                .POST('/results', {
                    body: validResults,
                })
                .then(() => {})
                .catch((err: unknown) => {
                    console.error('Failed to submit results:', err);
                })
                .finally(() => {
                    inFlightRef.current = null;
                });
            inFlightRef.current = promise;
            await promise;
        },
        [cards, setId],
    );

    const handleBack = async () => {
        if (isLeaving) {
            return;
        }
        setIsLeaving(true);
        try {
            await submitResults(results);
        } finally {
            if (onBack) {
                onBack();
            } else {
                onExit();
            }
        }
    };

    const handleExit = async () => {
        if (inFlightRef.current) {
            try {
                await inFlightRef.current;
            } catch {
                // ignore
            }
        }
        onExit();
    };

    useEffect(() => {
        if (!card && results.length > 0) {
            void submitResults(results);
        }
    }, [card, results, submitResults]);

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
                    <Flex direction="column" align="stretch" gap={8}>
                        <Button size="medium" variant="primary" stretched onClick={retry}>
                            Повторить
                        </Button>
                        <Button size="medium" variant="ghost" stretched onClick={handleBack}>
                            Назад
                        </Button>
                    </Flex>
                }
            />
        );
    }

    if (!card) {
        // A finished run gets its result; «На сегодня всё» is for a feed
        // that had nothing to review from the start.
        if (results.length > 0) {
            return (
                <Result
                    setTitle={setTitle}
                    cards={cards}
                    results={results}
                    onExit={handleExit}
                    onShare={onShare}
                />
            );
        }
        return (
            <StatusScreen
                icon={<IconCheck size={48} tone="muted" />}
                title="На сегодня всё"
                text="Сложные карточки вернутся через 2 дня"
                action={
                    <Button size="medium" variant="secondary" stretched onClick={handleExit}>
                        {setId ? 'К набору' : 'На главную'}
                    </Button>
                }
            />
        );
    }

    const showAnswer = given !== null || revealed;
    // A flip card has no answer of its own, so the right one is always
    // shown — after «Знал» the screen would otherwise hold no answer.
    const hasGiven = given !== null && card.kind !== 'flip';
    const showRightAnswer = !hasGiven || verdict === 'wrong';
    // The buttons are off while the card is turning or leaving.
    const busy = flipping || leaving;
    // A flip card has nothing to pick from, so the side itself opens
    // the answer — the button below stays for keyboard and screen readers.
    const tapToReveal = card.kind === 'flip' && !showAnswer;

    return (
        <Screen>
            <Flex direction="column" align="stretch" gap={8}>
                <Flex justify="space-between" align="center" gap={8}>
                    <Flex align="center" gap={8} style={{ minWidth: 0 }}>
                        <IconButton
                            size="small"
                            variant="ghost"
                            aria-label="Назад"
                            disabled={isLeaving}
                            onClick={handleBack}
                        >
                            <IconChevronLeft size={20} />
                        </IconButton>
                        <Typography.Text variant="label" color="secondary">
                            {card.topic}
                        </Typography.Text>
                    </Flex>
                    <Typography.Text variant="label" color="secondary" style={{ flexShrink: 0 }}>
                        {index + 1} / {cards.length}
                    </Typography.Text>
                </Flex>
                <div className={s.progressTrack} aria-hidden="true">
                    <div
                        className={s.progressFill}
                        style={{ transform: `scaleX(${(index + 1) / cards.length})` }}
                    />
                </div>
            </Flex>

            <div key={card.id} className={cx(s.flip, leaving && s.leaving)}>
                <div className={cx(s.flipInner, showAnswer && s.flipped)}>
                    {/* While the answer is being checked the card carries three
                        blocks — question, answer, what the check is doing —
                        spread top to bottom instead of one question in the middle. */}
                    <Flex
                        direction="column"
                        align="stretch"
                        justify={check ? 'space-between' : 'center'}
                        gap={24}
                        inert={showAnswer || flipping}
                        aria-hidden={showAnswer}
                        onClick={tapToReveal ? reveal : undefined}
                        className={cx(
                            s.face,
                            s.faceFront,
                            s.card,
                            !check && s.cardCentered,
                            tapToReveal && s.tappable,
                        )}
                    >
                        <Typography.Text variant="subheader" asChild>
                            <h1 className={s.question}>{card.question}</h1>
                        </Typography.Text>

                        {check ? (
                            <AnswerCheck
                                stage={check.stage}
                                answer={check.answer}
                                sourceQuote={card.sourceQuote}
                                sourceRef={card.sourceRef}
                                onSelfCheck={selfCheck}
                            />
                        ) : null}

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

                    </Flex>

                    <Flex
                        direction="column"
                        align="stretch"
                        gap={16}
                        aria-live="polite"
                        inert={!showAnswer || flipping}
                        aria-hidden={!showAnswer}
                        className={cx(
                            s.face,
                            s.faceBack,
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

                        {!layout && reason ? (
                            <Typography.Text variant="body" color="secondary">
                                {reason}
                            </Typography.Text>
                        ) : null}

                        {method === 'fallback' ? (
                            <Typography.Text variant="description" color="tertiary">
                                Сверили по словам: модель не ответила вовремя
                            </Typography.Text>
                        ) : null}

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
                                            {labelOf(card, answerTextOf(card))}
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
                </div>
            </div>

            {showAnswer ? (
                <Flex direction="column" align="stretch" gap={8}>
                    {verdict === null ? (
                        <Flex gap={8} align="stretch">
                            <Button
                                size="medium"
                                variant="secondary"
                                stretched
                                disabled={flipping}
                                iconBefore={<IconCross size={20} tone="negative" />}
                                onClick={() => setVerdict('wrong')}
                            >
                                Не знал
                            </Button>
                            <Button
                                size="medium"
                                variant="secondary"
                                stretched
                                disabled={flipping}
                                iconBefore={<IconCheck size={20} tone="positive" />}
                                onClick={() => setVerdict('correct')}
                            >
                                Знал
                            </Button>
                        </Flex>
                    ) : (
                        <Button
                            size="medium"
                            variant="primary"
                            stretched
                            disabled={busy}
                            onClick={goNext}
                        >
                            Дальше
                        </Button>
                    )}
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
                            onClick={() => submit(option.value)}
                        >
                            <Typography.Text variant="body">{option.label}</Typography.Text>
                        </button>
                    ))}

                    {card.kind === 'input' && !check ? (
                        <>
                            <Input
                                size="large"
                                value={draft}
                                placeholder="Ваш ответ"
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

                    {check ? (
                        <>
                            {check.stage === 'checking' || check.stage === 'slow' ? (
                                <Button size="medium" variant="secondary" stretched disabled>
                                    Проверяю ответ
                                </Button>
                            ) : null}

                            {check.stage === 'slow' ? (
                                <Button
                                    size="medium"
                                    variant="secondary"
                                    stretched
                                    onClick={showReference}
                                >
                                    Показать ответ
                                </Button>
                            ) : null}

                            {check.stage === 'failed' || check.stage === 'shown' ? (
                                <Button
                                    size="medium"
                                    variant="secondary"
                                    stretched
                                    onClick={() => askCheck(card.id, check.answer)}
                                >
                                    Попробовать снова
                                </Button>
                            ) : null}
                        </>
                    ) : null}

                    {card.kind === 'flip' ? (
                        <Button
                            size="medium"
                            variant="secondary"
                            stretched
                            onClick={reveal}
                        >
                            Показать ответ
                        </Button>
                    ) : null}
                </Flex>
            )}
        </Screen>
    );
}

export default Feed;
