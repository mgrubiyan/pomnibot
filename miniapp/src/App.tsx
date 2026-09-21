import { useEffect, useState } from 'react';
import AddNote from './screens/AddNote';
import CardEdit from './screens/CardEdit';
import CardIssue from './screens/CardIssue';
import Feed from './screens/Feed';
import Home from './screens/Home';
import JoinSet from './screens/JoinSet';
import SetScreen from './screens/SetScreen';
import Share from './screens/Share';
import { mockCards, mockToday } from './mocks';
import type { CardIssueReason } from './types';
import { applyPatch, visibleCards, type CardPatch } from './utils/cards';

/** No router yet: browser history is not used. */
type Screen =
    | { name: 'home' }
    | { name: 'set'; setId: string }
    | { name: 'feed'; setId?: string }
    | { name: 'share'; setId: string }
    | { name: 'add' }
    | { name: 'join' }
    | { name: 'card-issue'; cardId: string }
    | { name: 'card-edit'; cardId: string };

/** What to report on the set screen after an action on a card. */
type Toast = { kind: 'removed'; cardId: string } | { kind: 'edited' };

function App() {
    const [screen, setScreen] = useState<Screen>({ name: 'home' });

    useEffect(() => {
        window.WebApp?.ready?.();
    }, []);

    // Edits and deletions live in memory for now: there is no backend,
    // and browser storage is off limits.
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

    const home = (
        <Home
            onStart={() => go({ name: 'feed' })}
            onOpenSet={(setId) => go({ name: 'set', setId })}
            onAddNote={() => go({ name: 'add' })}
            onJoinSet={() => go({ name: 'join' })}
            removedSetIds={removedSetIds}
        />
    );

    const openSet = (setId: string) => go({ name: 'set', setId });

    const removeSet = (setId: string) => {
        setRemovedSetIds((current) =>
            current.includes(setId) ? current : [...current, setId],
        );
        goHome();
    };

    const cardOf = (cardId: string) => {
        const found = mockCards.find((card) => card.id === cardId);
        return found ? applyPatch(found, cardPatches[cardId]) : undefined;
    };

    const titleOfSet = (setId: string) =>
        mockToday.sets.find((set) => set.id === setId)?.title ?? 'Набор';

    const removeCard = (cardId: string, setId: string, reason?: CardIssueReason) => {
        // TODO: send the reported reason to the backend — the concept counts it.
        void reason;
        setRemovedCardIds((current) =>
            current.includes(cardId) ? current : [...current, cardId],
        );
        setScreen({ name: 'set', setId });
        setToast({ kind: 'removed', cardId });
    };

    const undoRemoveCard = () => {
        if (toast?.kind !== 'removed') {
            return;
        }
        const { cardId } = toast;
        setRemovedCardIds((current) => current.filter((id) => id !== cardId));
        setToast(null);
    };

    if (screen.name === 'add') {
        return <AddNote onBack={goHome} />;
    }

    if (screen.name === 'join') {
        return <JoinSet onBack={goHome} onOpenSet={openSet} />;
    }

    if (screen.name === 'card-issue' || screen.name === 'card-edit') {
        const card = cardOf(screen.cardId);

        // The card was deleted while the screen was open.
        if (!card) {
            return home;
        }

        if (screen.name === 'card-issue') {
            return (
                <CardIssue
                    card={card}
                    setTitle={titleOfSet(card.setId)}
                    onBack={() => openSet(card.setId)}
                    onEdit={() => go({ name: 'card-edit', cardId: card.id })}
                    onRemove={(reason) => removeCard(card.id, card.setId, reason)}
                />
            );
        }

        return (
            <CardEdit
                card={card}
                setTitle={titleOfSet(card.setId)}
                onBack={() => go({ name: 'card-issue', cardId: card.id })}
                onSave={(patch) => {
                    setCardPatches((current) => ({ ...current, [card.id]: patch }));
                    setScreen({ name: 'set', setId: card.setId });
                    setToast({ kind: 'edited' });
                }}
                onRemove={() => removeCard(card.id, card.setId)}
            />
        );
    }

    if (screen.name === 'feed') {
        const { setId } = screen;
        return (
            <Feed
                key={setId ?? 'all'}
                setId={setId}
                setTitle={setId ? titleOfSet(setId) : undefined}
                removedCardIds={removedCardIds}
                cardPatches={cardPatches}
                onExit={goHome}
                onReportCard={(cardId) => go({ name: 'card-issue', cardId })}
                onShare={setId ? () => go({ name: 'share', setId }) : undefined}
            />
        );
    }

    if (screen.name === 'share') {
        // Back leads to the set: the result screen lived inside the feed
        // and is gone once we leave it.
        return (
            <Share
                setId={screen.setId}
                setTitle={titleOfSet(screen.setId)}
                cardsCount={visibleCards(mockCards, screen.setId, removedCardIds, cardPatches).length}
                onBack={() => openSet(screen.setId)}
            />
        );
    }

    if (screen.name === 'set') {
        return (
            <SetScreen
                key={screen.setId}
                setId={screen.setId}
                cards={visibleCards(mockCards, screen.setId, removedCardIds, cardPatches)}
                toast={toast}
                onBack={goHome}
                onStart={(setId) => go({ name: 'feed', setId })}
                onRemove={removeSet}
                onOpenCard={(cardId) => go({ name: 'card-issue', cardId })}
                onUndoRemoveCard={undoRemoveCard}
            />
        );
    }

    return home;
}

export default App;
