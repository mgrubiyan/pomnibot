import { useCallback, useEffect, useState } from 'react';
import AddNote from './screens/AddNote';
import CardEdit from './screens/CardEdit';
import CardIssue from './screens/CardIssue';
import Feed from './screens/Feed';
import Home from './screens/Home';
import JoinSet from './screens/JoinSet';
import SetScreen from './screens/SetScreen';
import Share from './screens/Share';
import Leaderboard from './screens/Leaderboard';
import { api } from './api';
import type { AnswerResult, Card, CardIssueReason, TodayData, User } from './types';
import type { CardPatch } from './utils/cards';

/** No router yet: browser history is not used. */
type Screen =
    | { name: 'home' }
    | { name: 'set'; setId: string }
    | {
          name: 'feed';
          setId?: string;
          setTitle?: string;
          initialIndex?: number;
          initialResults?: AnswerResult[];
      }
    | { name: 'share'; setId: string; setTitle?: string }
    | { name: 'leaderboard'; setId: string; setTitle?: string }
    | { name: 'add' }
    | { name: 'join' }
    | {
          name: 'card-issue';
          card: Card;
          setTitle?: string;
          isOwner?: boolean;
          fromFeed?: boolean;
          feedSetId?: string;
          feedIndex?: number;
          feedResults?: AnswerResult[];
      }
    | {
          name: 'card-edit';
          card: Card;
          setTitle?: string;
          isOwner?: boolean;
          fromFeed?: boolean;
          feedSetId?: string;
          feedIndex?: number;
          feedResults?: AnswerResult[];
      };

/** What to report on the set screen after an action on a card. */
type Toast =
    | { kind: 'removed'; cardId: string }
    | { kind: 'edited' }
    | { kind: 'reported' };

