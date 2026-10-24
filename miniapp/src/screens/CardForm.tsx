import { useEffect, useRef, useState } from 'react';
import { Button, Flex, IconButton, Typography } from '@maxhub/max-ui';
import type { Card, CardKind, TableItem, TableLayout } from '../types';
import { AutoTextarea } from '../components/AutoTextarea';
import { Screen } from '../components/Screen';
import { ScreenHeader } from '../components/ScreenHeader';
import { ConfirmDiscardDialog } from '../components/ConfirmDiscardDialog';
import { CardPreview } from '../components/CardPreview';
import { CARD_KINDS } from '../utils/kinds';
import { IconCross, IconDoc, IconPlus, IconTrash } from '../components/Icons';
import type { CardPatch } from '../utils/cards';
import s from './CardEdit.module.css';

const KIND_OPTIONS = CARD_KINDS.map(({ kind, label }) => ({ value: kind, label }));

export interface CardFormProps {
    card?: Partial<Card>;
    creationKind?: CardKind;
    onKindChange?: (kind: CardKind) => void;
    onDone?: () => void;
    onDirtyChange?: (dirty: boolean) => void;
    setTitle: string;
    onBack: () => void;
    onSave: (patch: CardPatch) => void | Promise<void>;
    onRemove?: () => void;
}

