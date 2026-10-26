import { useState } from 'react';
import { Button, CellList, CellSimple, Flex, Input, Typography } from '@maxhub/max-ui';
import { Screen } from '../components/Screen';
import { ScreenHeader } from '../components/ScreenHeader';
import { ConfirmDiscardDialog } from '../components/ConfirmDiscardDialog';
import CardForm from './CardForm';
import type { CardKind } from '../types';
import type { CardPatch } from '../utils/cards';
import { api } from '../api';
import { CARD_KINDS } from '../utils/kinds';
import s from './ManualCreate.module.css';

export default function ManualCreate({
    setId: initialSetId,
    setTitle: initialTitle = '',
    factId,
    onExit,
    onDone,
}: {
    setId?: string;
    setTitle?: string;
    factId?: string;
    onExit: () => void;
    onDone: (setId: string) => void;
}) {
    const [setId, setSetId] = useState(initialSetId);
    const [title, setTitle] = useState(initialTitle);
    const [step, setStep] = useState<'title' | 'kind' | 'card'>(initialSetId ? 'kind' : 'title');
    const [kind, setKind] = useState<CardKind>('flip');
    const [started, setStarted] = useState(false);
    const [version, setVersion] = useState(0);
    const [dirty, setDirty] = useState(false);
    const [creatingSet, setCreatingSet] = useState(false);
    const [pendingLeave, setPendingLeave] = useState<(() => void) | null>(null);

    const exit = () => {
        if (!dirty && (setId || !title.trim())) onExit();
        else setPendingLeave(() => onExit);
    };

    const handleCreateSet = async () => {
        const trimmed = title.trim();
        if (!trimmed || creatingSet) return;
        setCreatingSet(true);
        try {
            const { data, error } = await api.POST('/sets', {
                body: { title: trimmed },
            });
            if (error || !data) {
                throw new Error('Save failed');
            }
            setSetId(data.id);
            onDone(data.id);
        } catch (err) {
            console.error('Failed to create set:', err);
            setCreatingSet(false);
        }
    };

    const save = async (patch: CardPatch) => {
        if (!patch.kind || !patch.question || patch.answer === undefined) return;
        const body = {
            kind: patch.kind,
            question: patch.question,
            answer: patch.answer,
            explanation: patch.explanation,
            options: patch.options,
            table: patch.table,
            questionImageIds: patch.questionImageIds,
            explanationImageIds: patch.explanationImageIds,
        };

        if (factId) {
            const { error, data } = await api.POST('/facts/{factId}/cards', {
                params: { path: { factId } },
                body,
            });
            if (error || !data) throw new Error('Save failed');
        } else if (setId) {
            const { error, data } = await api.POST('/sets/{setId}/cards', {
                params: { path: { setId } },
                body,
            });
            if (error || !data) throw new Error('Save failed');
        } else {
            const { error, data } = await api.POST('/sets', {
                body: { title: title.trim(), card: body },
            });
            if (error || !data) throw new Error('Save failed');
            setSetId(data.id);
        }
        setDirty(false);
        setVersion((v) => v + 1);
    };

    return (
        <>
            {step === 'title' && (
                <Screen>
                    <ScreenHeader title="Новый набор" onBack={exit} />
                    <Flex direction="column" align="stretch" gap={12} className={s.body}>
                        <Typography.Text variant="body" color="secondary" className={s.hint}>
                            Как назовёте набор? Название увидят все, с кем вы им поделитесь.
                        </Typography.Text>
                        <Input
                            size="large"
                            aria-label="Название набора"
                            placeholder="Например, Органическая химия"
                            value={title}
                            maxLength={120}
                            onChange={(e) => setTitle(e.target.value)}
                            onKeyDown={(e) => {
                                if (e.key === 'Enter' && title.trim()) handleCreateSet();
                            }}
                        />
                    </Flex>
                    <Flex direction="column" align="stretch">
                        <Button
                            size="medium"
                            stretched
                            disabled={!title.trim() || creatingSet}
                            onClick={handleCreateSet}
                        >
                            {creatingSet ? 'Создаём...' : 'Создать набор'}
                        </Button>
                    </Flex>
                </Screen>
            )}
            {step === 'kind' && (
                <Screen>
                    <ScreenHeader title="Тип карточки" onBack={() => (setId ? exit() : setStep('title'))} />
                    <Flex direction="column" align="stretch" className={s.list}>
                        <CellList mode="island" filled className={s.cells}>
                            {CARD_KINDS.map((item, index) => (
                                <CellSimple
                                    key={item.kind}
                                    as="button"
                                    showChevron
                                    separator={index > 0}
                                    title={item.label}
                                    subtitle={item.hint}
                                    onClick={() => {
                                        setKind(item.kind);
                                        setStarted(true);
                                        setStep('card');
                                    }}
                                />
                            ))}
                        </CellList>
                    </Flex>
                </Screen>
            )}
            {started && (
                <div hidden={step !== 'card'}>
                    <CardForm
                        key={version}
                        card={{ setId }}
                        setTitle={title}
                        creationKind={kind}
                        onKindChange={setKind}
                        onDirtyChange={setDirty}
                        onSave={save}
                        onBack={() => {
                            if (!setId) setStep('kind');
                            else if (dirty) setPendingLeave(() => () => onDone(setId));
                            else onDone(setId);
                        }}
                        onDone={setId ? () => onDone(setId) : undefined}
                    />
                </div>
            )}
            {pendingLeave && (
                <ConfirmDiscardDialog
                    onCancel={() => setPendingLeave(null)}
                    onConfirm={() => {
                        setPendingLeave(null);
                        pendingLeave();
                    }}
                />
            )}
        </>
    );
}
