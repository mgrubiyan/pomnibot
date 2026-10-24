import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Button, CellList, CellSimple, Flex, IconButton, Spinner, Typography } from '@maxhub/max-ui';
import type { Card, CardSet, Fact, User } from '../types';
import { Screen } from '../components/Screen';
import { ScreenHeader } from '../components/ScreenHeader';
import { StatusScreen } from '../components/StatusScreen';
import { IconOffline, IconPencil, IconPlus, IconTrash } from '../components/Icons';
import { InputDialog } from '../components/InputDialog';
import { ConfirmActionDialog } from '../components/ConfirmActionDialog';
import {
    createFact,
    deleteFact,
    loadCards,
    loadFacts,
    loadSet,
    reorderCards,
    updateFact,
    updateSetTitle,
} from '../utils/sets';
import { groupCardsByFacts } from '../utils/factGroups';
import { getFactCardIds, reorderCardsInFact } from '../utils/cardsOrder';
import { classifyError } from '../utils/errors';
import s from './SetCards.module.css';

type Status = 'loading' | 'error' | 'ready';

export interface SetCardsProps {
    setId: string;
    currentUser?: User | null;
    /** What happened to a card on the way back here. */
    toast?: { kind: 'removed' | 'edited' } | null;
    onBack: () => void;
    onAddCard: (setTitle: string, factId: string) => void;
    onOpenCard: (card: Card, setTitle: string) => void;
}

