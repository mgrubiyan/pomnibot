import { Flex, Grid, Typography } from '@maxhub/max-ui';
import type { TableLayout } from '../types';
import { cx } from '../utils/cx';
import { placedCount, type Placement } from '../utils/table';
import { IconCheck, IconCross } from './Icons';
import s from './TableCard.module.css';

interface ChipProps {
    text: string;
    placed?: boolean;
    selected?: boolean;
    verdict?: 'correct' | 'wrong';
    disabled?: boolean;
    onClick?: () => void;
}

function Chip({ text, placed, selected, verdict, disabled, onClick }: ChipProps) {
    return (
        <button
            type="button"
            className={cx(
                s.chip,
                placed && s.chipPlaced,
                selected && s.chipSelected,
                verdict === 'correct' && s.chipCorrect,
                verdict === 'wrong' && s.chipWrong,
            )}
            aria-pressed={selected}
            disabled={disabled}
            onClick={onClick}
        >
            {verdict === 'correct' ? <IconCheck size={18} tone="positive" /> : null}
            {verdict === 'wrong' ? <IconCross size={18} tone="negative" /> : null}
            <Typography.Text variant="body" className={s.chipText}>
                {text}
            </Typography.Text>
        </button>
    );
}

export interface TableColumnsProps {
    layout: TableLayout;
    placement: Placement;
    /** Until the card is checked a term can be put back into the pool. */
    checked: boolean;
    hasSelection: boolean;
    onDropTo: (column: string) => void;
    onTakeBack: (index: number) => void;
}

/** Columns holding the placed terms — the upper part of the card. */
export function TableColumns({
    layout,
    placement,
    checked,
    hasSelection,
    onDropTo,
    onTakeBack,
}: TableColumnsProps) {
    const allPlaced = placedCount(placement) === layout.items.length;

    return (
        <Grid cols={2} gapX={0} gapY={12}>
            {layout.columns.map((column) => (
                <Flex key={column} direction="column" align="stretch" gap={8} className={s.column}>
                    <Typography.Text variant="description-strong" color="secondary" className={s.columnTitle}>
                        {column}
                    </Typography.Text>

                    {layout.items.map((item, index) =>
                        placement[index] === column ? (
                            <Chip
                                key={item.text}
                                text={item.text}
                                placed
                                verdict={
                                    checked
                                        ? placement[index] === item.column
                                            ? 'correct'
                                            : 'wrong'
                                        : undefined
                                }
                                disabled={checked}
                                onClick={() => onTakeBack(index)}
                            />
                        ) : null,
                    )}

                    {!checked && !allPlaced ? (
                        <button
                            type="button"
                            className={s.slot}
                            disabled={!hasSelection}
                            aria-label={`Положить термин в столбец «${column}»`}
                            onClick={() => onDropTo(column)}
                        >
                            <Typography.Text variant="description" color="secondary">
                                Положить сюда
                            </Typography.Text>
                        </button>
                    ) : null}
                </Flex>
            ))}
        </Grid>
    );
}

export interface TablePoolProps {
    layout: TableLayout;
    placement: Placement;
    selected: number | null;
    onSelect: (index: number) => void;
}

/** Terms not placed yet — the lower part of the screen. */
export function TablePool({ layout, placement, selected, onSelect }: TablePoolProps) {
    const left = layout.items.length - placedCount(placement);

    return (
        <Flex direction="column" align="stretch" gap={8}>
            <Typography.Text variant="label" color="tertiary">
                {left > 0 ? `Осталось ${left} · выберите термин, затем столбец` : 'Все термины разложены'}
            </Typography.Text>

            {left > 0 ? (
                <Flex gap={8} className={s.pool}>
                    {layout.items.map((item, index) =>
                        placement[index] === null ? (
                            <Chip
                                key={item.text}
                                text={item.text}
                                selected={selected === index}
                                onClick={() => onSelect(index)}
                            />
                        ) : null,
                    )}
                </Flex>
            ) : null}
        </Flex>
    );
}
