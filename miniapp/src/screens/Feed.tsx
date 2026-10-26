import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Button, Flex, IconButton, Input, Spinner, Typography } from '@maxhub/max-ui';
import type { Answer, Card, CardAnswer, CardReveal, FeedCard } from '../types';
import { api } from '../api';
import { TableColumns, TablePool } from '../components/TableCard';
import { StudyContent } from '../components/StudyContent';
import { useStudyImagesReady } from '../utils/useStudyImages';
import { StudyImages } from '../components/StudyImages';
import { StudyIllustration } from '../components/StudyIllustration';
import type { CardPatch } from '../utils/cards';
import { layoutOf, rejoinedFeed, resumedCards, revealedCard, type FeedResume } from '../utils/feed';
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
import { kindTask } from '../utils/kinds';
import { motionAllowed } from '../utils/motion';
import { voiceInputAvailable } from '../utils/speech';
import { useVoiceAnswer, type VoiceState } from '../utils/useVoiceAnswer';
import {
    IconCheck,
    IconChevronLeft,
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

/** Matches the transform duration in Feed.module.css. */
const FLIP_MS = 300;
/** Matches the cardOut animation in Feed.module.css. */
const CARD_OUT_MS = 150;

/** With reduced motion the card changes without turning or sliding. */

/** The reader always waits for the verdict; past these marks only the words change. */
const CHECK_LONGER_MS = 8_000;
const CHECK_ALMOST_MS = 12_000;
/** A lost request or a silent model: ask again after this pause. */
const CHECK_RETRY_MS = 2_000;
/** Requests lost in a row before the run stops on the error screen. */
const CHECK_LOST_LIMIT = 3;

interface AnswerAttempt {
    attemptId: string;
    answer: string;
    typed: string | null;
    onVerdict: (result: Checked) => void;
}

interface Checked {
    correct: boolean;
    reason: string | null;
    reveal: CardReveal;
}

/** The feed no longer asks the card, as when its fact moved on through another card meanwhile. */
const GONE = 'gone';
/**
 * The backend refused the card for another reason, as when the reader's
 * sign-in expired and the card is no longer theirs: the run stops instead of
 * skipping every card that follows.
 */
const FAILED = 'failed';

type Refusal = typeof GONE | typeof FAILED;

/** No answer from the backend: the request was lost or the backend failed. */
const LOST = 'lost';

function refusalOf(status: number): Refusal | null {
    if (status === 409) {
        return GONE;
    }
    return status >= 400 && status < 500 ? FAILED : null;
}

/**
 * Every answer goes to the backend, which grades and records it: the feed
 * holds no answers. A typed one need not match word for word — the backend
 * forgives word forms and typos and asks a model about the meaning. Null
 * when the backend gave up on the model (fallback): the reader keeps waiting
 * for a real verdict.
 */
async function sendAnswer(cardId: string, answer: string, attemptId: string): Promise<Checked | Refusal | typeof LOST | null> {
    try {
        const { data, response } = await api.POST('/cards/{cardId}/answer', {
            params: { path: { cardId } },
            body: { answer, attemptId },
        });
        const refusal = refusalOf(response.status);
        if (refusal) {
            return refusal;
        }
        if (!data) {
            return LOST;
        }
        if (data.method === 'fallback' || !data.reveal) {
            return null;
        }
        return { correct: data.isCorrect, reason: data.reason ?? null, reveal: data.reveal };
    } catch {
        return LOST;
    }
}

/** The answer of a flip card, which the reader then grades themselves. */
async function fetchReveal(cardId: string): Promise<CardReveal | Refusal | typeof LOST> {
    try {
        const { data, response } = await api.POST('/cards/{cardId}/reveal', {
            params: { path: { cardId } },
        });
        const refusal = refusalOf(response.status);
        if (refusal) {
            return refusal;
        }
        return data ?? LOST;
    } catch {
        return LOST;
    }
}

function optionsOf(card: FeedCard): Option[] {
    if (card.kind === 'boolean') {
        return BOOLEAN_OPTIONS;
    }
    if (card.kind === 'choice') {
        // In the order the backend shuffled them.
        return (card.options ?? []).map((option) => ({ label: option, value: option }));
    }
    return [];
}

function answerTextOf(answer: CardAnswer, options?: string[]): string {
    if (Array.isArray(answer)) return answer.join('\n\n');
    if (typeof answer === 'boolean') {
        return answer ? 'true' : 'false';
    }
    if (typeof answer === 'number' && options) {
        return options[answer] ?? String(answer);
    }
    if (typeof answer === 'object' && answer !== null) {
        return '';
    }
    return String(answer ?? '');
}

/** Boolean cards store 'true' / 'false', which must never reach the screen. */
function labelOf(card: FeedCard, value: string): string {
    if (card.kind !== 'boolean') {
        return value;
    }
    return value === 'true' ? 'Верно' : 'Неверно';
}

async function loadCards(setId?: string, early?: boolean): Promise<FeedCard[]> {
    const { data, error } = await api.GET('/feed', {
        params: { query: { setId, early } },
    });
    if (error || !data) {
        throw new Error(error?.message ?? 'Failed to load feed');
    }
    return data;
}

export interface FeedProps {
    /** When set, only the cards of this set are shown. */
    setId?: string;
    /** Title of that set, for the result screen. */
    setTitle?: string;
    /** Whether set is studied ahead of FSRS schedule. */
    early?: boolean;
    /** Cards deleted and edited during this session. */
    removedCardIds: string[];
    cardPatches: Record<string, CardPatch>;
    resume?: FeedResume;
    onExit: () => void;
    onBack?: () => void;
    /**
     * Opens the complaint about the card, whole now that it is answered.
     * Resolves to false when the complaint screen could not be opened.
     */
    onReportCard: (card: Card, resume: FeedResume) => Promise<boolean>;
    /** The result screen's main button: back to the main menu. */
    onHome: () => void;
}

export function Feed({
    setId,
    setTitle,
    early,
    removedCardIds,
    cardPatches,
    resume,
    onExit,
    onBack,
    onReportCard,
    onHome,
}: FeedProps) {
    const [status, setStatus] = useState<Status>(resume ? 'ready' : 'loading');
    const [cards, setCards] = useState<FeedCard[]>(() =>
        resume ? resumedCards(resume, removedCardIds, cardPatches) : [],
    );
    const [index, setIndex] = useState(resume?.index ?? 0);
    const [results, setResults] = useState<Answer[]>(resume?.results ?? []);

    const [given, setGiven] = useState<string | null>(null);
    const [selectedOptions, setSelectedOptions] = useState<string[]>([]);
    const [verdict, setVerdict] = useState<Verdict | null>(null);
    // An answer the backend is grading — a typed one stays on the card while
    // it waits — and why it counts or not.
    const [check, setCheck] = useState<{ stage: CheckStage; typed: string | null } | null>(null);
    /** The option tapped on a single-answer card, outlined while the backend checks it. */
    const [sentOption, setSentOption] = useState<string | null>(null);
    const [reason, setReason] = useState<string | null>(null);
    // What the card hid until it was answered or, for a flip card, opened.
    const [reveal, setReveal] = useState<CardReveal | null>(null);
    const [opening, setOpening] = useState(false);
    // Every attempt gets a number, so a late answer from an abandoned one
    // — the reader stopped waiting, moved on or retried — is dropped.
    const checkRunRef = useRef(0);
    const [checkRun, setCheckRun] = useState(0);
    const [revealed, setRevealed] = useState(false);
    const [draft, setDraft] = useState('');
    // A spoken answer lands in the field for the reader to check: a
    // misheard term would be graded as theirs.
    const [voiceAvailable] = useState(voiceInputAvailable);
    const voice = useVoiceAnswer(useCallback((text: string) => {
        setDraft((current) => (current.trim() ? `${current.trimEnd()} ${text}` : text));
    }, []));
    const { cancel: cancelVoice } = voice;
    // Opening the microphone may wait on a permission prompt: the field
    // stays free, and sending a typed answer drops the voice one.
    const voiceBusy = voice.state === 'recording' || voice.state === 'recognizing';
    // The field's placeholder tells how the recording goes; over typed text
    // the line under the field does.
    const voiceLine = voice.problem || (voice.state !== 'idle' && draft ? voicePlaceholder(voice.state, voice.seconds) : '');
    // The layout is keyed by card id instead of being reset by an effect:
    // that way it appears on its own and needs no setState in an effect.
    const [placements, setPlacements] = useState<Record<string, Placement>>({});
    const [picked, setPicked] = useState<{ cardId: string; index: number } | null>(null);

    // While the card is turning the buttons stay disabled: the answer
    // must not run ahead of the animation.
    const [flipping, setFlipping] = useState(false);
    // Keyed by card id like the layout, so the next card starts clean.
    const [report, setReport] = useState<{ cardId: string; stage: 'opening' | 'failed' } | null>(null);
    // The card on its way out is still the current one, so the index
    // only moves on once it has left.
    const [leaving, setLeaving] = useState(false);

    const [attempt, setAttempt] = useState(0);
    // The cards a stopped run has gone through: their answers are recorded,
    // so the reloaded feed no longer holds them.
    const passedRef = useRef<FeedCard[]>([]);

    const card = cards[index];
    const imagesReady = useStudyImagesReady(card?.questionImages);
    const options = useMemo(() => (card ? optionsOf(card) : []), [card]);

    // Reloading goes through an attempt counter: the button handler flips
    // the status, the effect only fetches the data. A resumed run keeps its
    // own cards.
    useEffect(() => {
        if (resume && attempt === 0) {
            return;
        }
        let cancelled = false;

        loadCards(setId, early)
            .then((next) => {
                if (cancelled) {
                    return;
                }
                setCards(rejoinedFeed(passedRef.current, next, removedCardIds, cardPatches));
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
    }, [attempt, resume, setId, early, removedCardIds, cardPatches]);

    const startFlip = useCallback(() => setFlipping(motionAllowed()), []);

    useEffect(() => {
        if (!flipping) {
            return;
        }
        const timer = window.setTimeout(() => setFlipping(false), FLIP_MS);

        return () => window.clearTimeout(timer);
    }, [flipping]);

    const reset = useCallback(() => {
        checkRunRef.current += 1;
        setCheck(null);
        setSentOption(null);
        setReason(null);
        setReveal(null);
        setOpening(false);
        setGiven(null);
        setVerdict(null);
        setRevealed(false);
        setDraft('');
        cancelVoice();
        setSelectedOptions([]);
        setPicked(null);
        setFlipping(false);
    }, [cancelVoice]);

    const showNext = useCallback(() => {
        setIndex((current) => current + 1);
        reset();
    }, [reset]);

    // Stop the check while retaining its submission for an explicit retry.
    const stop = useCallback(() => {
        reset();
        setStatus('error');
    }, [reset]);

    /**
     * Sends the answer and waits for the verdict, asking again while there is
     * none. A card the feed no longer asks is skipped without a result; a
     * backend that keeps failing stops the run.
     */
    // Keep the complete submission until its verdict is known.
    const answerAttempts = useRef(new Map<string, AnswerAttempt>());
    const ask = useCallback(
        (cardId: string, answer: string, typed: string | null, onVerdict: (result: Checked) => void) => {
            const submission = answerAttempts.current.get(cardId) ?? {
                attemptId: crypto.randomUUID(), answer, typed, onVerdict,
            };
            answerAttempts.current.set(cardId, submission);
            const run = checkRunRef.current + 1;
            checkRunRef.current = run;
            setCheck({ stage: 'checking', typed: submission.typed });
            setCheckRun(run);

            let lost = 0;
            const attempt = () => {
                if (checkRunRef.current !== run) {
                    return; // the reader moved on
                }
                void sendAnswer(cardId, submission.answer, submission.attemptId).then((result) => {
                    if (checkRunRef.current !== run) {
                        return;
                    }
                    lost = result === LOST ? lost + 1 : 0;
                    if (lost >= CHECK_LOST_LIMIT) {
                        stop();
                        return;
                    }
                    if (!result || result === LOST) {
                        // No verdict yet: the reader keeps waiting, we ask again.
                        window.setTimeout(attempt, CHECK_RETRY_MS);
                        return;
                    }
                    setCheck(null);
                    if (result === GONE) {
                        answerAttempts.current.delete(cardId);
                        showNext();
                        return;
                    }
                    if (result === FAILED) {
                        stop();
                        return;
                    }
                    answerAttempts.current.delete(cardId);
                    setReason(result.reason);
                    setReveal(result.reveal);
                    submission.onVerdict(result);
                });
            };
            attempt();
        },
        [showNext, stop],
    );

    const retry = () => {
        const submission = card && answerAttempts.current.get(card.id);
        if (card && submission) {
            // The backend may already have committed this answer. Replay it
            // before allowing fresh input, keeping the original table order.
            setStatus('ready');
            ask(card.id, submission.answer, submission.typed, submission.onVerdict);
            return;
        }
        passedRef.current = cards.slice(0, index);
        // The reloaded feed shuffles the terms anew, so a layout kept by
        // their places would put them in columns the reader never chose.
        setPlacements({});
        setStatus('loading');
        setAttempt((current) => current + 1);
    };

    // Leaving the feed drops a check that is still waiting or retrying.
    useEffect(
        () => () => {
            checkRunRef.current += 1;
        },
        [],
    );

    // The wait never ends early; past the marks only the words change.
    // Timers belong to the reader's attempt, so retries do not restart them.
    useEffect(() => {
        if (checkRun === 0) {
            return;
        }
        const toStage = (stage: CheckStage) => () =>
            setCheck((current) => (current ? { ...current, stage } : current));
        const longer = window.setTimeout(toStage('longer'), CHECK_LONGER_MS);
        const almost = window.setTimeout(toStage('almost'), CHECK_ALMOST_MS);

        return () => {
            window.clearTimeout(longer);
            window.clearTimeout(almost);
        };
    }, [checkRun]);

    /** Answers a choice, true-or-false or typed card; shown is what the reader picked. */
    const submit = useCallback(
        (answer: string, shown: string) => {
            if (!card || !imagesReady) {
                return;
            }
            // The card stays face up while the answer is being checked:
            // the breakdown has nothing to show until the verdict is in.
            ask(card.id, answer, card.kind === 'input' ? shown : null, (result) => {
                setGiven(shown);
                setVerdict(result.correct ? 'correct' : 'wrong');
                startFlip();
            });
        },
        [card, ask, startFlip, imagesReady],
    );

    /** Sends the typed answer, giving up on a microphone still opening. */
    const sendTyped = () => {
        cancelVoice();
        submit(draft, draft);
    };

    const layout =card?.table ? layoutOf(card.table, reveal?.answer, placements[card.id]) : undefined;
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

    // A flip card asks the backend for its answer; the reader grades
    // themselves once they have seen it.
    const openFlip = () => {
        if (!card || opening || !imagesReady) {
            return;
        }
        const cardId = card.id;
        const run = checkRunRef.current + 1;
        checkRunRef.current = run;
        setOpening(true);

        let lost = 0;
        const attempt = () => {
            if (checkRunRef.current !== run) {
                return;
            }
            void fetchReveal(cardId).then((result) => {
                if (checkRunRef.current !== run) {
                    return;
                }
                if (result === LOST) {
                    lost += 1;
                    if (lost >= CHECK_LOST_LIMIT) {
                        stop();
                    } else {
                        window.setTimeout(attempt, CHECK_RETRY_MS);
                    }
                    return;
                }
                setOpening(false);
                if (result === GONE) {
                    showNext();
                    return;
                }
                if (result === FAILED) {
                    stop();
                    return;
                }
                setReveal(result);
                setRevealed(true);
                startFlip();
            });
        };
        attempt();
    };

    const grade = (knew: boolean) => {
        if (!card) {
            return;
        }
        ask(card.id, String(knew), null, (result) => {
            setRevealed(true);
            setVerdict(result.correct ? 'correct' : 'wrong');
        });
    };

    const checkTable = () => {
        if (!card || !layout || !imagesReady) {
            return;
        }
        const placed = layout.items.map((item, itemIndex) => ({ text: item.text, column: placement[itemIndex] ?? '' }));
        ask(card.id, JSON.stringify(placed), null, (result) => {
            setRevealed(true);
            setVerdict(result.correct ? 'correct' : 'wrong');
            startFlip();
        });
    };

    const goNext = () => {
        if (card && verdict) {
            const result: Answer = {
                cardId: card.id,
                correct: verdict === 'correct',
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

    // Every answer is recorded as it is given, so leaving sends nothing.
    const handleBack = onBack ?? onExit;

    // When the next review comes: FSRS may bring cards back in minutes,
    // and Result says so. Asked once the run is over, when every answer
    // has been recorded.
    const finished = !card && results.length > 0;
    const [nextReviewAt, setNextReviewAt] = useState<string | null>(null);

    useEffect(() => {
        if (!finished) {
            return;
        }
        let cancelled = false;
        api.GET('/')
            .then(({ data }) => {
                if (!cancelled && data) {
                    setNextReviewAt(data.nextReviewAt ?? null);
                }
            })
            .catch(() => {
                // Result just goes without the line.
            });
        return () => {
            cancelled = true;
        };
    }, [finished]);

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
                    nextReviewAt={nextReviewAt}
                    onExit={onExit}
                    onHome={onHome}
                />
            );
        }
        return (
            <StatusScreen
                icon={<IconCheck size={48} tone="muted" />}
                title="На сегодня всё"
                text="Карточки вернутся, когда подойдёт срок повторения"
                action={
                    <Button size="medium" variant="secondary" stretched onClick={onExit}>
                        {setId ? 'К набору' : 'На главную'}
                    </Button>
                }
            />
        );
    }

    const showAnswer = (given !== null || revealed) && reveal !== null;
    // A flip card has no answer of its own, so the right one is always
    // shown — after «Знал» the screen would otherwise hold no answer.
    const hasGiven = given !== null && card.kind !== 'flip';
    const showRightAnswer = !hasGiven || verdict === 'wrong';
    // The buttons are off while the card is turning or leaving, and while
    // the backend grades the answer.
    const busy = flipping || leaving || check !== null || !imagesReady;
    // A flip card has nothing to pick from, so the side itself opens
    // the answer — the button below stays for keyboard and screen readers.
    const tapToReveal = card.kind === 'flip' && !showAnswer && !opening && imagesReady;

    const openReport = async () => {
        if (!reveal) {
            return;
        }
        setReport({ cardId: card.id, stage: 'opening' });
        // An answered card is done: the run comes back at the next one.
        const answered = verdict !== null;
        const resumeAt: FeedResume = {
            cards,
            index: answered ? index + 1 : index,
            results: answered ? [...results, { cardId: card.id, correct: verdict === 'correct' }] : results,
        };
        if (!(await onReportCard(revealedCard(card, reveal), resumeAt))) {
            setReport({ cardId: card.id, stage: 'failed' });
        }
    };

    return (
        <Screen>
            <Flex direction="column" align="stretch" gap={8}>
                <Flex justify="space-between" align="center" gap={8}>
                    <Flex align="center" gap={8} style={{ minWidth: 0 }}>
                        <IconButton
                            size="small"
                            variant="ghost"
                            aria-label="Назад"
                            className={s.back}
                            onClick={handleBack}
                        >
                            <IconChevronLeft size={20} />
                        </IconButton>
                        <Typography.Text variant="label" color="secondary" className={s.topic}>
                            {card.factName}
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
                    {/* While a typed answer is being checked the card carries three
                        blocks — question, answer, what the check is doing —
                        spread top to bottom instead of one question in the middle. */}
                    <Flex
                        direction="column"
                        align="stretch"
                        justify={check?.typed ? 'space-between' : 'start'}
                        gap={24}
                        inert={showAnswer || flipping}
                        aria-hidden={showAnswer}
                        onClick={tapToReveal ? openFlip : undefined}
                        className={cx(
                            s.face,
                            s.faceFront,
                            s.card,
                            !check?.typed && s.cardCentered,
                            tapToReveal && s.tappable,
                        )}
                    >
                        <Flex direction="column" align="stretch" gap={8}>
                            <Typography.Text variant="label" color="tertiary">
                                {kindTask(card.kind)}
                            </Typography.Text>
                            <Typography.Text variant="subheader" asChild>
                                <StudyContent className={s.question} role="heading" aria-level={1} text={card.question} />
                            </Typography.Text>
                        </Flex>

                        <StudyImages images={card.questionImages} />
                        <StudyIllustration svg={card.questionSvg} alt="Иллюстрация к вопросу" />

                        {check?.typed ? (
                            <AnswerCheck stage={check.stage} answer={check.typed} />
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

                        <Typography.Text variant="body-strong" color="secondary" asChild>
                            <StudyContent text={card.question} />
                        </Typography.Text>
                        <StudyImages images={card.questionImages} />
                        <StudyIllustration svg={card.questionSvg} alt="Иллюстрация к вопросу" />

                        {layout && showAnswer ? (
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
                                <Flex align="flex-start" gap={12} className={s.answerRow}>
                                    {verdict === 'correct' ? (
                                        <IconCheck tone="positive" className={s.answerIcon} />
                                    ) : (
                                        <IconCross tone="negative" className={s.answerIcon} />
                                    )}
                                    <Flex direction="column" className={s.answerBody}>
                                        <Typography.Text variant="label" color="tertiary">
                                            Ваш ответ
                                        </Typography.Text>
                                        <Typography.Text variant="body" asChild>
                                            <StudyContent className={s.answerText} text={labelOf(card, given)} />
                                        </Typography.Text>
                                    </Flex>
                                </Flex>
                            ) : null}

                            {!layout && reveal && showRightAnswer ? (
                                <Flex align="flex-start" gap={12} className={s.answerRow}>
                                    <IconCheck tone="positive" className={s.answerIcon} />
                                    <Flex direction="column" className={s.answerBody}>
                                        <Typography.Text variant="label" color="tertiary">
                                            Верный ответ
                                        </Typography.Text>
                                        <Typography.Text variant="body" asChild>
                                            <StudyContent className={s.answerText} text={labelOf(card, answerTextOf(reveal.answer, card.options))} />
                                        </Typography.Text>
                                    </Flex>
                                </Flex>
                            ) : null}
                        </Flex>

                        <div className={s.divider} />

                        {reveal?.explanation ? (
                            <Typography.Text variant="body" asChild>
                                <StudyContent className={s.explanation} text={reveal.explanation} />
                            </Typography.Text>
                        ) : null}

                        <StudyImages images={reveal?.explanationImages} />
                        <StudyIllustration svg={reveal?.explanationSvg} alt="Иллюстрация к объяснению" />

                        {/* A card written by hand may have no quote to show. */}
                        {reveal?.sourceQuote ? (
                            <figure className={s.source}>
                                <Flex direction="column" align="stretch" gap={4}>
                                    <Typography.Text variant="label-strong" color="tertiary" asChild>
                                        <figcaption>
                                            <Flex align="flex-start" gap={4}>
                                                <IconDoc size={14} tone="muted" className={s.sourceIcon} />
                                                <span>
                                                    Из конспекта
                                                    {reveal.sourceRef ? ` · ${reveal.sourceRef}` : ''}
                                                </span>
                                            </Flex>
                                        </figcaption>
                                    </Typography.Text>
                                    <Typography.Text variant="description" color="tertiary" asChild>
                                        <blockquote className={s.quote}>«{reveal.sourceQuote}»</blockquote>
                                    </Typography.Text>
                                </Flex>
                            </figure>
                        ) : null}
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
                                disabled={busy}
                                iconBefore={<IconCross size={20} tone="negative" />}
                                onClick={() => grade(false)}
                            >
                                Не знал
                            </Button>
                            <Button
                                size="medium"
                                variant="secondary"
                                stretched
                                disabled={busy}
                                iconBefore={<IconCheck size={20} tone="positive" />}
                                onClick={() => grade(true)}
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
                        disabled={busy || (report?.cardId === card.id && report.stage === 'opening')}
                        iconBefore={<IconFlag size={16} tone="muted" />}
                        onClick={openReport}
                    >
                        <Typography.Text variant="description" color="tertiary">
                            Ошибка в карточке
                        </Typography.Text>
                    </Button>
                    {report?.cardId === card.id && report.stage === 'failed' ? (
                        <Typography.Text variant="description" color="secondary" className={s.reportError}>
                            Не удалось открыть. Попробуйте ещё раз
                        </Typography.Text>
                    ) : null}
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
                        disabled={!imagesReady || check !== null || placedCount(placement) < layout.items.length}
                        onClick={checkTable}
                    >
                        {check ? 'Проверяю ответ' : 'Проверить'}
                    </Button>
                </Flex>
            ) : (
                <Flex direction="column" align="stretch" gap={8} className={s.controls}>
                    {options.length > 0 ? (
                        <div className={s.options}>
                            {options.map((option) => (
                                <button
                                    key={option.value}
                                    type="button"
                                    className={s.option}
                                    disabled={!imagesReady || check !== null}
                                    aria-pressed={card.kind === 'choice' ? selectedOptions.includes(option.value) : undefined}
                                    data-sent={check !== null && sentOption === option.value ? '' : undefined}
                                    onClick={() => {
                                        if (card.kind === 'choice') {
                                            setSelectedOptions((current) => current.includes(option.value)
                                                ? current.filter((value) => value !== option.value) : [...current, option.value]);
                                            return;
                                        }
                                        setSentOption(option.value);
                                        submit(option.value, option.value);
                                    }}
                                >
                                    <Typography.Text variant="body" asChild>
                                        <StudyContent text={option.label} inControl />
                                    </Typography.Text>
                                </button>
                            ))}
                        </div>
                    ) : null}

                    {card.kind === 'choice' ? (
                        <Button size="medium" variant="secondary" stretched disabled={!imagesReady || check !== null || selectedOptions.length === 0}
                            onClick={() => submit(JSON.stringify(selectedOptions), selectedOptions.join('\n\n'))}>
                            {check ? 'Проверяю ответ' : 'Проверить'}
                        </Button>
                    ) : null}

                    {card.kind === 'input' && !check ? (
                        <>
                            <Input
                                size="large"
                                value={draft}
                                placeholder={voicePlaceholder(voice.state, voice.seconds)}
                                readOnly={voiceBusy}
                                enterKeyHint="send"
                                autoCorrect="off"
                                aria-describedby={voiceLine ? 'voice-line' : undefined}
                                onChange={(event) => setDraft(event.target.value)}
                                onKeyDown={(event) => {
                                    // A held key repeats, and Enter also ends an IME composition.
                                    if (event.key !== 'Enter' || event.repeat || event.nativeEvent.isComposing) return;
                                    if (!voiceBusy && draft.trim().length > 0) sendTyped();
                                }}
                                iconAfter={voiceAvailable ? (
                                    <IconButton
                                        size="xsmall"
                                        variant={voice.state === 'recording' ? 'primary' : 'ghost'}
                                        className={s.mic}
                                        aria-label={MIC_LABEL[voice.state]}
                                        aria-pressed={voice.state === 'recording'}
                                        loading={voice.state === 'recognizing'}
                                        disabled={!imagesReady}
                                        onClick={voice.toggle}
                                    >
                                        {voice.state === 'starting' ? <Spinner size={16} appearance="neutral-themed" />
                                            : voice.state === 'recording' ? <IconStop size={16} /> : <IconMic size={20} />}
                                    </IconButton>
                                ) : undefined}
                            />
                            {voiceLine ? (
                                <Typography.Text id="voice-line" variant="description" color="secondary" className={s.voiceLine} role="status">
                                    {voiceLine}
                                </Typography.Text>
                            ) : null}
                            <Button
                                size="medium"
                                variant="secondary"
                                stretched
                                disabled={!imagesReady || voiceBusy || draft.trim().length === 0}
                                onClick={sendTyped}
                            >
                                Ответить
                            </Button>
                        </>
                    ) : null}

                    {card.kind === 'input' && check ? (
                        <Button size="medium" variant="secondary" stretched disabled>
                            Проверяю ответ
                        </Button>
                    ) : null}

                    {card.kind === 'flip' ? (
                        <Button
                            size="medium"
                            variant="secondary"
                            stretched
                            disabled={!imagesReady || opening}
                            onClick={openFlip}
                        >
                            {opening ? 'Открываю ответ' : 'Показать ответ'}
                        </Button>
                    ) : null}
                </Flex>
            )}
        </Screen>
    );
}

const MIC_LABEL: Record<VoiceState, string> = {
    idle: 'Ответить голосом',
    starting: 'Не включать микрофон',
    recording: 'Остановить запись',
    recognizing: 'Распознаю ответ',
};

/** The field tells what the microphone is doing while it is busy. */
function voicePlaceholder(state: VoiceState, seconds: number): string {
    switch (state) {
        case 'starting':
            return 'Включаю микрофон';
        case 'recording':
            return `Говорите · 0:${String(seconds).padStart(2, '0')}`;
        case 'recognizing':
            return 'Распознаю';
        default:
            return 'Ваш ответ';
    }
}

export default Feed;