/** Cards of a set, organized into facts with title editing and drag-and-drop reordering. */
export function SetCards({
    setId,
    currentUser,
    toast,
    onBack,
    onAddCard,
    onOpenCard,
}: SetCardsProps) {
    const [status, setStatus] = useState<Status>('loading');
    const [set, setSet] = useState<CardSet | null>(null);
    const [cards, setCards] = useState<Card[]>([]);
    const [facts, setFacts] = useState<Fact[]>([]);
    const [loadError, setLoadError] = useState<unknown>(null);
    const [attempt, setAttempt] = useState(0);

    // Dialog states
    const [isEditingTitle, setIsEditingTitle] = useState(false);
    const [isCreatingFact, setIsCreatingFact] = useState(false);
    const [editingFact, setEditingFact] = useState<Fact | null>(null);
    const [deletingFact, setDeletingFact] = useState<{
        factId: string;
        factName: string;
        cardCount: number;
    } | null>(null);

    // Drag-and-drop state for reordering cards within a fact
    const [draggingState, setDraggingState] = useState<{
        factId: string;
        cardId: string;
    } | null>(null);
    const dragFactIdRef = useRef<string | null>(null);
    const dragCardIdRef = useRef<string | null>(null);
    const dragStartIndexRef = useRef<number | null>(null);
    const dragCurrentIndexRef = useRef<number | null>(null);
    const initialFactCardsOrderRef = useRef<Card[]>([]);
    const activePointerIdRef = useRef<number | null>(null);
    const latestClientYRef = useRef<number | null>(null);
    const autoScrollRafRef = useRef<number | null>(null);
    const bodyRef = useRef<HTMLDivElement | null>(null);

    useEffect(() => {
        let cancelled = false;

        Promise.all([loadSet(setId), loadCards(setId), loadFacts(setId)])
            .then(([nextSet, nextCards, nextFacts]) => {
                if (cancelled) return;
                setSet(nextSet);
                setCards(nextCards);
                setFacts(nextFacts);
                setStatus('ready');
            })
            .catch((err) => {
                if (!cancelled) {
                    setLoadError(err);
                    setStatus('error');
                }
            });

        return () => {
            cancelled = true;
        };
    }, [attempt, setId]);

    useEffect(() => {
        if (status === 'ready' && set && currentUser && set.author && set.author.id !== currentUser.id) {
            onBack();
        }
    }, [status, set, currentUser, onBack]);

    const retry = () => {
        setStatus('loading');
        setAttempt((current) => current + 1);
    };

    const isOwner = Boolean(currentUser && set?.author && set.author.id === currentUser.id);

    const factGroups = useMemo(() => groupCardsByFacts(facts, cards), [facts, cards]);

    // Handle Set title update
    const handleSaveTitle = async (newTitle: string) => {
        if (!set) return;
        try {
            await updateSetTitle(setId, newTitle);
            setSet({ ...set, title: newTitle });
        } catch (err) {
            console.error('Failed to update set title:', err);
        }
    };

    // Handle Fact creation
    const handleCreateFact = async (name: string) => {
        try {
            const newFact = await createFact(setId, name);
            setFacts((prev) => [...prev, newFact]);
        } catch (err) {
            console.error('Failed to create fact:', err);
        }
    };

    // Handle Fact rename
    const handleUpdateFact = async (newName: string) => {
        if (!editingFact) return;
        try {
            await updateFact(editingFact.id, newName);
            setFacts((prev) =>
                prev.map((f) => (f.id === editingFact.id ? { ...f, name: newName } : f)),
            );
        } catch (err) {
            console.error('Failed to update fact:', err);
        }
    };

    // Handle Fact deletion
    const handleDeleteFactConfirm = async () => {
        if (!deletingFact) return;
        try {
            await deleteFact(deletingFact.factId);
            setFacts((prev) => prev.filter((f) => f.id !== deletingFact.factId));
            setCards((prev) => prev.filter((c) => c.factId !== deletingFact.factId));
        } catch (err) {
            console.error('Failed to delete fact:', err);
        }
    };

    const cardsRef = useRef(cards);
    useEffect(() => {
        cardsRef.current = cards;
    }, [cards]);

    const stopAutoScroll = useCallback(() => {
        if (autoScrollRafRef.current !== null) {
            cancelAnimationFrame(autoScrollRafRef.current);
            autoScrollRafRef.current = null;
        }
    }, []);

    const cleanupDrag = useCallback(() => {
        stopAutoScroll();
        activePointerIdRef.current = null;
        latestClientYRef.current = null;
        dragCardIdRef.current = null;
        setDraggingState(null);
    }, [stopAutoScroll]);

    // Lock page scrolling and text selection while dragging
    useEffect(() => {
        if (!draggingState) return;

        const originalUserSelect = document.body.style.userSelect;
        const originalWebkitUserSelect = document.body.style.webkitUserSelect;
        const originalTouchAction = document.body.style.touchAction;

        document.body.style.userSelect = 'none';
        document.body.style.webkitUserSelect = 'none';
        document.body.style.touchAction = 'none';

        return () => {
            document.body.style.userSelect = originalUserSelect;
            document.body.style.webkitUserSelect = originalWebkitUserSelect;
            document.body.style.touchAction = originalTouchAction;
        };
    }, [draggingState]);

    const updateCardPosition = useCallback((clientY: number) => {
        const activeFactId = dragFactIdRef.current;
        const draggingCardId = dragCardIdRef.current;
        if (!activeFactId || !draggingCardId) return;

        const cardElements = document.querySelectorAll<HTMLElement>(
            `[data-fact-id="${activeFactId}"][data-card-index]`,
        );
        if (cardElements.length === 0) return;

        let targetIndex = -1;
        for (let i = 0; i < cardElements.length; i++) {
            const rect = cardElements[i].getBoundingClientRect();
            if (clientY >= rect.top && clientY <= rect.bottom) {
                targetIndex = i;
                break;
            }
        }

        if (targetIndex === -1) {
            const firstRect = cardElements[0].getBoundingClientRect();
            const lastRect = cardElements[cardElements.length - 1].getBoundingClientRect();
            if (clientY < firstRect.top) targetIndex = 0;
            else if (clientY > lastRect.bottom) targetIndex = cardElements.length - 1;
        }

        if (targetIndex !== -1) {
            setCards((prevCards) => {
                const factCards = prevCards.filter((c) => c.factId === activeFactId);
                const fromIdx = factCards.findIndex((c) => c.id === draggingCardId);
                if (fromIdx === -1 || fromIdx === targetIndex) return prevCards;
                dragCurrentIndexRef.current = targetIndex;
                return reorderCardsInFact(prevCards, activeFactId, fromIdx, targetIndex);
            });
        }
    }, []);

    const startAutoScroll = useCallback(() => {
        if (autoScrollRafRef.current !== null) return;

        const checkAndScroll = () => {
            const container = bodyRef.current;
            const clientY = latestClientYRef.current;

            if (container && clientY !== null) {
                const rect = container.getBoundingClientRect();
                const EDGE_THRESHOLD = 50;
                const MAX_SPEED = 10;

                let scrollDelta = 0;
                if (clientY < rect.top + EDGE_THRESHOLD) {
                    const intensity = Math.min(
                        1,
                        Math.max(0, (rect.top + EDGE_THRESHOLD - clientY) / EDGE_THRESHOLD),
                    );
                    scrollDelta = -Math.max(2, intensity * MAX_SPEED);
                } else if (clientY > rect.bottom - EDGE_THRESHOLD) {
                    const intensity = Math.min(
                        1,
                        Math.max(0, (clientY - (rect.bottom - EDGE_THRESHOLD)) / EDGE_THRESHOLD),
                    );
                    scrollDelta = Math.max(2, intensity * MAX_SPEED);
                }

                if (scrollDelta !== 0) {
                    const prevScrollTop = container.scrollTop;
                    container.scrollTop += scrollDelta;
                    if (container.scrollTop !== prevScrollTop) {
                        updateCardPosition(clientY);
                    }
                }
            }

            autoScrollRafRef.current = requestAnimationFrame(checkAndScroll);
        };

        autoScrollRafRef.current = requestAnimationFrame(checkAndScroll);
    }, [updateCardPosition]);

    // Pointer event handlers for card drag-and-drop
    const handlePointerDown = (
        e: React.PointerEvent<HTMLSpanElement>,
        factId: string,
        cardId: string,
        index: number,
    ) => {
        e.stopPropagation();
        if (e.button !== 0 && e.pointerType === 'mouse') return;

        dragFactIdRef.current = factId;
        dragCardIdRef.current = cardId;
        dragStartIndexRef.current = index;
        dragCurrentIndexRef.current = index;
        initialFactCardsOrderRef.current = cardsRef.current;
        activePointerIdRef.current = e.pointerId;
        latestClientYRef.current = e.clientY;

        setDraggingState({ factId, cardId });
        startAutoScroll();
    };

    useEffect(() => {
        if (!draggingState) return;

        const onPointerMove = (e: PointerEvent) => {
            if (activePointerIdRef.current !== null && e.pointerId !== activePointerIdRef.current) return;
            e.preventDefault();
            latestClientYRef.current = e.clientY;
            updateCardPosition(e.clientY);
        };

        const onPointerUp = async (e: PointerEvent) => {
            if (activePointerIdRef.current !== null && e.pointerId !== activePointerIdRef.current) return;
            const activeFactId = dragFactIdRef.current;
            const startIdx = dragStartIndexRef.current;
            const finalIdx = dragCurrentIndexRef.current;

            cleanupDrag();

            dragFactIdRef.current = null;
            dragStartIndexRef.current = null;
            dragCurrentIndexRef.current = null;

            if (activeFactId && startIdx !== null && finalIdx !== null && startIdx !== finalIdx) {
                const newIds = getFactCardIds(cardsRef.current, activeFactId);
                try {
                    await reorderCards(activeFactId, newIds);
                } catch (err) {
                    console.error('Failed to commit reorder to server:', err);
                    // Revert to initial order
                    setCards(initialFactCardsOrderRef.current);
                }
            }
        };

        const onPointerCancel = (e: PointerEvent) => {
            if (activePointerIdRef.current !== null && e.pointerId !== activePointerIdRef.current) return;
            cleanupDrag();

            dragFactIdRef.current = null;
            dragStartIndexRef.current = null;
            dragCurrentIndexRef.current = null;

            // Revert order on cancel without server call
            setCards(initialFactCardsOrderRef.current);
        };

        window.addEventListener('pointermove', onPointerMove, { passive: false });
        window.addEventListener('pointerup', onPointerUp);
        window.addEventListener('pointercancel', onPointerCancel);

        return () => {
            window.removeEventListener('pointermove', onPointerMove);
            window.removeEventListener('pointerup', onPointerUp);
            window.removeEventListener('pointercancel', onPointerCancel);
        };
    }, [cleanupDrag, draggingState, updateCardPosition]);

    if (status === 'loading') {
        return (
            <StatusScreen
                icon={<Spinner size={32} appearance="neutral-themed" />}
                title="Загружаем карточки"
                text="Это займёт пару секунд"
            />
        );
    }

    if (status === 'error' || !set) {
        const errorInfo = classifyError(loadError);
        return (
            <StatusScreen
                icon={errorInfo.kind === 'offline' ? <IconOffline size={48} tone="muted" /> : undefined}
                title={errorInfo.title}
                text={errorInfo.text}
                action={
                    <Flex direction="column" align="stretch" gap={8}>
                        {errorInfo.canRetry && (
                            <Button size="medium" variant="primary" stretched onClick={retry}>
                                Повторить
                            </Button>
                        )}
                        <Button size="medium" variant="ghost" stretched onClick={onBack}>
                            Назад
                        </Button>
                    </Flex>
                }
            />
        );
    }

    return (
        <Screen>
            <ScreenHeader
                title={`Карточки · ${cards.length}`}
                subtitle={!isOwner ? set.title : undefined}
                onBack={onBack}
            />

            <Flex ref={bodyRef} direction="column" align="stretch" gap={8} className={s.body}>
                {isOwner && (
                    <Flex align="center" justify="space-between" className={s.setTitleCard}>
                        <Flex direction="column" gap={2} className={s.setTitleContent}>
                            <Typography.Text variant="description" color="tertiary">
                                Набор
                            </Typography.Text>
                            <Typography.Text variant="title" className={s.setTitleText}>
                                {set.title}
                            </Typography.Text>
                        </Flex>
                        <IconButton
                            size="small"
                            variant="ghost"
                            aria-label="Изменить название"
                            onClick={() => setIsEditingTitle(true)}
                        >
                            <IconPencil size={20} />
                        </IconButton>
                    </Flex>
                )}

                {isOwner ? (
                    <Typography.Text variant="description" color="tertiary" className={s.hint}>
                        Нажмите на карточку, чтобы редактировать её. Тяните за ≡, чтобы изменить
                        порядок в факте.
                    </Typography.Text>
                ) : null}

                {isOwner && (
                    <div className={s.addFactContainer}>
                        <Button
                            size="medium"
                            variant="secondary"
                            stretched
                            onClick={() => setIsCreatingFact(true)}
                        >
                            <IconPlus size={18} /> Добавить факт
                        </Button>
                    </div>
                )}

                {factGroups.length === 0 && (
                    <div className={s.emptyFactsState}>
                        <Typography.Text variant="body" color="secondary">
                            В наборе пока нет фактов. Создайте факт, чтобы добавлять карточки.
                        </Typography.Text>
                    </div>
                )}

                {factGroups.map((group) => {
                    const isRealFact = group.factId !== 'orphan';
                    return (
                        <Flex
                            key={group.factId}
                            direction="column"
                            align="stretch"
                            gap={6}
                            className={s.factSection}
                        >
                            <Flex
                                align="center"
                                justify="space-between"
                                className={s.factHeaderRow}
                            >
                                <Typography.Text
                                    variant="label-strong"
                                    color="secondary"
                                    className={s.factHeaderTitle}
                                >
                                    {group.factName}
                                </Typography.Text>
                                {isOwner && isRealFact && (
                                    <Flex align="center" gap={4} className={s.factActions}>
                                        <IconButton
                                            size="small"
                                            variant="ghost"
                                            aria-label="Редактировать факт"
                                            onClick={() =>
                                                setEditingFact({
                                                    id: group.factId,
                                                    setId,
                                                    name: group.factName,
                                                })
                                            }
                                        >
                                            <IconPencil size={18} />
                                        </IconButton>
                                        <IconButton
                                            size="small"
                                            variant="ghost"
                                            aria-label="Удалить факт"
                                            onClick={() =>
                                                setDeletingFact({
                                                    factId: group.factId,
                                                    factName: group.factName,
                                                    cardCount: group.cards.length,
                                                })
                                            }
                                        >
                                            <IconTrash size={18} />
                                        </IconButton>
                                    </Flex>
                                )}
                            </Flex>

                            {group.cards.length > 0 ? (
                                <CellList mode="island" filled className={s.list}>
                                    {group.cards.map((card, index) => {
                                        const isDragging =
                                            draggingState?.cardId === card.id;
                                        return (
                                            <div
                                                key={card.id}
                                                data-fact-id={group.factId}
                                                data-card-index={index}
                                                className={
                                                    isDragging
                                                        ? `${s.cardItem} ${s.cardDragging}`
                                                        : s.cardItem
                                                }
                                            >
                                                <CellSimple
                                                    as={isOwner ? 'button' : undefined}
                                                    separator={index > 0}
                                                    showChevron={isOwner && group.cards.length <= 1}
                                                    onClick={
                                                        isOwner
                                                            ? () => onOpenCard(card, set.title)
                                                            : undefined
                                                    }
                                                    title={
                                                        <span className={s.cardTitle}>
                                                            {card.question}
                                                        </span>
                                                    }
                                                    after={
                                                        isOwner &&
                                                        isRealFact &&
                                                        group.cards.length > 1 ? (
                                                            <span
                                                                role="button"
                                                                aria-label="Переместить карточку"
                                                                className={s.dragHandle}
                                                                onPointerDown={(e) =>
                                                                    handlePointerDown(
                                                                        e,
                                                                        group.factId,
                                                                        card.id,
                                                                        index,
                                                                    )
                                                                }
                                                                onClick={(e) => e.stopPropagation()}
                                                            >
                                                                ≡
                                                            </span>
                                                        ) : undefined
                                                    }
                                                />
                                            </div>
                                        );
                                    })}
                                </CellList>
                            ) : null}

                            {isOwner && isRealFact && (
                                <div className={s.factFooter}>
                                    <Button
                                        size="small"
                                        variant="ghost"
                                        stretched
                                        onClick={() => onAddCard(set.title, group.factId)}
                                    >
                                        + Добавить карточку
                                    </Button>
                                </div>
                            )}
                        </Flex>
                    );
                })}
            </Flex>

            {toast ? (
                <div className={s.notice} role="status">
                    <Typography.Text variant="detail">
                        {toast.kind === 'removed' ? 'Карточка удалена' : 'Карточка исправлена'}
                    </Typography.Text>
                </div>
            ) : null}

            {/* Dialog to edit Set Title */}
            {isEditingTitle && (
                <InputDialog
                    title="Название набора"
                    initialValue={set.title}
                    placeholder="Например, Органическая химия"
                    confirmLabel="Сохранить"
                    onConfirm={(newTitle) => {
                        setIsEditingTitle(false);
                        handleSaveTitle(newTitle);
                    }}
                    onCancel={() => setIsEditingTitle(false)}
                />
            )}

            {/* Dialog to create Fact */}
            {isCreatingFact && (
                <InputDialog
                    title="Новый факт"
                    description="Внутри факта можно создавать связанные карточки и менять их порядок."
                    placeholder="Например, Основные понятия"
                    confirmLabel="Создать"
                    onConfirm={(name) => {
                        setIsCreatingFact(false);
                        handleCreateFact(name);
                    }}
                    onCancel={() => setIsCreatingFact(false)}
                />
            )}

            {/* Dialog to rename Fact */}
            {editingFact && (
                <InputDialog
                    title="Переименовать факт"
                    initialValue={editingFact.name}
                    placeholder="Название факта"
                    confirmLabel="Сохранить"
                    onConfirm={(newName) => {
                        setEditingFact(null);
                        handleUpdateFact(newName);
                    }}
                    onCancel={() => setEditingFact(null)}
                />
            )}

            {/* Dialog to delete Fact */}
            {deletingFact && (
                <ConfirmActionDialog
                    title="Удалить факт?"
                    description={
                        deletingFact.cardCount > 0
                            ? `Факт «${deletingFact.factName}» и все его карточки (${deletingFact.cardCount}) будут удалены.`
                            : `Факт «${deletingFact.factName}» будет удалён.`
                    }
                    confirmLabel="Удалить"
                    cancelLabel="Отмена"
                    isDestructive
                    onConfirm={() => {
                        setDeletingFact(null);
                        handleDeleteFactConfirm();
                    }}
                    onCancel={() => setDeletingFact(null)}
                />
            )}
        </Screen>
    );
}

export default SetCards;
