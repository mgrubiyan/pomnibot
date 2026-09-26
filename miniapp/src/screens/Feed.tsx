import { useCallback, useEffect, useState } from 'react';
import { Button, Flex, Input, Spinner, Typography } from '@maxhub/max-ui';
import type { AnswerResult, Card } from '../types';
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
import { Result } from './Result';
import { useBackButton } from '../max/useBackButton';
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
/** Matches the transform duration in Feed.module.css. */
const FLIP_MS = 350;
/** Matches the cardOut animation in Feed.module.css. */
const CARD_OUT_MS = 150;

/** With reduced motion the card changes without turning or sliding. */
const motionAllowed = () => !window.matchMedia('(prefers-reduced-motion: reduce)').matches;

/** Answers are compared loosely: case, extra spaces and ё do not matter. */
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

/** Boolean cards store 'true' / 'false', which must never reach the screen. */
function labelOf(card: Card, value: string): string {
    if (card.kind !== 'boolean') {
        return value;
    }
    return value === 'true' ? 'Верно' : 'Неверно';
}

/**
 * Mocks instead of a request; the real endpoint comes later.
 * Error screen — ?fail, a single card by kind or id — ?card=table, ?card=c9
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

/** Recognition stub: no engine yet, so we always «hear» the right answer. */
function mockTranscript(card: Card): string {
    if (card.kind === 'boolean') {
        return card.answer === 'true' ? 'верно' : 'неверно';
    }
    if (card.kind === 'choice') {
        return card.answer.split(' ').slice(0, 4).join(' ').toLowerCase();
    }
    return card.answer.toLowerCase();
}

/** Maps the recognized phrase onto an answer option when one matches. */
function matchTranscript(card: Card, phrase: string): string {
    const options = optionsOf(card);
    if (options.length === 0) {
        return phrase;
    }
    const hit = options.find((option) => norm(option.label).startsWith(norm(phrase)));
    return hit ? hit.value : phrase;
}

export interface FeedProps {
    /** When set, only the cards of this set are shown. */
    setId?: string;
    /** Title of that set, for the result screen. */
    setTitle?: string;
    /** Cards deleted and edited during this session. */
    removedCardIds: string[];
    cardPatches: Record<string, CardPatch>;
    onExit: () => void;
    onReportCard: (cardId: string) => void;
    /** Opens sharing from the result screen; absent for the daily mix. */
    onShare?: () => void;
}

export function Feed({
    setId,
    setTitle,
    removedCardIds,
    cardPatches,
    onExit,
    onReportCard,
    onShare,
}: FeedProps) {
    const [status, setStatus] = useState<Status>('loading');
    const [cards, setCards] = useState<Card[]>([]);
    const [index, setIndex] = useState(0);

    const [given, setGiven] = useState<string | null>(null);
    const [verdict, setVerdict] = useState<Verdict | null>(null);
    const [revealed, setRevealed] = useState(false);
    const [draft, setDraft] = useState('');
    const [listening, setListening] = useState(false);
    const [transcript, setTranscript] = useState('');
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
    const [results, setResults] = useState<AnswerResult[]>([]);

    const [attempt, setAttempt] = useState(0);

    // The feed has no back control of its own: in MAX the header button
    // leaves it, the same way the result screen does.
    useBackButton(onExit);

    const card = cards[index];

    // Reloading goes through an attempt counter: the button handler flips
    // the status, the effect only fetches the data.
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

    const startFlip = useCallback(() => setFlipping(motionAllowed()), []);

    useEffect(() => {
        if (!flipping) {
            return;
        }
        const timer = window.setTimeout(() => setFlipping(false), FLIP_MS);

        return () => window.clearTimeout(timer);
    }, [flipping]);

    const submit = useCallback(
        (value: string) => {
            if (!card) {
                return;
            }
            setGiven(value);
            setVerdict(norm(value) === norm(card.answer) ? 'correct' : 'wrong');
            startFlip();
        },
        [card, startFlip],
    );

    // Voice answer stub: a «listening» pause, then the phrase, then the answer.
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

    const reset = useCallback(() => {
        setGiven(null);
        setVerdict(null);
        setRevealed(false);
        setDraft('');
        setListening(false);
        setTranscript('');
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
        // A finished run gets its result; «На сегодня всё» is for a feed
        // that had nothing to review from the start.
        if (results.length > 0) {
            return (
                <Result
                    setTitle={setTitle}
                    cards={cards}
                    results={results}
                    onExit={onExit}
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
                        style={{ transform: `scaleX(${(index + 1) / cards.length})` }}
                    />
                </div>
            </Flex>

            <div key={card.id} className={cx(s.flip, leaving && s.leaving)}>
                <div className={cx(s.flipInner, showAnswer && s.flipped)}>
                    <Flex
                        direction="column"
                        align="stretch"
                        justify="center"
                        gap={24}
                        inert={showAnswer || flipping}
                        aria-hidden={showAnswer}
                        onClick={tapToReveal ? reveal : undefined}
                        className={cx(
                            s.face,
                            s.faceFront,
                            s.card,
                            s.cardCentered,
                            tapToReveal && s.tappable,
                        )}
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

                    <Button
                        size="small"
                        variant="ghost"
                        stretched
                        disabled={busy}
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
                            onClick={reveal}
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
