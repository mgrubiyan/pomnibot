import { useCallback, useEffect, useState } from 'react';
import AddNote from './screens/AddNote';
import CardEdit from './screens/CardEdit';
import Feed from './screens/Feed';
import Home from './screens/Home';
import JoinSet from './screens/JoinSet';
import SetScreen from './screens/SetScreen';
import Share from './screens/Share';
import Leaderboard from './screens/Leaderboard';
import { api } from './api';
import { startParam } from './max/bridge';
import type { AnswerResult, Card, TodayData, User } from './types';
import type { CardPatch } from './utils/cards';
import { codeFromStartParam } from './utils/code';

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
    | { name: 'join'; code?: string }
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
    | { kind: 'edited' };

/** An invite link opens the code screen with the code filled in. */
function startScreen(): Screen {
    const code = codeFromStartParam(startParam());
    return code ? { name: 'join', code } : { name: 'home' };
}

function App() {
    const [screen, setScreen] = useState<Screen>(startScreen);
    const [currentUser, setCurrentUser] = useState<User | null>(null);

    useEffect(() => {
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
        fromFeed?: boolean,
        feedSetId?: string,
        feedSetTitle?: string,
        feedIndex?: number,
        feedResults?: AnswerResult[],
    ) => {
        try {
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
        return <JoinSet initialCode={screen.code} onBack={goHome} onOpenSet={openSet} />;
    }

    if (screen.name === 'card-edit') {
        const {
            card,
            setTitle,
            isOwner = false,
            fromFeed,
            feedSetId,
            feedIndex,
            feedResults,
        } = screen;

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
                onOpenCard={(card, isOwner, setTitle) => {
                    if (!isOwner) {
                        return;
                    }
                    go({
                        name: 'card-edit',
                        card,
                        setTitle,
                        isOwner: true,
                        fromFeed: false,
                        feedSetId: undefined,
                    });
                }}
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
