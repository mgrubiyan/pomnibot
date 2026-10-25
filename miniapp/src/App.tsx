import { lazy, useCallback, useEffect, useState } from 'react';
import AddNote from './screens/AddNote';
import CreateSet from './screens/CreateSet';
import Home from './screens/Home';
import JoinSet from './screens/JoinSet';
import SetScreen from './screens/SetScreen';
import SetWarnings from './screens/SetWarnings';
import Share from './screens/Share';
import Leaderboard from './screens/Leaderboard';
import WhyItWorks from './screens/WhyItWorks';
import { api } from './api';
import { config } from './config';
import { useProfile } from './components/profileContext';
import type { Card, Profile as ProfileData, TodayData, User } from './types';
import type { CardIssueReport } from './utils/issues';
import type { CardPatch } from './utils/cards';
import type { FeedResume } from './utils/feed';

const ManualCreate = lazy(() => import('./screens/ManualCreate'));
const CardEdit = lazy(() => import('./screens/CardEdit'));
const AdminIssues = lazy(() => import('./screens/AdminIssues'));
const CardIssue = lazy(() => import('./screens/CardIssue'));
const Feed = lazy(() => import('./screens/Feed'));
const SetCards = lazy(() => import('./screens/SetCards'));
const SetIssues = lazy(() => import('./screens/SetIssues'));
const SetMistakes = lazy(() => import('./screens/SetMistakes'));
const Profile = lazy(() => import('./screens/Profile'));

/** No router yet: browser history is not used. */
type Screen =
    | { name: 'home' }
    | { name: 'set'; setId: string }
    | { name: 'set-cards'; setId: string }
    | { name: 'set-issues'; setId: string }
    | { name: 'set-warnings'; setId: string }
    | { name: 'admin-issues' }
    | {
          name: 'feed';
          setId?: string;
          setTitle?: string;
          early?: boolean;
          resume?: FeedResume;
      }
    | { name: 'share'; setId: string; setTitle?: string }
    | { name: 'leaderboard'; setId: string; setTitle?: string }
    | { name: 'set-mistakes'; setId: string; setTitle?: string }
    | { name: 'why' }
    | { name: 'profile'; returnTo: Screen }
    | { name: 'add' }
    | { name: 'create-method' }
    | { name: 'manual'; setId?: string; setTitle?: string; factId?: string }
    | { name: 'join' }
    | {
          name: 'card-issue';
          card: Card;
          setTitle?: string;
          isOwner: boolean;
          feedSetId?: string;
          feedEarly?: boolean;
          feedResume: FeedResume;
      }
    | {
          name: 'card-edit';
          card: Card;
          setTitle?: string;
          isOwner?: boolean;
          fromFeed?: boolean;
          feedSetId?: string;
          feedEarly?: boolean;
          feedResume?: FeedResume;
          /** Where to go after the edit when it did not start in the feed. */
          returnTo?: Screen;
      };

/** What to report on the cards screen after an action on a card. */
type Toast =
    | { kind: 'removed'; cardId: string }
    | { kind: 'edited' };

