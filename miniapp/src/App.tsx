import { useEffect, useState } from 'react';
import AddNote from './screens/AddNote';
import CardEdit from './screens/CardEdit';
import CardIssue from './screens/CardIssue';
import Feed from './screens/Feed';
import Home from './screens/Home';
import JoinSet from './screens/JoinSet';
import SetScreen from './screens/SetScreen';
import Share from './screens/Share';
import { api } from './api';
import type { Card, CardIssueReason } from './types';
import type { CardPatch } from './utils/cards';

/** No router yet: browser history is not used. */
type Screen =
    | { name: 'home' }
    | { name: 'set'; setId: string }
    | { name: 'feed'; setId?: string }
    | { name: 'share'; setId: string; setTitle?: string }
    | { name: 'add' }
    | { name: 'join' }
    | {
          name: 'card-issue';
          card: Card;
          setTitle?: string;
          isOwner?: boolean;
          fromFeed?: boolean;
          feedSetId?: string;
      }
    | {
          name: 'card-edit';
          card: Card;
          setTitle?: string;
          isOwner?: boolean;
          fromFeed?: boolean;
          feedSetId?: string;
      };

/** What to report on the set screen after an action on a card. */
type Toast = { kind: 'removed'; cardId: string } | { kind: 'edited' };

function App() {
    const [screen, setScreen] = useState<Screen>({ name: 'home' });

    useEffect(() => {
        window.WebApp?.ready?.();
    }, []);

    const [removedSetIds, setRemovedSetIds] = useState<string[]>([]);
    const [removedCardIds, setRemovedCardIds] = useState<string[]>([]);
    const [cardPatches, setCardPatches] = useState<Record<string, CardPatch>>({});
    const [toast, setToast] = useState<Toast | null>(null);

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
            setScreen({ name: 'feed', setId: feedSetId });
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
            setScreen({ name: 'feed', setId: feedSetId });
        } else {
            setScreen({ name: 'set', setId });
        }
    };

    const saveCardEdit = async (
        cardId: string,
        setId: string,
        patch: CardPatch,
        fromFeed?: boolean,
        feedSetId?: string,
    ) => {
        try {
            await api.PUT('/cards/{cardId}', {
                params: { path: { cardId } },
                body: {
                    question: patch.question,
                    answer: patch.answer,
                    options: patch.options,
                },
            });
        } catch (err) {
            console.error('Failed to update card:', err);
        }

        setCardPatches((current) => ({ ...current, [cardId]: patch }));
        if (fromFeed) {
            setScreen({ name: 'feed', setId: feedSetId });
        } else {
            setScreen({ name: 'set', setId });
            setToast({ kind: 'edited' });
        }
    };

    const openCardIssueFromFeed = async (card: Card, feedSetId?: string) => {
        let isOwner = true;
        try {
            const { data } = await api.GET('/sets/{setId}', {
                params: { path: { setId: card.setId } },
            });
            if (data?.authorName) {
                isOwner = false;
            }
        } catch {
            // fallback
        }
        go({
            name: 'card-issue',
            card,
            isOwner,
            fromFeed: true,
            feedSetId,
        });
    };

    const home = (
        <Home
            onStart={() => go({ name: 'feed' })}
            onOpenSet={(setId) => go({ name: 'set', setId })}
            onAddNote={() => go({ name: 'add' })}
            onJoinSet={() => go({ name: 'join' })}
            removedSetIds={removedSetIds}
        />
    );

    if (screen.name === 'add') {
        return <AddNote onBack={goHome} />;
    }

    if (screen.name === 'join') {
        return <JoinSet onBack={goHome} onOpenSet={openSet} />;
    }

    if (screen.name === 'card-issue' || screen.name === 'card-edit') {
        const { card, setTitle, isOwner, fromFeed, feedSetId } = screen;

        if (screen.name === 'card-issue') {
            return (
                <CardIssue
                    card={card}
                    setTitle={setTitle ?? 'Набор'}
                    isOwner={isOwner}
                    onBack={() => {
                        if (fromFeed) {
                            go({ name: 'feed', setId: feedSetId });
                        } else {
                            openSet(card.setId);
                        }
                    }}
                    onEdit={() =>
                        go({
                            name: 'card-edit',
                            card,
                            setTitle,
                            fromFeed,
                            feedSetId,
                        })
                    }
                    onRemove={(reason) =>
                        removeCard(card.id, card.setId, reason, fromFeed, feedSetId)
                    }
                    onReport={(reason) =>
                        reportCardIssue(card.id, reason, card.setId, fromFeed, feedSetId)
                    }
                />
            );
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
                    })
                }
                onSave={(patch) =>
                    saveCardEdit(card.id, card.setId, patch, fromFeed, feedSetId)
                }
                onRemove={() =>
                    removeCard(card.id, card.setId, undefined, fromFeed, feedSetId)
                }
            />
        );
    }

    if (screen.name === 'feed') {
        const { setId } = screen;
        return (
            <Feed
                key={setId ?? 'all'}
                setId={setId}
                removedCardIds={removedCardIds}
                cardPatches={cardPatches}
                onExit={goHome}
                onBack={setId ? () => openSet(setId) : goHome}
                onReportCard={(card) => openCardIssueFromFeed(card, setId)}
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

    if (screen.name === 'set') {
        return (
            <SetScreen
                key={screen.setId}
                setId={screen.setId}
                toast={toast}
                onBack={goHome}
                onStart={(setId) => go({ name: 'feed', setId })}
                onRemove={removeSet}
                onOpenCard={(card) =>
                    go({
                        name: 'card-issue',
                        card,
                        fromFeed: false,
                        feedSetId: undefined,
                    })
                }
            />
        );
    }

    return home;
}

export default App;