export function CardForm({ card = {}, creationKind, onKindChange, onDone, onDirtyChange, setTitle, onBack, onSave, onRemove }: CardFormProps) {
    const [editKind, setEditKind] = useState<CardKind>(card.kind ?? 'flip');
    const kind = creationKind ?? editKind;
    const setKind = onKindChange ?? setEditKind;
    const [saving, setSaving] = useState(false);
    const savingRef = useRef(false);
    const [error, setError] = useState('');
    const [question, setQuestion] = useState(card.question ?? '');
    const [explanation, setExplanation] = useState(card.explanation ?? '');
    const [confirmingRemove, setConfirmingRemove] = useState(false);
    const [pendingLeave, setPendingLeave] = useState<(() => void) | null>(null);

    // --- Choice kind state ---
    const [options, setOptions] = useState<string[]>(() => {
        if (card.options && card.options.length > 0) {
            return [...card.options];
        }
        const ans = typeof card.answer === 'string' ? card.answer : String(card.answer ?? '');
        return ans ? [ans, ''] : ['', ''];
    });

    const [correctOptionIndices, setCorrectOptionIndices] = useState<number[]>(() => {
        if (Array.isArray(card.answer)) {
            return (card.options ?? []).flatMap((option, i) => card.answer && Array.isArray(card.answer) && card.answer.includes(option) ? [i] : []);
        }
        if (typeof card.answer === 'number') return [card.answer];
        const found = (card.options ?? []).findIndex((option) => option === card.answer);
        return [found >= 0 ? found : 0];
    });

    // --- Boolean kind state ---
    const [booleanAnswer, setBooleanAnswer] = useState<boolean>(() => {
        if (typeof card.answer === 'boolean') {
            return card.answer;
        }
        return String(card.answer ?? '').trim().toLowerCase() === 'true';
    });

    // --- Input & Flip kind state ---
    const [textAnswer, setTextAnswer] = useState<string>(() => {
        if (typeof card.answer === 'string') {
            return card.answer;
        }
        if (typeof card.answer === 'number' || typeof card.answer === 'boolean') {
            return String(card.answer);
        }
        return '';
    });

    // --- Table kind state ---
    const [tableColumns, setTableColumns] = useState<string[]>(() => {
        if (card.table?.columns && card.table.columns.length > 0) {
            return [...card.table.columns];
        }
        return ['Столбец 1', 'Столбец 2'];
    });

    const [tableItems, setTableItems] = useState<TableItem[]>(() => {
        if (card.table?.items && card.table.items.length > 0) {
            return card.table.items.map((item) => ({ ...item }));
        }
        return [];
    });

    // --- Choice handlers ---
    const handleOptionChange = (index: number, value: string) => {
        setOptions((current) => current.map((opt, i) => (i === index ? value : opt)));
    };

    const handleOptionAdd = () => {
        setOptions((current) => [...current, '']);
    };

    const handleOptionRemove = (index: number) => {
        setOptions((current) => current.filter((_, i) => i !== index));
        setCorrectOptionIndices((prev) => prev.filter((i) => i !== index).map((i) => i > index ? i - 1 : i));
    };

    // --- Table handlers ---
    const handleColumnNameChange = (colIndex: number, newName: string) => {
        const oldName = tableColumns[colIndex];
        setTableColumns((current) => current.map((col, i) => (i === colIndex ? newName : col)));
        // Keep associated items updated with the new column name
        setTableItems((current) =>
            current.map((item) => (item.column === oldName ? { ...item, column: newName } : item)),
        );
    };

    const handleColumnAdd = () => {
        // Terms are tied to columns by name, so the new name must be free:
        // after a removal, «Столбец 3» may still be taken.
        let number = tableColumns.length + 1;
        while (tableColumns.includes(`Столбец ${number}`)) number++;
        const newColName = `Столбец ${number}`;
        setTableColumns((current) => [...current, newColName]);
    };

    const handleColumnRemove = (colIndex: number) => {
        const colToRemove = tableColumns[colIndex];
        setTableColumns((current) => current.filter((_, i) => i !== colIndex));
        setTableItems((current) => current.filter((item) => item.column !== colToRemove));
    };

    const handleTableItemAdd = (colName: string) => {
        setTableItems((current) => [...current, { text: '', column: colName }]);
    };

    const handleTableItemChange = (itemIndex: number, text: string) => {
        setTableItems((current) =>
            current.map((item, i) => (i === itemIndex ? { ...item, text } : item)),
        );
    };

    const handleTableItemRemove = (itemIndex: number) => {
        setTableItems((current) => current.filter((_, i) => i !== itemIndex));
    };

    // --- Validation logic ---
    // The first thing still missing, shown under the disabled «Сохранить»;
    // null when the card can be saved.
    const saveHint = ((): string | null => {
        if (question.trim().length === 0) return 'Напишите вопрос';
        if (kind === 'choice') {
            const trimmedOptions = options.map((opt) => opt.trim());
            if (trimmedOptions.length < 2) return 'Нужно хотя бы два варианта';
            if (!trimmedOptions.every((opt) => opt.length > 0)) return 'Заполните все варианты';
            if (new Set(trimmedOptions).size !== trimmedOptions.length) return 'Варианты не должны повторяться';
            if (correctOptionIndices.length === 0 || !correctOptionIndices.every((i) => i >= 0 && i < trimmedOptions.length)) {
                return 'Отметьте верный вариант';
            }
            return null;
        }
        if (kind === 'boolean') {
            return typeof booleanAnswer === 'boolean' ? null : 'Выберите верный ответ';
        }
        if (kind === 'table') {
            const trimmedCols = tableColumns.map((c) => c.trim());
            if (trimmedCols.length < 2) return 'Нужно хотя бы два столбца';
            if (!trimmedCols.every((c) => c.length > 0)) return 'Назовите все столбцы';
            if (new Set(trimmedCols).size !== trimmedCols.length) return 'Названия столбцов не должны повторяться';
            if (!tableItems.every((it) => it.text.trim().length > 0)) return 'Заполните все термины';
            if (!tableItems.every((it) => trimmedCols.includes(it.column.trim()))) return 'Разложите термины по столбцам';
            // Each column should have at least 1 item
            if (tableItems.length < 2 || !trimmedCols.every((col) => tableItems.some((it) => it.column.trim() === col))) {
                return 'В каждом столбце нужен хотя бы один термин';
            }
            return null;
        }
        // 'input' or 'flip'
        return textAnswer.trim().length > 0 ? null : 'Напишите ответ';
    })();

    const canSave = saveHint === null;

    const snapshot = JSON.stringify([kind, question, explanation, options, correctOptionIndices, booleanAnswer, textAnswer, tableColumns, tableItems]);
    const [initialSnapshot] = useState(snapshot);
    const dirty = snapshot !== initialSnapshot;
    const previewTable = { columns: tableColumns, items: tableItems };
    const previewCard: Partial<Card> = {
        kind,
        question,
        explanation,
        options: kind === 'choice' ? options : undefined,
        answer: kind === 'choice' ? correctOptionIndices.map((index) => options[index])
            : kind === 'boolean' ? booleanAnswer
            : kind === 'table' ? previewTable : textAnswer,
        table: kind === 'table' ? previewTable : undefined,
        questionSvg: dirty ? undefined : card.questionSvg,
        explanationSvg: dirty ? undefined : card.explanationSvg,
    };
    useEffect(() => { onDirtyChange?.(dirty); }, [dirty, onDirtyChange]);
    const leave = (action: () => void) => {
        if (savingRef.current) return;
        if (dirty) setPendingLeave(() => action);
        else action();
    };
    const save = async () => {
        if (savingRef.current || !canSave) {
            return;
        }

        const patch: CardPatch = {
            kind,
            question,
            explanation,
        };

        if (kind === 'choice') {
            patch.options = options;
            patch.answer = correctOptionIndices.map((i) => options[i]);
        } else if (kind === 'boolean') {
            patch.answer = booleanAnswer;
        } else if (kind === 'table') {
            const tableLayout: TableLayout = {
                columns: tableColumns,
                items: tableItems.map((item) => ({ ...item })),
            };
            patch.table = tableLayout;
            patch.answer = tableLayout;
        } else {
            // input or flip
            patch.answer = textAnswer;
        }

        savingRef.current = true;
        setSaving(true);
        setError('');
        try { await onSave(patch); }
        catch { setError('Не удалось сохранить карточку. Попробуйте ещё раз.'); }
        finally { savingRef.current = false; setSaving(false); }
    };

    return (
        <Screen aria-busy={saving}>
            <fieldset disabled={saving} style={{ border: 0, padding: 0, margin: 0, minWidth: 0, display: 'contents' }}>
                <ScreenHeader
                    title={creationKind ? 'Создать карточку' : 'Исправить карточку'}
                    subtitle={setTitle}
                    onBack={() => { if (!savingRef.current) { if (creationKind) onBack(); else leave(onBack); } }}
                />

                <Flex direction="column" align="stretch" gap={16} className={s.form}>
                    <details className={s.preview} open={Boolean(card.id)}>
                        <summary>Предпросмотр</summary>
                        <CardPreview card={previewCard} />
                    </details>
                    {card.questionSvg || card.explanationSvg ? (
                        <Typography.Text variant="description" color="secondary">
                            При сохранении карточки иллюстрации будут удалены.
                        </Typography.Text>
                    ) : null}
                    {/* Card kind Combobox */}
                    <Flex direction="column" align="stretch" gap={4}>
                        <Typography.Text variant="label" color="tertiary" asChild>
                            <label htmlFor="card-kind">Тип карточки</label>
                        </Typography.Text>
                        <div className={s.selectWrapper}>
                            <select
                                id="card-kind"
                                className={s.select}
                                value={kind}
                                onChange={(event) => {
                                    const newKind = event.target.value as CardKind;
                                    setKind(newKind);
                                }}
                            >
                                {KIND_OPTIONS.map((item) => (
                                    <option key={item.value} value={item.value}>
                                        {item.label}
                                    </option>
                                ))}
                            </select>
                        </div>
                    </Flex>

                    {/* Question field */}
                    <Flex direction="column" align="stretch" gap={4}>
                        <Typography.Text variant="label" color="tertiary" asChild>
                            <label htmlFor="card-question">Вопрос</label>
                        </Typography.Text>
                        <AutoTextarea
                            id="card-question"
                            rows={2}
                            value={question}
                            className={s.field}
                            placeholder="Введите текст вопроса"
                            onChange={(event) => setQuestion(event.target.value)}
                        />
                        <Typography.Text variant="description" color="tertiary">
                            Поддерживаются Markdown и формулы: $...$, $$...$$.
                        </Typography.Text>
                    </Flex>

                    {/* Answer section based on selected kind */}
                    {kind === 'choice' && (
                        <Flex direction="column" align="stretch" gap={8}>
                            <Typography.Text variant="label" color="tertiary">
                                Варианты ответа (отметьте все верные)
                            </Typography.Text>

                            {options.map((option, index) => (
                                <Flex key={index} align="center" gap={8} className={s.optionRow}>
                                    <input
                                        type="checkbox"
                                        value={String(index)}
                                        checked={correctOptionIndices.includes(index)}
                                        aria-label={`Отметить вариант ${index + 1} как верный`}
                                        onChange={() => setCorrectOptionIndices((current) => current.includes(index) ? current.filter((i) => i !== index) : [...current, index])}
                                    />
                                    {/* MAX UI wraps the field in a div of its own; this one is
                                        the flex item that takes the free width. */}
                                    <div className={s.optionInput}>
                                        <AutoTextarea
                                            rows={1}
                                            className={s.field}
                                            value={option}
                                            aria-label={`Вариант ${index + 1}`}
                                            placeholder={`Вариант ${index + 1}`}
                                            onChange={(event) => handleOptionChange(index, event.target.value)}
                                        />
                                    </div>
                                    <IconButton
                                        size="small"
                                        variant="ghost"
                                        disabled={options.length <= 2}
                                        aria-label={`Удалить вариант ${index + 1}`}
                                        onClick={() => handleOptionRemove(index)}
                                    >
                                        <IconCross size={20} tone="muted" />
                                    </IconButton>
                                </Flex>
                            ))}

                            <Button
                                size="small"
                                variant="ghost"
                                iconBefore={<IconPlus size={20} />}
                                onClick={handleOptionAdd}
                            >
                                Добавить вариант
                            </Button>
                        </Flex>
                    )}

                    {kind === 'boolean' && (
                        <Flex direction="column" align="stretch" gap={8}>
                            <Typography.Text variant="label" color="tertiary">
                                Верный ответ
                            </Typography.Text>
                            <Flex gap={8} align="stretch">
                                <Button
                                    size="medium"
                                    variant={booleanAnswer ? 'primary' : 'secondary'}
                                    stretched
                                    onClick={() => setBooleanAnswer(true)}
                                >
                                    Верно
                                </Button>
                                <Button
                                    size="medium"
                                    variant={!booleanAnswer ? 'primary' : 'secondary'}
                                    stretched
                                    onClick={() => setBooleanAnswer(false)}
                                >
                                    Неверно
                                </Button>
                            </Flex>
                        </Flex>
                    )}

                    {(kind === 'input' || kind === 'flip') && (
                        <Flex direction="column" align="stretch" gap={4}>
                            <Typography.Text variant="label" color="tertiary" asChild>
                                <label htmlFor="card-answer">Верный ответ</label>
                            </Typography.Text>
                            <AutoTextarea
                                id="card-answer"
                                rows={3}
                                value={textAnswer}
                                className={s.field}
                                placeholder="Текст верного ответа"
                                onChange={(event) => setTextAnswer(event.target.value)}
                            />
                        </Flex>
                    )}

                    {kind === 'table' && (
                        <Flex direction="column" align="stretch" gap={12}>
                            <Flex direction="column" gap={2}>
                                <Typography.Text variant="label" color="tertiary">
                                    Таблица: столбцы и термины
                                </Typography.Text>
                                <Typography.Text variant="description" color="secondary">
                                    Распределите термины по колонкам (минимум 2 колонки и по термину в каждой)
                                </Typography.Text>
                            </Flex>

                            {tableColumns.map((colName, colIdx) => {
                                // Find all items belonging to this column
                                const colItems = tableItems
                                    .map((item, itemIdx) => ({ item, itemIdx }))
                                    .filter(({ item }) => item.column === colName);

                                return (
                                    <Flex
                                        key={colIdx}
                                        direction="column"
                                        align="stretch"
                                        gap={8}
                                        className={s.columnCard}
                                    >
                                        <Flex align="center" gap={8} className={s.columnHeader}>
                                            <AutoTextarea
                                                rows={1}
                                                value={colName}
                                                aria-label={`Название столбца ${colIdx + 1}`}
                                                className={s.columnInput}
                                                placeholder={`Столбец ${colIdx + 1}`}
                                                onChange={(e) => handleColumnNameChange(colIdx, e.target.value)}
                                            />
                                            <IconButton
                                                size="small"
                                                variant="ghost"
                                                disabled={tableColumns.length <= 2}
                                                aria-label={`Удалить столбец ${colName}`}
                                                onClick={() => handleColumnRemove(colIdx)}
                                            >
                                                <IconCross size={20} tone="muted" />
                                            </IconButton>
                                        </Flex>

                                        <Flex direction="column" align="stretch" gap={6}>
                                            {colItems.map(({ item, itemIdx }) => (
                                                <Flex key={itemIdx} align="center" gap={8} className={s.itemRow}>
                                                    <AutoTextarea
                                                        rows={1}
                                                        value={item.text}
                                                        aria-label={`Термин столбца ${colName}`}
                                                        className={s.itemInput}
                                                        placeholder="Введите термин"
                                                        onChange={(e) =>
                                                            handleTableItemChange(itemIdx, e.target.value)
                                                        }
                                                    />
                                                    <IconButton
                                                        size="small"
                                                        variant="ghost"
                                                        aria-label="Удалить термин"
                                                        onClick={() => handleTableItemRemove(itemIdx)}
                                                    >
                                                        <IconCross size={18} tone="muted" />
                                                    </IconButton>
                                                </Flex>
                                            ))}
                                        </Flex>

                                        <Button
                                            size="small"
                                            variant="ghost"
                                            iconBefore={<IconPlus size={16} />}
                                            onClick={() => handleTableItemAdd(colName)}
                                        >
                                            Добавить термин
                                        </Button>
                                    </Flex>
                                );
                            })}

                            <Button
                                size="small"
                                variant="secondary"
                                iconBefore={<IconPlus size={20} />}
                                onClick={handleColumnAdd}
                            >
                                Добавить столбец
                            </Button>
                        </Flex>
                    )}

                    {/* Explanation field */}
                    <Flex direction="column" align="stretch" gap={4}>
                        <Typography.Text variant="label" color="tertiary" asChild>
                            <label htmlFor="card-explanation">Объяснение (необязательно)</label>
                        </Typography.Text>
                        <AutoTextarea
                            id="card-explanation"
                            rows={3}
                            value={explanation}
                            className={s.field}
                            placeholder="2–3 предложения с пояснением верного ответа"
                            onChange={(event) => setExplanation(event.target.value)}
                        />
                    </Flex>

                    {/* Read-only Source quote from notes */}
                    {card.sourceQuote ? (<>
                        <div className={s.divider} />
                        <figure className={s.source}>
                            <Flex direction="column" align="stretch" gap={4}>
                                <Typography.Text variant="label-strong" color="tertiary" asChild>
                                    <figcaption>
                                        <Flex align="flex-start" gap={4}>
                                            <IconDoc size={14} tone="muted" className={s.sourceIcon} />
                                            <span>
                                                Источник
                                                {card.sourceRef ? ` · ${card.sourceRef}` : ''}
                                            </span>
                                        </Flex>
                                    </figcaption>
                                </Typography.Text>
                                <Typography.Text variant="description" color="tertiary" asChild>
                                    <blockquote className={s.quote}>«{card.sourceQuote}»</blockquote>
                                </Typography.Text>
                            </Flex>
                        </figure>
                    </>) : null}
                </Flex>

                {/* Bottom Actions */}
                <Flex direction="column" align="stretch" gap={8}>
                    {confirmingRemove ? (
                        <div className={s.confirmBox}>
                            <Flex direction="column" align="stretch" gap={8}>
                                <Typography.Text variant="description" color="secondary" className={s.confirmText}>
                                    Точно удалить карточку? Она пропадёт из набора навсегда.
                                </Typography.Text>
                                <Flex gap={8} align="stretch">
                                    <Button
                                        size="medium"
                                        variant="secondary"
                                        stretched
                                        onClick={() => setConfirmingRemove(false)}
                                    >
                                        Отмена
                                    </Button>
                                    <Button
                                        size="medium"
                                        variant="destructive"
                                        stretched
                                        onClick={onRemove}
                                    >
                                        Удалить
                                    </Button>
                                </Flex>
                            </Flex>
                        </div>
                    ) : (
                        <>
                            <Button size="medium" variant="primary" stretched disabled={!canSave || saving} onClick={save}>
                                {saving ? 'Сохраняем…' : 'Сохранить'}
                            </Button>
                            {saveHint && !saving ? (
                                <Typography.Text variant="description" color="tertiary" className={s.saveHint}>
                                    {saveHint}
                                </Typography.Text>
                            ) : null}
                            {onRemove && <Button
                                size="small"
                                variant="ghost"
                                stretched
                                iconBefore={<IconTrash size={16} tone="muted" />}
                                onClick={() => setConfirmingRemove(true)}
                            >
                                <Typography.Text variant="description" color="tertiary">
                                    Удалить карточку
                                </Typography.Text>
                            </Button>}
                            {onDone && <Button size="medium" variant="secondary" stretched onClick={() => leave(onDone)}>Готово</Button>}
                        </>
                    )}
                </Flex>
                {error && <Typography.Text role="alert">{error}</Typography.Text>}
            </fieldset>
            {pendingLeave && <ConfirmDiscardDialog
                onCancel={() => setPendingLeave(null)}
                onConfirm={() => { setPendingLeave(null); pendingLeave(); }}
            />}
        </Screen>
    );
}

export default CardForm;
