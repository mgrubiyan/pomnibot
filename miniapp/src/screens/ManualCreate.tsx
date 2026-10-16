import { useState } from 'react';
import { Button, Flex, Input, Typography } from '@maxhub/max-ui';
import { Screen } from '../components/Screen';
import CardForm from './CardForm';
import type { CardKind } from '../types';
import type { CardPatch } from '../utils/cards';
import { api } from '../api';

const kinds: { kind: CardKind; label: string }[] = [
    { kind: 'flip', label: 'Флип-карточка' },
    { kind: 'choice', label: 'Выбор ответа (Тест)' },
    { kind: 'boolean', label: 'Верно / Неверно' },
    { kind: 'input', label: 'Ввод текста' },
    { kind: 'table', label: 'Таблица (сортировка)' },
];

export default function ManualCreate({ setId: initialSetId, setTitle: initialTitle = '', onExit, onDone }: {
    setId?: string; setTitle?: string; onExit: () => void; onDone: (setId: string) => void;
}) {
    const [setId, setSetId] = useState(initialSetId);
    const [title, setTitle] = useState(initialTitle);
    const [step, setStep] = useState<'title' | 'kind' | 'card'>(initialSetId ? 'kind' : 'title');
    const [kind, setKind] = useState<CardKind>('flip');
    const [started, setStarted] = useState(false);
    const [version, setVersion] = useState(0);
    const [dirty, setDirty] = useState(false);
    const exit = () => {
        if ((!dirty && (setId || !title.trim())) || window.confirm('Несохранённые изменения будут потеряны. Выйти?')) onExit();
    };
    const save = async (patch: CardPatch) => {
        if (!patch.kind || !patch.question || patch.answer === undefined) return;
        const body = { kind: patch.kind, question: patch.question, answer: patch.answer,
            explanation: patch.explanation, options: patch.options, table: patch.table };
        if (setId) {
            const { error, data } = await api.POST('/sets/{setId}/cards', { params: { path: { setId } }, body });
            if (error || !data) throw new Error('Save failed');
        } else {
            const { error, data } = await api.POST('/sets', { body: { title: title.trim(), card: body } });
            if (error || !data) throw new Error('Save failed');
            setSetId(data.id);
        }
        setDirty(false);
        setVersion(v => v + 1);
    };
    return <>
        {step !== 'card' && <Screen><Flex direction="column" align="stretch" gap={16}>
            <Typography.Text variant="subheader">{step === 'title' ? 'Название набора' : 'Тип карточки'}</Typography.Text>
            {step === 'title' ? <>
                <Input aria-label="Название набора" placeholder="Название набора" value={title} onChange={e => setTitle(e.target.value)} />
                <Button disabled={!title.trim()} onClick={() => setStep('kind')}>Далее</Button>
            </> : kinds.map(item => <Button key={item.kind} variant="secondary" onClick={() => { setKind(item.kind); setStarted(true); setStep('card'); }}>{item.label}</Button>)}
            <Button variant="ghost" onClick={() => step === 'kind' && !setId ? setStep('title') : exit()}>Назад</Button>
        </Flex></Screen>}
        {started && <div hidden={step !== 'card'}><CardForm key={version}
            setTitle={title} creationKind={kind} onKindChange={setKind} onDirtyChange={setDirty}
            onSave={save} onBack={() => {
                if (!setId) setStep('kind');
                else if (!dirty || window.confirm('Несохранённые изменения будут потеряны. Выйти?')) onDone(setId);
            }}
            onDone={setId ? () => onDone(setId) : undefined} />
        </div>}
    </>;
}