function App() {
    const [screen, setScreen] = useState<Screen>({ name: 'home' });
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

    // Whether set authors may see the user decides the hint on others' sets.
    const profileState = useProfile();
    // Without a signature the server keeps no profile of its own, except
    // locally, where the browser account stands for the developer.
    const profile = profileState && (profileState.profile.platform || config.environment === 'local')
        ? profileState.profile
        : null;
    const setProfile = (next: ProfileData) => profileState?.setProfile(next);

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
        feedResume?: FeedResume,
        returnTo?: Screen,
        feedEarly?: boolean,
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
                early: feedEarly,
                setTitle: feedSetTitle,
                resume: feedResume,
            });
        } else if (returnTo) {
            setScreen(returnTo);
        } else {
            setScreen({ name: 'set-cards', setId });
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
        feedResume?: FeedResume,
        returnTo?: Screen,
        feedEarly?: boolean,
    ) => {
        try {
            const { error } = await api.PUT('/cards/{cardId}', {
                params: { path: { cardId } },
                body: {
                    kind: patch.kind,
                    question: patch.question,
                    answer: patch.answer,
                    options: patch.options,
                    table: patch.table,
                    explanation: patch.explanation,
                    sourceQuote: patch.sourceQuote,
                    sourceRef: patch.sourceRef,
                },
            });
            if (error) throw new Error('Failed to update card');
        } catch (err) {
            console.error('Failed to update card:', err);
            throw err;
        }

        setCardPatches((current) => ({ ...current, [cardId]: patch }));
        if (fromFeed) {
            setScreen({
                name: 'feed',
                setId: feedSetId,
                early: feedEarly,
                setTitle: feedSetTitle,
                resume: feedResume,
            });
        } else if (returnTo) {
            setScreen(returnTo);
        } else {
            setScreen({ name: 'set-cards', setId });
            setToast({ kind: 'edited' });
        }
    };

    // Throws when the complaint was not saved, so the screen can say so
    // instead of losing it. The status decides: an error with an empty
    // body, such as a proxy's 502, comes back without an `error`.
    const reportCardIssue = async (cardId: string, { reason, comment }: CardIssueReport) => {
        const { response } = await api.POST('/cards/{cardId}/issue', {
            params: { path: { cardId } },
            body: comment ? { reason, comment } : { reason },
        });
        if (!response.ok) {
            throw new Error(`Failed to report card issue: ${response.status}`);
        }
    };

    // The author gets other actions than a reader, so without the set or
    // the user the screen is not opened: a guess would treat the author as a reader.
    const openCardIssue = async (
        card: Card,
        feedSetId: string | undefined,
        feedResume: FeedResume,
        feedEarly?: boolean,
    ): Promise<boolean> => {
        try {
            const [{ data: set }, user] = await Promise.all([
                api.GET('/sets/{setId}', { params: { path: { setId: card.setId } } }),
                currentUser ?? api.GET('/').then(({ data }) => data?.user ?? null),
            ]);
            if (!set || !user) {
                return false;
            }
            setCurrentUser(user);
            go({
                name: 'card-issue',
                card,
                setTitle: set.title,
                isOwner: set.author.id === user.id,
                feedSetId,
                feedEarly,
                feedResume,
            });
            return true;
        } catch {
            return false;
        }
    };

    const home = (
        <Home
            onStart={() => go({ name: 'feed' })}
            onOpenSet={(setId) => go({ name: 'set', setId })}
            onAddNote={() => go({ name: 'create-method' })}
            onJoinSet={() => go({ name: 'join' })}
            onOpenWhy={() => go({ name: 'why' })}
            onOpenAdminIssues={() => go({ name: 'admin-issues' })}
            onOpenProfile={profile ? () => go({ name: 'profile', returnTo: { name: 'home' } }) : undefined}
            removedSetIds={removedSetIds}
            onTodayLoaded={handleTodayLoaded}
        />
    );

    if (screen.name === 'profile' && profile) {
        return <Profile profile={profile} onChange={setProfile} onBack={() => go(screen.returnTo)} />;
    }

    if (screen.name === 'create-method') {
        return <CreateSet onFromNotes={() => go({ name: 'add' })} onManual={() => go({ name: 'manual' })} onBack={goHome} />;
    }

    if (screen.name === 'manual') {
        return <ManualCreate setId={screen.setId} setTitle={screen.setTitle} factId={screen.factId}
            onExit={() => screen.setId ? go({ name: 'set-cards', setId: screen.setId }) : goHome()}
            onDone={setId => go({ name: 'set-cards', setId })} />;
    }

    if (screen.name === 'add') {
        return <AddNote onBack={goHome} />;
    }

    if (screen.name === 'join') {
        return <JoinSet onBack={goHome} onOpenSet={openSet} />;
    }

    if (screen.name === 'card-issue') {
        const { card, setTitle, isOwner, feedSetId, feedEarly, feedResume } = screen;
        const feedTitle = feedSetId ? setTitle : undefined;
        const backToFeed = () =>
            go({
                name: 'feed',
                setId: feedSetId,
                early: feedEarly,
                setTitle: feedTitle,
                resume: feedResume,
            });
        return (
            <CardIssue
                card={card}
                setTitle={setTitle ?? 'Набор'}
                isOwner={isOwner}
                onBack={backToFeed}
                onEdit={async (report) => {
                    await reportCardIssue(card.id, report);
                    go({
                        name: 'card-edit',
                        card,
                        setTitle,
                        isOwner: true,
                        fromFeed: true,
                        feedSetId,
                        feedEarly,
                        feedResume,
                    });
                }}
                onRemove={async (report) => {
                    await reportCardIssue(card.id, report);
                    await removeCard(card.id, card.setId, true, feedSetId, feedTitle, feedResume, undefined, feedEarly);
                }}
                onReport={async (report) => {
                    await reportCardIssue(card.id, report);
                    setRemovedCardIds((current) =>
                        current.includes(card.id) ? current : [...current, card.id],
                    );
                    backToFeed();
                }}
            />
        );
    }

    if (screen.name === 'card-edit') {
        const {
            card,
            setTitle,
            isOwner = false,
            fromFeed,
            feedSetId,
            feedEarly,
            feedResume,
            returnTo,
        } = screen;

        if (!isOwner) {
            if (fromFeed) {
                go({
                    name: 'feed',
                    setId: feedSetId,
                    early: feedEarly,
                    setTitle: feedSetId ? setTitle : undefined,
                    resume: feedResume,
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
                            early: feedEarly,
                            setTitle: feedSetId ? setTitle : undefined,
                            resume: feedResume,
                        });
                    } else {
                        go(returnTo ?? { name: 'set-cards', setId: card.setId });
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
                        feedResume,
                        returnTo,
                        feedEarly,
                    )
                }
                onRemove={() =>
                    removeCard(
                        card.id,
                        card.setId,
                        fromFeed,
                        feedSetId,
                        setTitle,
                        feedResume,
                        returnTo,
                        feedEarly,
                    )
                }
            />
        );
    }

    if (screen.name === 'feed') {
        const { setId, setTitle, resume, early } = screen;
        return (
            <Feed
                key={`${setId ?? 'all'}-${early ? 'early' : 'due'}`}
                setId={setId}
                setTitle={setTitle}
                early={early}
                resume={resume}
                removedCardIds={removedCardIds}
                cardPatches={cardPatches}
                onExit={setId ? () => openSet(setId) : goHome}
                onBack={setId ? () => openSet(setId) : goHome}
                onReportCard={(card, feedResume) => openCardIssue(card, setId, feedResume, early)}
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

    if (screen.name === 'set-mistakes') {
        return (
            <SetMistakes
                setId={screen.setId}
                setTitle={screen.setTitle}
                onBack={() => openSet(screen.setId)}
            />
        );
    }

    if (screen.name === 'why') {
        return <WhyItWorks onBack={goHome} />;
    }

    if (screen.name === 'admin-issues') {
        return <AdminIssues onBack={goHome} />;
    }

    if (screen.name === 'set-warnings') {
        return (
            <SetWarnings
                key={screen.setId}
                setId={screen.setId}
                currentUser={currentUser}
                onBack={() => openSet(screen.setId)}
            />
        );
    }

    if (screen.name === 'set-issues') {
        const issues: Screen = { name: 'set-issues', setId: screen.setId };
        return (
            <SetIssues
                key={screen.setId}
                setId={screen.setId}
                onBack={() => openSet(screen.setId)}
                onEditCard={(card, setTitle) =>
                    go({ name: 'card-edit', card, setTitle, isOwner: true, returnTo: issues })
                }
            />
        );
    }

    if (screen.name === 'set-cards') {
        return (
            <SetCards
                key={screen.setId}
                setId={screen.setId}
                currentUser={currentUser}
                toast={toast}
                onAddCard={(setTitle, factId) => go({ name: 'manual', setId: screen.setId, setTitle, factId })}
                onBack={() => openSet(screen.setId)}
                onOpenCard={(card, setTitle) =>
                    go({
                        name: 'card-edit',
                        card,
                        setTitle,
                        isOwner: true,
                        fromFeed: false,
                    })
                }
            />
        );
    }

    if (screen.name === 'set') {
        return (
            <SetScreen
                key={screen.setId}
                setId={screen.setId}
                currentUser={currentUser}
                hidden={profile ? !profile.dataConsent : false}
                onOpenProfile={() => go({ name: 'profile', returnTo: { name: 'set', setId: screen.setId } })}
                onBack={goHome}
                onStart={(setId, setTitle, early) => go({ name: 'feed', setId, setTitle, early })}
                onRemove={removeSet}
                onOpenCards={(setId) => go({ name: 'set-cards', setId })}
                onOpenWarnings={(setId) => go({ name: 'set-warnings', setId })}
                onOpenIssues={(setId) => go({ name: 'set-issues', setId })}
                onShare={(setId, setTitle) => go({ name: 'share', setId, setTitle })}
                onOpenMistakes={(setId, setTitle) => go({ name: 'set-mistakes', setId, setTitle })}
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