function App() {
    const [screen, setScreen] = useState<Screen>({ name: 'home' });
    const [currentUser, setCurrentUser] = useState<User | null>(null);

    useEffect(() => {
        window.WebApp?.ready?.();
        api.GET('/').then(({ data }) => {
            if (data?.user) {
                setCurrentUser(data.user);
            }
        }).catch(() => {
            // fallback
        });
    }, []);

    const [removedSetIds, setRemovedSetIds] = useState<string[]>([]);
    const [removedCardIds, setRemovedCardIds] = useState<string[]>([]);
    const [cardPatches, setCardPatches] = useState<Record<string, CardPatch>>({});
    const [toast, setToast] = useState<Toast | null>(null);

    const handleTodayLoaded = useCallback((today: TodayData) => {
        setCurrentUser(today.user);
    }, []);

    // The notice belongs to a single action, so any navigation clears it;
    // only deleting and saving raise it.
    const go = (next: Screen) => {
        setToast(null);
        setScreen(next);
    };

    const goHome = () => go({ name: 'home' });

    const openSet = (setId: string) => go({ name: 'set', setId });

    const removeSet = async (setId: string) => {
        try {
            await api.DELETE('/sets/{setId}', {
                params: { path: { setId } },
            });
        } catch (err) {
            console.error('Failed to delete set:', err);
        }
        setRemovedSetIds((current) =>
            current.includes(setId) ? current : [...current, setId],
        );
        goHome();
    };

    const removeCard = async (
        cardId: string,
        setId: string,
        reason?: CardIssueReason,
        fromFeed?: boolean,
        feedSetId?: string,
        feedSetTitle?: string,
        feedIndex?: number,
        feedResults?: AnswerResult[],
    ) => {
        try {
            if (reason) {
                await api.POST('/cards/{cardId}/issue', {
                    params: { path: { cardId } },
                    body: { reason },
                });
            }
            await api.DELETE('/cards/{cardId}', {
                params: { path: { cardId } },
            });
        } catch (err) {
            console.error('Failed to delete card:', err);
        }

        setRemovedCardIds((current) =>
            current.includes(cardId) ? current : [...current, cardId],
        );

        if (fromFeed) {
            setScreen({
                name: 'feed',
                setId: feedSetId,
                setTitle: feedSetTitle,
                initialIndex: feedIndex,
                initialResults: feedResults,
            });
        } else {
            setScreen({ name: 'set', setId });
            setToast({ kind: 'removed', cardId });
        }
    };

    const reportCardIssue = async (
        cardId: string,
        reason: CardIssueReason,
        setId: string,
        fromFeed?: boolean,
        feedSetId?: string,
        feedSetTitle?: string,
        feedIndex?: number,
        feedResults?: AnswerResult[],
    ) => {
        try {
            await api.POST('/cards/{cardId}/issue', {
                params: { path: { cardId } },
                body: { reason },
            });
        } catch (err) {
            console.error('Failed to report card issue:', err);
        }

        if (fromFeed) {
            setRemovedCardIds((current) =>
                current.includes(cardId) ? current : [...current, cardId],
            );
            setScreen({
                name: 'feed',
                setId: feedSetId,
                setTitle: feedSetTitle,
                initialIndex: feedIndex,
                initialResults: feedResults,
            });
        } else {
            setScreen({ name: 'set', setId });
            setToast({ kind: 'reported' });
        }
    };

    const saveCardEdit = async (
        cardId: string,
        setId: string,
        patch: CardPatch,
        fromFeed?: boolean,
        feedSetId?: string,
        feedSetTitle?: string,
        feedIndex?: number,
        feedResults?: AnswerResult[],
    ) => {
        try {
            await api.PUT('/cards/{cardId}', {
                params: { path: { cardId } },
                body: {
                    kind: patch.kind,
                    question: patch.question,
                    answer: patch.answer,
                    options: patch.options,
                    table: patch.table,
                    explanation: patch.explanation,
                    topic: patch.topic,
                    sourceQuote: patch.sourceQuote,
                    sourceRef: patch.sourceRef,
                },
            });
        } catch (err) {
            console.error('Failed to update card:', err);
        }

        setCardPatches((current) => ({ ...current, [cardId]: patch }));
        if (fromFeed) {
            setScreen({
                name: 'feed',
                setId: feedSetId,
                setTitle: feedSetTitle,
                initialIndex: feedIndex,
                initialResults: feedResults,
            });
        } else {
            setScreen({ name: 'set', setId });
            setToast({ kind: 'edited' });
        }
    };

    const openCardIssueFromFeed = async (
        card: Card,
        feedSetId?: string,
        feedSetTitle?: string,
        feedIndex?: number,
        feedResults?: AnswerResult[],
    ) => {
        let isOwner = false;
        let setTitle: string | undefined = feedSetTitle;
        try {
            const { data } = await api.GET('/sets/{setId}', {
                params: { path: { setId: card.setId } },
            });
            if (data?.title) {
                setTitle = data.title;
            }
            if (data?.author && currentUser && data.author.id === currentUser.id) {
                isOwner = true;
            }
        } catch {
            // fallback
        }
        go({
            name: 'card-issue',
            card,
            setTitle,
            isOwner,
            fromFeed: true,
            feedSetId,
            feedIndex,
            feedResults,
        });
    };

    const home = (
        <Home
            onStart={() => go({ name: 'feed' })}
            onOpenSet={(setId) => go({ name: 'set', setId })}
            onAddNote={() => go({ name: 'add' })}
            onJoinSet={() => go({ name: 'join' })}
            removedSetIds={removedSetIds}
            onTodayLoaded={handleTodayLoaded}
        />
    );

    if (screen.name === 'add') {
        return <AddNote onBack={goHome} />;
    }

    if (screen.name === 'join') {
        return <JoinSet onBack={goHome} onOpenSet={openSet} />;
    }

    if (screen.name === 'card-issue' || screen.name === 'card-edit') {
        const {
            card,
            setTitle,
            isOwner = false,
            fromFeed,
            feedSetId,
            feedIndex,
            feedResults,
        } = screen;

        if (screen.name === 'card-issue') {
            return (
                <CardIssue
                    card={card}
                    setTitle={setTitle ?? 'Набор'}
                    isOwner={isOwner}
                    onBack={() => {
                        if (fromFeed) {
                            go({
                                name: 'feed',
                                setId: feedSetId,
                                setTitle: feedSetId ? setTitle : undefined,
                                initialIndex: feedIndex,
                                initialResults: feedResults,
                            });
                        } else {
                            openSet(card.setId);
                        }
                    }}
                    onEdit={() => {
                        if (!isOwner) {
                            return;
                        }
                        go({
                            name: 'card-edit',
                            card,
                            setTitle,
                            isOwner: true,
                            fromFeed,
                            feedSetId,
                            feedIndex,
                            feedResults,
                        });
                    }}
                    onRemove={(reason) =>
                        removeCard(
                            card.id,
                            card.setId,
                            reason,
                            fromFeed,
                            feedSetId,
                            setTitle,
                            feedIndex,
                            feedResults,
                        )
                    }
                    onReport={(reason) =>
                        reportCardIssue(
                            card.id,
                            reason,
                            card.setId,
                            fromFeed,
                            feedSetId,
                            setTitle,
                            feedIndex,
                            feedResults,
                        )
                    }
                />
            );
        }

        if (!isOwner) {
            if (fromFeed) {
                go({
                    name: 'feed',
                    setId: feedSetId,
                    setTitle: feedSetId ? setTitle : undefined,
                    initialIndex: feedIndex,
                    initialResults: feedResults,
                });
            } else {
                openSet(card.setId);
            }
            return null;
        }

        return (
            <CardEdit
                card={card}
                setTitle={setTitle ?? 'Набор'}
                onBack={() =>
                    go({
                        name: 'card-issue',
                        card,
                        setTitle,
                        isOwner: true,
                        fromFeed,
                        feedSetId,
                        feedIndex,
                        feedResults,
                    })
                }
                onSave={(patch) =>
                    saveCardEdit(
                        card.id,
                        card.setId,
                        patch,
                        fromFeed,
                        feedSetId,
                        setTitle,
                        feedIndex,
                        feedResults,
                    )
                }
                onRemove={() =>
                    removeCard(
                        card.id,
                        card.setId,
                        undefined,
                        fromFeed,
                        feedSetId,
                        setTitle,
                        feedIndex,
                        feedResults,
                    )
                }
            />
        );
    }

    if (screen.name === 'feed') {
        const { setId, setTitle, initialIndex, initialResults } = screen;
        return (
            <Feed
                key={setId ?? 'all'}
                setId={setId}
                setTitle={setTitle}
                initialIndex={initialIndex}
                initialResults={initialResults}
                removedCardIds={removedCardIds}
                cardPatches={cardPatches}
                onExit={setId ? () => openSet(setId) : goHome}
                onBack={setId ? () => openSet(setId) : goHome}
                onReportCard={(card, idx, res) =>
                    openCardIssueFromFeed(card, setId, setTitle, idx, res)
                }
                onShare={setId ? () => go({ name: 'share', setId }) : undefined}
            />
        );
    }

    if (screen.name === 'share') {
        return (
            <Share
                setId={screen.setId}
                setTitle={screen.setTitle}
                onBack={() => openSet(screen.setId)}
            />
        );
    }

    if (screen.name === 'leaderboard') {
        return (
            <Leaderboard
                setId={screen.setId}
                setTitle={screen.setTitle}
                onBack={() => openSet(screen.setId)}
            />
        );
    }

    if (screen.name === 'set') {
        return (
            <SetScreen
                key={screen.setId}
                setId={screen.setId}
                toast={toast}
                currentUser={currentUser}
                onBack={goHome}
                onStart={(setId, setTitle) => go({ name: 'feed', setId, setTitle })}
                onRemove={removeSet}
                onOpenCard={(card, isOwner, setTitle) =>
                    go({
                        name: 'card-issue',
                        card,
                        setTitle,
                        isOwner,
                        fromFeed: false,
                        feedSetId: undefined,
                    })
                }
                onOpenLeaderboard={(setId, setTitle) =>
                    go({
                        name: 'leaderboard',
                        setId,
                        setTitle,
                    })
                }
            />
        );
    }

    return home;
}

export default App;
